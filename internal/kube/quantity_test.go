package kube

import "testing"

func TestParseCPU(t *testing.T) {
	tests := map[string]int64{
		"1":          1000,
		"1500m":      1500,
		"250000000n": 250,
		"750000u":    750,
	}
	for input, want := range tests {
		got, err := parseCPU(input)
		if err != nil {
			t.Errorf("parseCPU(%q): %v", input, err)
		} else if got != want {
			t.Errorf("parseCPU(%q) = %d, want %d", input, got, want)
		}
	}
}

func TestParseBytes(t *testing.T) {
	tests := map[string]int64{
		"1024": 1024,
		"1Ki":  1024,
		"2Mi":  2 * 1024 * 1024,
		"1.5G": 1500000000,
	}
	for input, want := range tests {
		got, err := parseBytes(input)
		if err != nil {
			t.Errorf("parseBytes(%q): %v", input, err)
		} else if got != want {
			t.Errorf("parseBytes(%q) = %d, want %d", input, got, want)
		}
	}
}
