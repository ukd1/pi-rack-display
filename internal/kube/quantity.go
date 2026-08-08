package kube

import (
	"fmt"
	"strconv"
	"strings"
)

func parseCPU(value string) (int64, error) {
	if value == "" {
		return 0, fmt.Errorf("empty CPU quantity")
	}
	multipliers := []struct {
		suffix     string
		multiplier float64
	}{
		{"n", 0.000001},
		{"u", 0.001},
		{"m", 1},
	}
	for _, item := range multipliers {
		if strings.HasSuffix(value, item.suffix) {
			number, err := parseNumber(strings.TrimSuffix(value, item.suffix))
			return int64(number * item.multiplier), err
		}
	}
	number, err := parseNumber(value)
	return int64(number * 1000), err
}

func parseBytes(value string) (int64, error) {
	if value == "" {
		return 0, fmt.Errorf("empty memory quantity")
	}
	multipliers := []struct {
		suffix     string
		multiplier float64
	}{
		{"Ei", 1 << 60}, {"Pi", 1 << 50}, {"Ti", 1 << 40},
		{"Gi", 1 << 30}, {"Mi", 1 << 20}, {"Ki", 1 << 10},
		{"E", 1e18}, {"P", 1e15}, {"T", 1e12},
		{"G", 1e9}, {"M", 1e6}, {"K", 1e3},
	}
	for _, item := range multipliers {
		if strings.HasSuffix(value, item.suffix) {
			number, err := parseNumber(strings.TrimSuffix(value, item.suffix))
			return int64(number * item.multiplier), err
		}
	}
	number, err := parseNumber(value)
	return int64(number), err
}

func parseNumber(value string) (float64, error) {
	number, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %q: %w", value, err)
	}
	return number, nil
}
