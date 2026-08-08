package screen

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/ukd1/pi-rack-display/internal/display"
	"github.com/ukd1/pi-rack-display/internal/model"
)

func TestRenderPages(t *testing.T) {
	snapshot := healthySnapshot()
	var previous []byte
	for page := 0; page < PageCount; page++ {
		frame := Render(page, snapshot)
		if got := frame.Bounds(); got != image.Rect(0, 0, display.Width, display.Height) {
			t.Fatalf("page %d bounds = %v", page, got)
		}
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, frame); err != nil {
			t.Fatal(err)
		}
		if page > 0 && bytes.Equal(previous, encoded.Bytes()) {
			t.Fatalf("page %d is identical to the previous page", page)
		}
		previous = bytes.Clone(encoded.Bytes())
	}
}

func TestHeaderIsPermanentAcrossPages(t *testing.T) {
	snapshot := healthySnapshot()
	first := Render(0, snapshot)
	second := Render(1, snapshot)
	if !samePixels(first, second, image.Rect(0, 0, display.Width, headerHeight)) {
		t.Fatal("header differs between pages")
	}
	if samePixels(first, second, image.Rect(0, headerHeight, display.Width, display.Height)) {
		t.Fatal("page bodies are identical")
	}
}

func TestPageNumberNormalization(t *testing.T) {
	snapshot := healthySnapshot()
	if !samePixels(Render(-1, snapshot), Render(PageCount-1, snapshot), image.Rect(0, 0, display.Width, display.Height)) {
		t.Fatal("negative page did not wrap to the final page")
	}
	if !samePixels(Render(PageCount, snapshot), Render(0, snapshot), image.Rect(0, 0, display.Width, display.Height)) {
		t.Fatal("page count did not wrap to the first page")
	}
}

func TestClusterSummary(t *testing.T) {
	tests := []struct {
		name     string
		snapshot model.Snapshot
		label    string
		colour   color.Color
	}{
		{name: "unknown", snapshot: model.Snapshot{}, label: "NODES --/--", colour: yellow},
		{name: "healthy", snapshot: model.Snapshot{HasCluster: true, ClusterReady: 4, ClusterTotal: 4}, label: "NODES 4/4", colour: green},
		{name: "stale", snapshot: model.Snapshot{HasCluster: true, ClusterReady: 4, ClusterTotal: 4, ClusterStale: true}, label: "NODES 4/4", colour: yellow},
		{name: "degraded", snapshot: model.Snapshot{HasCluster: true, ClusterReady: 3, ClusterTotal: 4}, label: "NODES 3/4", colour: yellow},
		{name: "down", snapshot: model.Snapshot{HasCluster: true, ClusterReady: 0, ClusterTotal: 4}, label: "NODES 0/4", colour: red},
		{name: "empty", snapshot: model.Snapshot{HasCluster: true}, label: "NODES 0/0", colour: yellow},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			label, colour := clusterSummary(test.snapshot)
			if label != test.label || !sameColor(colour, test.colour) {
				t.Fatalf("clusterSummary() = %q %v, want %q %v", label, colour, test.label, test.colour)
			}
		})
	}
}

func TestPodSummary(t *testing.T) {
	tests := []struct {
		name     string
		snapshot model.Snapshot
		count    string
		status   string
		colour   color.Color
	}{
		{name: "unknown", snapshot: model.Snapshot{}, count: "PODS --", status: "UNKNOWN", colour: yellow},
		{name: "healthy", snapshot: model.Snapshot{HasPods: true, PodsReady: 24, PodsTotal: 24}, count: "PODS 24", status: "READY", colour: green},
		{name: "stale", snapshot: model.Snapshot{HasPods: true, PodsReady: 24, PodsTotal: 24, PodsStale: true}, count: "PODS 24", status: "STALE", colour: yellow},
		{name: "degraded", snapshot: model.Snapshot{HasPods: true, PodsReady: 23, PodsTotal: 24}, count: "PODS 24", status: "1 UNREADY", colour: yellow},
		{name: "down", snapshot: model.Snapshot{HasPods: true, PodsReady: 0, PodsTotal: 4}, count: "PODS 4", status: "4 UNREADY", colour: red},
		{name: "empty", snapshot: model.Snapshot{HasPods: true}, count: "PODS 0", status: "NONE", colour: yellow},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			count, status, colour := podSummary(test.snapshot)
			if count != test.count || status != test.status || !sameColor(colour, test.colour) {
				t.Fatalf("podSummary() = %q %q %v, want %q %q %v", count, status, colour, test.count, test.status, test.colour)
			}
		})
	}
}

