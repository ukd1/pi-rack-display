package kube

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/ukd1/pi-rack-display/internal/model"
)

const (
	serviceAccountToken = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	serviceAccountCA    = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
)

type Client struct {
	baseURL   string
	tokenPath string
	http      *http.Client
}

func NewInCluster() (*Client, error) {
	host := os.Getenv("KUBERNETES_SERVICE_HOST")
	port := os.Getenv("KUBERNETES_SERVICE_PORT_HTTPS")
	if port == "" {
		port = os.Getenv("KUBERNETES_SERVICE_PORT")
	}
	if host == "" || port == "" {
		return nil, errors.New("kube: KUBERNETES_SERVICE_HOST and service port are required")
	}
	ca, err := os.ReadFile(serviceAccountCA)
	if err != nil {
		return nil, fmt.Errorf("kube: read service account CA: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, errors.New("kube: service account CA contains no certificates")
	}
	return &Client{
		baseURL:   "https://" + netJoinHostPort(host, port),
		tokenPath: serviceAccountToken,
		http: &http.Client{
			Timeout: 6 * time.Second,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
				RootCAs:    roots,
			}},
		},
	}, nil
}

func netJoinHostPort(host, port string) string {
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		return "[" + host + "]:" + port
	}
	return host + ":" + port
}

// Collect obtains independent snapshots. A failed endpoint leaves only that
// datum unknown and is returned as a displayable warning.
func (c *Client) Collect(ctx context.Context, nodeName, fallbackIP, temperaturePath string) model.Snapshot {
	snapshot := model.Snapshot{
		CollectedAt: time.Now(),
		NodeName:    nodeName,
		NodeIP:      fallbackIP,
	}

	var node nodeResponse
	if err := c.getJSON(ctx, "/api/v1/nodes/"+url.PathEscape(nodeName), &node); err != nil {
		snapshot.Warnings = append(snapshot.Warnings, "node API: "+err.Error())
	} else {
		applyNode(&snapshot, node)
		snapshot.HasNode = true
	}

	var pods podListResponse
	selector := url.QueryEscape("spec.nodeName=" + nodeName)
	if err := c.getJSON(ctx, "/api/v1/pods?fieldSelector="+selector, &pods); err != nil {
		snapshot.Warnings = append(snapshot.Warnings, "pod API: "+err.Error())
	} else {
		applyPods(&snapshot, pods)
		snapshot.HasPods = true
	}

	var metrics nodeMetricsResponse
	metricsPath := "/apis/metrics.k8s.io/v1beta1/nodes/" + url.PathEscape(nodeName)
	if err := c.getJSON(ctx, metricsPath, &metrics); err != nil {
		snapshot.Warnings = append(snapshot.Warnings, "metrics API: "+err.Error())
	} else {
		cpu, cpuErr := parseCPU(metrics.Usage.CPU)
		memory, memoryErr := parseBytes(metrics.Usage.Memory)
		if cpuErr != nil || memoryErr != nil {
			snapshot.Warnings = append(snapshot.Warnings, "metrics API: invalid resource quantity")
		} else {
			snapshot.CPUMilli = cpu
			snapshot.MemoryBytes = memory
			snapshot.HasMetrics = true
		}
	}

	var nodes nodeListResponse
	if err := c.getJSON(ctx, "/api/v1/nodes", &nodes); err != nil {
		snapshot.Warnings = append(snapshot.Warnings, "cluster API: "+err.Error())
	} else {
		snapshot.HasCluster = true
		snapshot.ClusterTotal = len(nodes.Items)
		for _, item := range nodes.Items {
			if nodeReady(item.Status.Conditions) {
				snapshot.ClusterReady++
			}
		}
	}

	if temperature, err := readTemperature(temperaturePath); err != nil {
		snapshot.Warnings = append(snapshot.Warnings, "temperature: "+err.Error())
	} else {
		snapshot.TemperatureC = temperature
		snapshot.HasTemperature = true
	}
	return snapshot
}

func (c *Client) getJSON(ctx context.Context, requestPath string, destination any) error {
	token, err := os.ReadFile(c.tokenPath)
	if err != nil {
		return fmt.Errorf("read token: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+requestPath, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	request.Header.Set("Accept", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return fmt.Errorf("%s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(destination); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

type nodeResponse struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		Unschedulable bool `json:"unschedulable"`
	} `json:"spec"`
	Status struct {
		Addresses   []nodeAddress   `json:"addresses"`
		Conditions  []nodeCondition `json:"conditions"`
		Capacity    resourceList    `json:"capacity"`
		Allocatable resourceList    `json:"allocatable"`
	} `json:"status"`
}

type nodeListResponse struct {
	Items []nodeResponse `json:"items"`
}

type nodeAddress struct {
	Type    string `json:"type"`
	Address string `json:"address"`
}

type nodeCondition struct {
	Type   string `json:"type"`
	Status string `json:"status"`
}

type resourceList struct {
	CPU    string `json:"cpu"`
	Memory string `json:"memory"`
}

type podListResponse struct {
	Items []struct {
		Metadata struct {
			Name              string  `json:"name"`
			Namespace         string  `json:"namespace"`
			DeletionTimestamp *string `json:"deletionTimestamp"`
		} `json:"metadata"`
		Status struct {
			Phase      string         `json:"phase"`
			Conditions []podCondition `json:"conditions"`
		} `json:"status"`
	} `json:"items"`
}

type podCondition struct {
	Type   string `json:"type"`
	Status string `json:"status"`
}

type nodeMetricsResponse struct {
	Usage resourceList `json:"usage"`
}

func applyNode(snapshot *model.Snapshot, node nodeResponse) {
	snapshot.Ready = nodeReady(node.Status.Conditions)
	snapshot.Cordoned = node.Spec.Unschedulable
	for _, address := range node.Status.Addresses {
		if address.Type == "InternalIP" {
			snapshot.NodeIP = address.Address
			break
		}
	}
	capacity := node.Status.Allocatable
	if capacity.CPU == "" {
		capacity = node.Status.Capacity
	}
	snapshot.CPUCapacityMilli, _ = parseCPU(capacity.CPU)
	snapshot.MemoryCapacity, _ = parseBytes(capacity.Memory)
}

func applyPods(snapshot *model.Snapshot, pods podListResponse) {
	for _, pod := range pods.Items {
		if pod.Metadata.DeletionTimestamp != nil || pod.Status.Phase == "Succeeded" || pod.Status.Phase == "Failed" {
			continue
		}
		snapshot.PodsTotal++
		ready := false
		for _, condition := range pod.Status.Conditions {
			if condition.Type == "Ready" && condition.Status == "True" {
				ready = true
				break
			}
		}
		if ready {
			snapshot.PodsReady++
		} else if len(snapshot.PodsBad) < 3 {
			snapshot.PodsBad = append(snapshot.PodsBad, path.Join(pod.Metadata.Namespace, pod.Metadata.Name))
		}
	}
}

func nodeReady(conditions []nodeCondition) bool {
	for _, condition := range conditions {
		if condition.Type == "Ready" {
			return condition.Status == "True"
		}
	}
	return false
}

func readTemperature(filePath string) (float64, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return 0, err
	}
	value, err := parseNumber(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, err
	}
	if value > 1000 {
		value /= 1000
	}
	return value, nil
}
