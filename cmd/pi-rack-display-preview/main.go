package main

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"time"

	"github.com/ukd1/pi-rack-display/internal/model"
	"github.com/ukd1/pi-rack-display/internal/screen"
)

func main() {
	output := flag.String("output", ".", "directory for rendered PNG files")
	flag.Parse()
	if err := os.MkdirAll(*output, 0o755); err != nil {
		fatal(err)
	}

	scenarios := []struct {
		name     string
		snapshot model.Snapshot
	}{
		{name: "healthy", snapshot: healthySnapshot()},
		{name: "degraded", snapshot: degradedSnapshot()},
	}
	pageNames := []string{"pods", "usage"}
	for _, scenario := range scenarios {
		for page := 0; page < screen.PageCount; page++ {
			fileName := filepath.Join(*output, scenario.name+"-"+pageNames[page]+".png")
			if err := writePNG(fileName, screen.Render(page, scenario.snapshot)); err != nil {
				fatal(err)
			}
			fmt.Println(fileName)
		}
	}
}

func healthySnapshot() model.Snapshot {
	return model.Snapshot{
		CollectedAt:      time.Date(2026, 8, 8, 5, 7, 0, 0, time.UTC),
		NodeName:         "alols",
		NodeIP:           "10.0.0.210",
		Ready:            true,
		HasNode:          true,
		CPUMilli:         680,
		CPUCapacityMilli: 4000,
		MemoryBytes:      6_442_450_944,
		MemoryCapacity:   8_589_934_592,
		HasMetrics:       true,
		TemperatureC:     52.5,
		HasTemperature:   true,
		PodsReady:        24,
		PodsTotal:        24,
		HasPods:          true,
		ClusterReady:     4,
		ClusterTotal:     4,
		HasCluster:       true,
	}
}

func degradedSnapshot() model.Snapshot {
	return model.Snapshot{
		CollectedAt:      time.Date(2026, 8, 8, 5, 7, 0, 0, time.UTC),
		NodeName:         "long-rack-node-name",
		NodeIP:           "10.0.0.212",
		Ready:            true,
		Cordoned:         true,
		HasNode:          true,
		CPUMilli:         3720,
		CPUCapacityMilli: 4000,
		MemoryBytes:      7_730_941_133,
		MemoryCapacity:   8_589_934_592,
		HasMetrics:       true,
		TemperatureC:     81.5,
		HasTemperature:   true,
		PodsReady:        21,
		PodsTotal:        24,
		PodsBad: []string{
			"default/prometheus-server-7b76845559-bgghs",
			"kube-system/metrics-server-7c5b56f87d-c8vjk",
		},
		HasPods:      true,
		ClusterReady: 3,
		ClusterTotal: 4,
		HasCluster:   true,
		Warnings:     []string{"metrics API unavailable"},
	}
}

func writePNG(fileName string, frame image.Image) error {
	file, err := os.Create(fileName)
	if err != nil {
		return err
	}
	if err := png.Encode(file, frame); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
