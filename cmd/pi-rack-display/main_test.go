package main

import (
	"testing"

	"github.com/ukd1/pi-rack-display/internal/model"
)

func TestMergeSnapshotKeepsIndependentLastGoodValues(t *testing.T) {
	previous := model.Snapshot{
		NodeName:         "alols",
		NodeIP:           "192.168.1.210",
		Ready:            true,
		HasNode:          true,
		CPUMilli:         500,
		CPUCapacityMilli: 4000,
		HasMetrics:       true,
		PodsReady:        7,
		PodsTotal:        7,
		HasPods:          true,
	}
	current := model.Snapshot{NodeName: "alols", Warnings: []string{"metrics API unavailable"}}
	merged := mergeSnapshot(previous, current)
	if !merged.HasNode || !merged.Ready || merged.NodeIP != previous.NodeIP {
		t.Errorf("node data was not retained: %+v", merged)
	}
	if !merged.HasMetrics || merged.CPUMilli != previous.CPUMilli {
		t.Errorf("metrics were not retained: %+v", merged)
	}
	if !merged.HasPods || merged.PodsReady != previous.PodsReady {
		t.Errorf("pod data was not retained: %+v", merged)
	}
	if len(merged.Warnings) != 1 {
		t.Errorf("current warnings were not retained: %+v", merged.Warnings)
	}
}
