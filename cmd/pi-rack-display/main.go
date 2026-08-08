package main

import (
	"context"
	"errors"
	"fmt"
	"image"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/ukd1/pi-rack-display/internal/deviceplugin"
	"github.com/ukd1/pi-rack-display/internal/display"
	"github.com/ukd1/pi-rack-display/internal/kube"
	"github.com/ukd1/pi-rack-display/internal/model"
	"github.com/ukd1/pi-rack-display/internal/screen"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

var version = "dev"

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.LUTC)
	if len(os.Args) != 2 {
		fatalf("usage: %s display|device-plugin|version", os.Args[0])
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var err error
	switch os.Args[1] {
	case "display":
		err = runDisplay(ctx)
	case "device-plugin":
		err = runDevicePlugin(ctx)
	case "version":
		fmt.Println(version)
		return
	default:
		fatalf("unknown command %q; use display, device-plugin, or version", os.Args[1])
	}
	if err != nil {
		fatalf("%v", err)
	}
}

func runDevicePlugin(ctx context.Context) error {
	pluginDirectory := envOr("PLUGIN_DIRECTORY", pluginapi.DevicePluginPath)
	server := deviceplugin.New(
		pluginDirectory,
		envOr("DISPLAY_DEVICE", deviceplugin.DefaultDevicePath),
		envOr("THERMAL_HOST_PATH", deviceplugin.DefaultThermalHostPath),
		envOr("THERMAL_CONTAINER_PATH", deviceplugin.DefaultThermalContainerPath),
	)
	log.Printf("starting device plugin version=%s resource=%s", version, deviceplugin.ResourceName)
	return server.Run(ctx)
}

func runDisplay(ctx context.Context) error {
	nodeName := strings.TrimSpace(os.Getenv("NODE_NAME"))
	if nodeName == "" {
		return errors.New("NODE_NAME is required")
	}
	pageInterval, err := durationEnv("PAGE_INTERVAL", 10*time.Second, 2*time.Second)
	if err != nil {
		return err
	}
	sampleInterval, err := durationEnv("SAMPLE_INTERVAL", 15*time.Second, 5*time.Second)
	if err != nil {
		return err
	}
	devicePath := envOr("DISPLAY_DEVICE", deviceplugin.DefaultDevicePath)
	temperaturePath := envOr("THERMAL_PATH", deviceplugin.DefaultThermalContainerPath)

	client, err := kube.NewInCluster()
	if err != nil {
		return err
	}
	displayDevice, err := display.Open(devicePath, display.I2CAddress)
	if err != nil {
		return err
	}
	defer displayDevice.Close()

	var ready atomic.Bool
	var lastLoop atomic.Int64
	lastLoop.Store(time.Now().UnixNano())
	stallThreshold := max(45*time.Second, 3*pageInterval)
	healthServer := newHealthServer(envOr("LISTEN_ADDRESS", ":8080"), stallThreshold, &ready, &lastLoop)
	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("health server listening address=%s", healthServer.Addr)
		if err := healthServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
	}()
	defer func() {
		shutdownContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = healthServer.Shutdown(shutdownContext)
	}()

	log.Printf("starting display version=%s node=%s device=%s page_interval=%s sample_interval=%s", version, nodeName, devicePath, pageInterval, sampleInterval)
	var snapshot model.Snapshot
	nextSample := time.Time{}
	firstFrame := true
	for page := 0; ; page = (page + 1) % screen.PageCount {
		if time.Now().After(nextSample) {
			collectContext, cancel := context.WithTimeout(ctx, 6*time.Second)
			collected := client.Collect(collectContext, nodeName, os.Getenv("HOST_IP"), temperaturePath)
			cancel()
			snapshot = mergeSnapshot(snapshot, collected)
			for _, warning := range collected.Warnings {
				log.Printf("telemetry warning node=%s warning=%q", nodeName, warning)
			}
			nextSample = time.Now().Add(sampleInterval)
		}

		if err := displayDevice.Draw(screen.Render(page, snapshot)); err != nil {
			ready.Store(false)
			return err
		}
		lastLoop.Store(time.Now().UnixNano())
		ready.Store(true)
		if firstFrame {
			log.Printf("first display frame written node=%s", nodeName)
			firstFrame = false
		}

		timer := time.NewTimer(pageInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			ready.Store(false)
			blank := image.NewRGBA(image.Rect(0, 0, display.Width, display.Height))
			if err := displayDevice.Draw(blank); err != nil {
				log.Printf("blank display during shutdown: %v", err)
			}
			return nil
		case err := <-serverErrors:
			timer.Stop()
			return fmt.Errorf("health server: %w", err)
		case <-timer.C:
		}
	}
}

func newHealthServer(address string, stallThreshold time.Duration, ready *atomic.Bool, lastLoop *atomic.Int64) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(writer http.ResponseWriter, _ *http.Request) {
		last := time.Unix(0, lastLoop.Load())
		if time.Since(last) > stallThreshold {
			http.Error(writer, "display loop stalled", http.StatusServiceUnavailable)
			return
		}
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/readyz", func(writer http.ResponseWriter, _ *http.Request) {
		if !ready.Load() {
			http.Error(writer, "display has not written a frame", http.StatusServiceUnavailable)
			return
		}
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("ready\n"))
	})
	return &http.Server{
		Addr:              address,
		Handler:           mux,
		ReadHeaderTimeout: 2 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
}

func mergeSnapshot(previous, current model.Snapshot) model.Snapshot {
	if !current.HasNode && previous.HasNode {
		current.NodeIP = previous.NodeIP
		current.Ready = previous.Ready
		current.Cordoned = previous.Cordoned
		current.CPUCapacityMilli = previous.CPUCapacityMilli
		current.MemoryCapacity = previous.MemoryCapacity
		current.HasNode = true
		current.NodeStale = true
	}
	if !current.HasMetrics && previous.HasMetrics {
		current.CPUMilli = previous.CPUMilli
		current.MemoryBytes = previous.MemoryBytes
		current.HasMetrics = true
	}
	if !current.HasTemperature && previous.HasTemperature {
		current.TemperatureC = previous.TemperatureC
		current.HasTemperature = true
	}
	if !current.HasPods && previous.HasPods {
		current.PodsReady = previous.PodsReady
		current.PodsTotal = previous.PodsTotal
		current.PodsBad = append([]string(nil), previous.PodsBad...)
		current.HasPods = true
		current.PodsStale = true
	}
	if !current.HasCluster && previous.HasCluster {
		current.ClusterReady = previous.ClusterReady
		current.ClusterTotal = previous.ClusterTotal
		current.HasCluster = true
		current.ClusterStale = true
	}
	return current
}

func durationEnv(name string, fallback, minimum time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	if duration < minimum {
		return 0, fmt.Errorf("%s must be at least %s", name, minimum)
	}
	return duration, nil
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func fatalf(format string, arguments ...any) {
	log.Printf("fatal: "+format, arguments...)
	os.Exit(1)
}