func TestNodeStatusColor(t *testing.T) {
	tests := []struct {
		name     string
		snapshot model.Snapshot
		colour   color.Color
	}{
		{name: "unknown", snapshot: model.Snapshot{}, colour: yellow},
		{name: "healthy", snapshot: model.Snapshot{HasNode: true, Ready: true}, colour: green},
		{name: "cordoned", snapshot: model.Snapshot{HasNode: true, Ready: true, Cordoned: true}, colour: yellow},
		{name: "stale", snapshot: model.Snapshot{HasNode: true, Ready: true, NodeStale: true}, colour: yellow},
		{name: "not ready", snapshot: model.Snapshot{HasNode: true}, colour: red},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := nodeStatusColor(test.snapshot); !sameColor(got, test.colour) {
				t.Fatalf("nodeStatusColor() = %v, want %v", got, test.colour)
			}
		})
	}
}

func TestWorstCaseHeaderAndPodLabelsFit(t *testing.T) {
	snapshot := model.Snapshot{
		NodeName:     strings.Repeat("very-long-node-", 4),
		HasNode:      true,
		Ready:        true,
		Cordoned:     true,
		HasCluster:   true,
		ClusterReady: 100,
		ClusterTotal: 100,
		HasPods:      true,
		PodsTotal:    1000,
	}
	clusterLabel, _ := clusterSummary(snapshot)
	clusterLeft := rightEdge - textWidth(clusterLabel)
	const nodeX = 13
	badgeWidth := textWidth("C") + 3
	nodeName := truncatePixels(snapshot.NodeName, clusterLeft-nodeX-4-badgeWidth)
	leftGroupRight := nodeX + textWidth(nodeName) + badgeWidth
	if leftGroupRight > clusterLeft-4 {
		t.Fatalf("header groups overlap: left ends at %d, cluster starts at %d", leftGroupRight, clusterLeft)
	}
	count, status, _ := podSummary(snapshot)
	if textWidth(count)+textWidth(status)+10 > display.Width {
		t.Fatalf("pod labels do not fit: %q and %q", count, status)
	}
	_ = Render(0, snapshot)
}

func TestTruncatePixels(t *testing.T) {
	const maximum = 70
	got := truncatePixels("a-very-long-node-name", maximum)
	if textWidth(got) > maximum {
		t.Fatalf("truncated width = %d, want <= %d", textWidth(got), maximum)
	}
	if !strings.HasSuffix(got, "~") {
		t.Fatalf("truncated value %q does not have a suffix", got)
	}
	if got := truncatePixels("alols", maximum); got != "alols" {
		t.Fatalf("short value = %q", got)
	}
}

func healthySnapshot() model.Snapshot {
	return model.Snapshot{
		CollectedAt:      time.Date(2026, 8, 7, 12, 34, 0, 0, time.UTC),
		NodeName:         "alols",
		NodeIP:           "10.0.0.210",
		Ready:            true,
		HasNode:          true,
		CPUMilli:         1000,
		CPUCapacityMilli: 4000,
		MemoryBytes:      2 << 30,
		MemoryCapacity:   8 << 30,
		HasMetrics:       true,
		TemperatureC:     52.5,
		HasTemperature:   true,
		PodsReady:        8,
		PodsTotal:        9,
		PodsBad:          []string{"default/example"},
		HasPods:          true,
		ClusterReady:     4,
		ClusterTotal:     4,
		HasCluster:       true,
	}
}

func samePixels(left, right *image.RGBA, rectangle image.Rectangle) bool {
	for y := rectangle.Min.Y; y < rectangle.Max.Y; y++ {
		for x := rectangle.Min.X; x < rectangle.Max.X; x++ {
			if left.RGBAAt(x, y) != right.RGBAAt(x, y) {
				return false
			}
		}
	}
	return true
}

func sameColor(left, right color.Color) bool {
	lr, lg, lb, la := left.RGBA()
	rr, rg, rb, ra := right.RGBA()
	return lr == rr && lg == rg && lb == rb && la == ra
}
