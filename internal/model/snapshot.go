package model

import "time"

// Snapshot is the small, display-oriented view of a node and its cluster.
type Snapshot struct {
	CollectedAt time.Time
	NodeName    string
	NodeIP      string
	Ready       bool
	Cordoned    bool
	HasNode     bool
	NodeStale   bool

	CPUMilli         int64
	CPUCapacityMilli int64
	MemoryBytes      int64
	MemoryCapacity   int64
	HasMetrics       bool
	TemperatureC     float64
	HasTemperature   bool

	PodsReady int
	PodsTotal int
	PodsBad   []string
	HasPods   bool
	PodsStale bool

	ClusterReady int
	ClusterTotal int
	HasCluster   bool
	ClusterStale bool

	Warnings []string
}

func (s Snapshot) CPUPercent() int {
	if s.CPUCapacityMilli <= 0 {
		return 0
	}
	return clampPercent(s.CPUMilli * 100 / s.CPUCapacityMilli)
}

func (s Snapshot) MemoryPercent() int {
	if s.MemoryCapacity <= 0 {
		return 0
	}
	return clampPercent(s.MemoryBytes * 100 / s.MemoryCapacity)
}

func clampPercent(value int64) int {
	if value < 0 {
		return 0
	}
	if value > 999 {
		return 999
	}
	return int(value)
}
