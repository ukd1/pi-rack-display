package deviceplugin

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

const (
	ResourceName                = "uctronics.com/rm0004"
	DeviceID                    = "display0"
	DefaultDevicePath           = "/dev/i2c-1"
	DefaultThermalHostPath      = "/sys/class/thermal/thermal_zone0/temp"
	DefaultThermalContainerPath = "/host/thermal/temp"
	pluginSocketName            = "uctronics-rm0004.sock"
)

type Server struct {
	pluginapi.UnimplementedDevicePluginServer

	pluginDirectory      string
	devicePath           string
	thermalHostPath      string
	thermalContainerPath string

	mu           sync.Mutex
	grpcServer   *grpc.Server
	listener     net.Listener
	serveErrors  chan error
	pollInterval time.Duration
}

func New(pluginDirectory, devicePath, thermalHostPath, thermalContainerPath string) *Server {
	return &Server{
		pluginDirectory:      pluginDirectory,
		devicePath:           devicePath,
		thermalHostPath:      thermalHostPath,
		thermalContainerPath: thermalContainerPath,
		serveErrors:          make(chan error, 1),
		pollInterval:         5 * time.Second,
	}
}

func (server *Server) Run(ctx context.Context) error {
	if err := server.start(); err != nil {
		return err
	}
	defer server.stop()

	var registeredAgainst os.FileInfo
	retry := time.NewTicker(server.pollInterval)
	defer retry.Stop()
	for {
		if _, err := os.Stat(filepath.Join(server.pluginDirectory, pluginSocketName)); err != nil {
			server.stop()
			if err := server.start(); err != nil {
				return err
			}
			registeredAgainst = nil
		}
		kubeletInfo, err := os.Stat(filepath.Join(server.pluginDirectory, filepath.Base(pluginapi.KubeletSocket)))
		needsRegistration := err == nil && (registeredAgainst == nil || !os.SameFile(registeredAgainst, kubeletInfo))
		if needsRegistration {
			registerContext, cancel := context.WithTimeout(ctx, 5*time.Second)
			err = server.register(registerContext)
			cancel()
			if err == nil {
				registeredAgainst = kubeletInfo
				log.Printf("device plugin registered resource=%s", ResourceName)
			} else {
				log.Printf("device plugin registration pending: %v", err)
			}
		}
		if registeredAgainst != nil {
			select {
			case <-ctx.Done():
				return nil
			case err := <-server.serveErrors:
				server.stop()
				if restartErr := server.start(); restartErr != nil {
					return fmt.Errorf("device plugin: recover after serve error %v: %w", err, restartErr)
				}
				registeredAgainst = nil
			case <-retry.C:
			}
			continue
		}
		select {
		case <-ctx.Done():
			return nil
		case err := <-server.serveErrors:
			server.stop()
			if restartErr := server.start(); restartErr != nil {
				return fmt.Errorf("device plugin: recover after serve error %v: %w", err, restartErr)
			}
		case <-retry.C:
		}
	}
}

func (server *Server) start() error {
	socketPath := filepath.Join(server.pluginDirectory, pluginSocketName)
	if err := os.Remove(socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("device plugin: remove stale socket: %w", err)
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("device plugin: listen on %s: %w", socketPath, err)
	}
	grpcServer := grpc.NewServer()
	pluginapi.RegisterDevicePluginServer(grpcServer, server)
	server.mu.Lock()
	server.listener = listener
	server.grpcServer = grpcServer
	server.mu.Unlock()
	go func() {
		if err := grpcServer.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			select {
			case server.serveErrors <- err:
			default:
			}
		}
	}()
	return nil
}

func (server *Server) stop() {
	server.mu.Lock()
	grpcServer := server.grpcServer
	listener := server.listener
	server.grpcServer = nil
	server.listener = nil
	server.mu.Unlock()
	if grpcServer != nil {
		grpcServer.Stop()
	}
	if listener != nil {
		_ = listener.Close()
	}
	_ = os.Remove(filepath.Join(server.pluginDirectory, pluginSocketName))
}

func (server *Server) register(ctx context.Context) error {
	kubeletSocket := filepath.Join(server.pluginDirectory, filepath.Base(pluginapi.KubeletSocket))
	connection, err := grpc.DialContext(
		ctx,
		"unix://"+kubeletSocket,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", kubeletSocket)
		}),
	)
	if err != nil {
		return fmt.Errorf("device plugin: connect to kubelet: %w", err)
	}
	defer connection.Close()
	client := pluginapi.NewRegistrationClient(connection)
	_, err = client.Register(ctx, &pluginapi.RegisterRequest{
		Version:      pluginapi.Version,
		Endpoint:     pluginSocketName,
		ResourceName: ResourceName,
		Options:      &pluginapi.DevicePluginOptions{},
	})
	if err != nil {
		return fmt.Errorf("device plugin: register %s: %w", ResourceName, err)
	}
	return nil
}

func (server *Server) GetDevicePluginOptions(context.Context, *pluginapi.Empty) (*pluginapi.DevicePluginOptions, error) {
	return &pluginapi.DevicePluginOptions{}, nil
}

func (server *Server) ListAndWatch(_ *pluginapi.Empty, stream pluginapi.DevicePlugin_ListAndWatchServer) error {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	lastHealth := ""
	for {
		health := pluginapi.Unhealthy
		if server.healthy() {
			health = pluginapi.Healthy
		}
		if health != lastHealth {
			response := &pluginapi.ListAndWatchResponse{Devices: []*pluginapi.Device{{ID: DeviceID, Health: health}}}
			if err := stream.Send(response); err != nil {
				return err
			}
			lastHealth = health
			log.Printf("device plugin health resource=%s health=%s", ResourceName, health)
		}
		select {
		case <-stream.Context().Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (server *Server) Allocate(_ context.Context, request *pluginapi.AllocateRequest) (*pluginapi.AllocateResponse, error) {
	response := &pluginapi.AllocateResponse{}
	for _, containerRequest := range request.ContainerRequests {
		if len(containerRequest.DevicesIds) != 1 || containerRequest.DevicesIds[0] != DeviceID {
			return nil, fmt.Errorf("device plugin: expected only device %q, got %v", DeviceID, containerRequest.DevicesIds)
		}
		response.ContainerResponses = append(response.ContainerResponses, &pluginapi.ContainerAllocateResponse{
			Devices: []*pluginapi.DeviceSpec{{
				ContainerPath: server.devicePath,
				HostPath:      server.devicePath,
				Permissions:   "rw",
			}},
			Mounts: []*pluginapi.Mount{{
				ContainerPath: server.thermalContainerPath,
				HostPath:      server.thermalHostPath,
				ReadOnly:      true,
			}},
		})
	}
	return response, nil
}

func (server *Server) healthy() bool {
	device, err := os.Stat(server.devicePath)
	if err != nil || device.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	_, err = os.Stat(server.thermalHostPath)
	return err == nil
}
