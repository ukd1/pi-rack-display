package screen

import (
	"bytes"
	"image"
	"image/png"
	"testing"
	"time"

	"github.com/ukd1/pi-rack-display/internal/display"
	"github.com/ukd1/pi-rack-display/internal/model"
)

func TestRenderPages(t *testing.T) {
	snapshot := model.Snapshot{
		CollectedAt:      time.Date(2026, 8, 7, 12, 34, 0, 0, time.UTC),
		NodeName:         "alols",
		NodeIP:           "192.168.1.210",
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
