package kube

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (function roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestCollectParsesNodePodsMetricsClusterAndTemperature(t *testing.T) {
	tokenPath := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenPath, []byte("test-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	temperaturePath := filepath.Join(t.TempDir(), "temp")
	if err := os.WriteFile(temperaturePath, []byte("52500\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	transport := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if got := request.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q", got)
		}
		body := ""
		status := http.StatusOK
		switch request.URL.Path {
		case "/api/v1/nodes/alols":
			body = `{
              "spec":{"unschedulable":true},
              "status":{
                "addresses":[{"type":"InternalIP","address":"10.0.0.210"}],
                "conditions":[{"type":"Ready","status":"True"}],
                "allocatable":{"cpu":"4","memory":"8Gi"}
              }
            }`
		case "/api/v1/pods":
			if got := request.URL.Query().Get("fieldSelector"); got != "spec.nodeName=alols" {
				t.Errorf("fieldSelector = %q", got)
			}
			body = `{"items":[
              {"metadata":{"namespace":"default","name":"ready"},"status":{"phase":"Running","conditions":[{"type":"Ready","status":"True"}]}},
              {"metadata":{"namespace":"default","name":"pending"},"status":{"phase":"Pending","conditions":[]}},
              {"metadata":{"namespace":"default","name":"done"},"status":{"phase":"Succeeded","conditions":[]}}
            ]}`
		case "/apis/metrics.k8s.io/v1beta1/nodes/alols":
			body = `{"usage":{"cpu":"1000000000n","memory":"2Gi"}}`
		case "/api/v1/nodes":
			body = `{"items":[
              {"status":{"conditions":[{"type":"Ready","status":"True"}]}},
              {"status":{"conditions":[{"type":"Ready","status":"False"}]}}
            ]}`
		default:
			status = http.StatusNotFound
			body = "not found"
		}
		return &http.Response{
			StatusCode: status,
			Status:     http.StatusText(status),
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    request,
		}, nil
	})

	client := &Client{baseURL: "https://kubernetes.test", tokenPath: tokenPath, http: &http.Client{Transport: transport}}
	snapshot := client.Collect(context.Background(), "alols", "127.0.0.1", temperaturePath)
	if len(snapshot.Warnings) != 0 {
		t.Fatalf("warnings = %v", snapshot.Warnings)
	}
	if !snapshot.HasNode || !snapshot.Ready || !snapshot.Cordoned || snapshot.NodeIP != "10.0.0.210" {
		t.Errorf("node snapshot = %+v", snapshot)
	}
	if !snapshot.HasMetrics || snapshot.CPUPercent() != 25 || snapshot.MemoryPercent() != 25 {
		t.Errorf("usage snapshot = %+v", snapshot)
	}
	if !snapshot.HasTemperature || snapshot.TemperatureC != 52.5 {
		t.Errorf("temperature = %.1f available=%v", snapshot.TemperatureC, snapshot.HasTemperature)
	}
	if !snapshot.HasPods || snapshot.PodsReady != 1 || snapshot.PodsTotal != 2 || len(snapshot.PodsBad) != 1 {
		t.Errorf("pod snapshot = %+v", snapshot)
	}
	if !snapshot.HasCluster || snapshot.ClusterReady != 1 || snapshot.ClusterTotal != 2 {
		t.Errorf("cluster snapshot = %+v", snapshot)
	}
}
