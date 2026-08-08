package deviceplugin

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"google.golang.org/grpc"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

type registrationServer struct {
	pluginapi.UnimplementedRegistrationServer
	registrations atomic.Int32
}

func (server *registrationServer) Register(context.Context, *pluginapi.RegisterRequest) (*pluginapi.Empty, error) {
	server.registrations.Add(1)
	return &pluginapi.Empty{}, nil
}

func TestAllocate(t *testing.T) {
	server := New(pluginapi.DevicePluginPath, DefaultDevicePath, DefaultThermalHostPath, DefaultThermalContainerPath)
	response, err := server.Allocate(context.Background(), &pluginapi.AllocateRequest{
		ContainerRequests: []*pluginapi.ContainerAllocateRequest{{DevicesIds: []string{DeviceID}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(response.ContainerResponses); got != 1 {
		t.Fatalf("container responses = %d", got)
	}
	container := response.ContainerResponses[0]
	if got := container.Devices[0].Permissions; got != "rw" {
		t.Errorf("device permissions = %q", got)
	}
	if !container.Mounts[0].ReadOnly {
		t.Error("thermal mount is not read-only")
	}
}

func TestAllocateRejectsUnknownDevice(t *testing.T) {
	server := New(pluginapi.DevicePluginPath, DefaultDevicePath, DefaultThermalHostPath, DefaultThermalContainerPath)
	_, err := server.Allocate(context.Background(), &pluginapi.AllocateRequest{
		ContainerRequests: []*pluginapi.ContainerAllocateRequest{{DevicesIds: []string{"unknown"}}},
	})
	if err == nil {
		t.Fatal("Allocate returned nil error for an unknown device")
	}
}

func TestHealthyRequiresCharacterDeviceAndTemperature(t *testing.T) {
	directory := t.TempDir()
	regularFile := filepath.Join(directory, "device")
	temperature := filepath.Join(directory, "temp")
	if err := os.WriteFile(regularFile, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(temperature, []byte("50000"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := New(directory, regularFile, temperature, DefaultThermalContainerPath)
	if server.healthy() {
		t.Fatal("regular file was accepted as an I2C character device")
	}
}

func TestRunRecreatesRemovedPluginSocket(t *testing.T) {
	directory, err := os.MkdirTemp("/tmp", "dp-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(directory)
	kubeletSocket := filepath.Join(directory, filepath.Base(pluginapi.KubeletSocket))
	listener, err := net.Listen("unix", kubeletSocket)
	if err != nil {
		if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
			t.Skipf("sandbox does not allow Unix sockets: %v", err)
		}
		t.Fatal(err)
	}
	grpcServer := grpc.NewServer()
	registrations := &registrationServer{}
	pluginapi.RegisterRegistrationServer(grpcServer, registrations)
	go grpcServer.Serve(listener)
	defer grpcServer.Stop()

	server := New(directory, DefaultDevicePath, DefaultThermalHostPath, DefaultThermalContainerPath)
	server.pollInterval = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx) }()

	waitFor(t, 2*time.Second, func() bool { return registrations.registrations.Load() >= 1 })
	if err := os.Remove(filepath.Join(directory, pluginSocketName)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, func() bool { return registrations.registrations.Load() >= 2 })

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("device plugin did not stop")
	}
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition was not met before timeout")
}
