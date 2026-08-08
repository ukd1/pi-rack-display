package screen

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"strings"

	"github.com/ukd1/pi-rack-display/internal/display"
	"github.com/ukd1/pi-rack-display/internal/model"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

const (
	PageCount    = 2
	headerHeight = 16
	rightEdge    = display.Width - 3
)

var (
	black    = color.RGBA{0x00, 0x00, 0x00, 0xff}
	white    = color.RGBA{0xf5, 0xf7, 0xfa, 0xff}
	dim      = color.RGBA{0x92, 0xa2, 0xb6, 0xff}
	green    = color.RGBA{0x48, 0xd5, 0x97, 0xff}
	yellow   = color.RGBA{0xff, 0xcc, 0x66, 0xff}
	red      = color.RGBA{0xff, 0x5d, 0x73, 0xff}
	header   = color.RGBA{0x0c, 0x2d, 0x48, 0xff}
	barTrack = color.RGBA{0x22, 0x2d, 0x3a, 0xff}
)

func Render(pageNumber int, snapshot model.Snapshot) *image.RGBA {
	pageNumber = ((pageNumber % PageCount) + PageCount) % PageCount
	frame := image.NewRGBA(image.Rect(0, 0, display.Width, display.Height))
	draw.Draw(frame, frame.Bounds(), &image.Uniform{black}, image.Point{}, draw.Src)
	renderHeader(frame, snapshot)

	switch pageNumber {
	case 0:
		renderPods(frame, snapshot)
	case 1:
		renderUsage(frame, snapshot)
	}
	return frame
}

func renderHeader(frame *image.RGBA, snapshot model.Snapshot) {
	draw.Draw(frame, image.Rect(0, 0, display.Width, headerHeight), &image.Uniform{header}, image.Point{}, draw.Src)
	draw.Draw(frame, image.Rect(0, headerHeight-1, display.Width, headerHeight), &image.Uniform{barTrack}, image.Point{}, draw.Src)
	draw.Draw(frame, image.Rect(3, 4, 10, 11), &image.Uniform{nodeStatusColor(snapshot)}, image.Point{}, draw.Src)

	clusterLabel, clusterColor := clusterSummary(snapshot)
	textRight(frame, rightEdge, 12, clusterColor, clusterLabel)
	clusterLeft := rightEdge - textWidth(clusterLabel)

	nodeName := strings.TrimSpace(snapshot.NodeName)
	if nodeName == "" {
		nodeName = "unknown"
	}
	const nodeX = 13
	badgeWidth := 0
	if snapshot.Cordoned {
		badgeWidth = textWidth("C") + 3
	}
	nodeName = truncatePixels(nodeName, clusterLeft-nodeX-4-badgeWidth)
	text(frame, nodeX, 12, white, nodeName)
	if snapshot.Cordoned {
		text(frame, nodeX+textWidth(nodeName)+3, 12, yellow, "C")
	}
}

func renderPods(frame *image.RGBA, snapshot model.Snapshot) {
	count, status, statusColor := podSummary(snapshot)
	text(frame, 3, 31, white, count)
	textRight(frame, rightEdge, 31, statusColor, status)

	if !snapshot.HasPods {
		text(frame, 3, 49, dim, "Pod data unavailable")
		return
	}

	if len(snapshot.PodsBad) == 0 {
		ip := "IP unavailable"
		if strings.TrimSpace(snapshot.NodeIP) != "" {
			ip = "IP " + snapshot.NodeIP
		}
		text(frame, 3, 49, dim, truncatePixels(ip, display.Width-6))
		return
	}

	for index, name := range snapshot.PodsBad {
		if index >= 2 {
			break
		}
		text(frame, 3, 49+index*18, red, truncatePixels("! "+name, display.Width-6))
	}
}

func renderUsage(frame *image.RGBA, snapshot model.Snapshot) {
	cpu := snapshot.CPUPercent()
	memory := snapshot.MemoryPercent()
	cpuLabel := "CPU  --%"
	memoryLabel := "MEM  --%"
	if snapshot.HasMetrics && snapshot.CPUCapacityMilli > 0 {
		cpuLabel = fmt.Sprintf("CPU %3d%%", cpu)
	} else {
		cpu = 0
	}
	if snapshot.HasMetrics && snapshot.MemoryCapacity > 0 {
		memoryLabel = fmt.Sprintf("MEM %3d%%", memory)
	} else {
		memory = 0
	}
	text(frame, 3, 29, white, cpuLabel)
	progress(frame, 67, 20, 89, 9, cpu)
	text(frame, 3, 47, white, memoryLabel)
	progress(frame, 67, 38, 89, 9, memory)

	temperature := "TEMP  --.-C"
	temperatureColor := dim
	if snapshot.HasTemperature {
		temperature = fmt.Sprintf("TEMP %5.1fC", snapshot.TemperatureC)
		temperatureColor = green
		if snapshot.TemperatureC >= 70 {
			temperatureColor = yellow
		}
		if snapshot.TemperatureC >= 80 {
			temperatureColor = red
		}
	}
	text(frame, 3, 66, temperatureColor, temperature)
	if len(snapshot.Warnings) > 0 {
		textRight(frame, rightEdge, 66, yellow, fmt.Sprintf("WARN %d", len(snapshot.Warnings)))
	}
}

func clusterSummary(snapshot model.Snapshot) (string, color.Color) {
	if !snapshot.HasCluster {
		return "NODES --/--", yellow
	}
	label := fmt.Sprintf("NODES %d/%d", snapshot.ClusterReady, snapshot.ClusterTotal)
	if snapshot.ClusterTotal <= 0 {
		return label, yellow
	}
	if snapshot.ClusterStale {
		return label, yellow
	}
	if snapshot.ClusterReady == snapshot.ClusterTotal {
		return label, green
	}
	if snapshot.ClusterReady <= 0 {
		return label, red
	}
	return label, yellow
}

func podSummary(snapshot model.Snapshot) (string, string, color.Color) {
	if !snapshot.HasPods {
		return "PODS --", "UNKNOWN", yellow
	}
	count := fmt.Sprintf("PODS %d", snapshot.PodsTotal)
	if snapshot.PodsStale {
		return count, "STALE", yellow
	}
	if snapshot.PodsTotal <= 0 {
		return count, "NONE", yellow
	}
	unready := max(snapshot.PodsTotal-snapshot.PodsReady, 0)
	if unready == 0 {
		return count, "READY", green
	}
	if snapshot.PodsReady <= 0 {
		return count, fmt.Sprintf("%d UNREADY", unready), red
	}
	return count, fmt.Sprintf("%d UNREADY", unready), yellow
}

func nodeStatusColor(snapshot model.Snapshot) color.Color {
	if !snapshot.HasNode {
		return yellow
	}
	if snapshot.NodeStale {
		return yellow
	}
	if !snapshot.Ready {
		return red
	}
	if snapshot.Cordoned {
		return yellow
	}
	return green
}

func text(frame *image.RGBA, x, baseline int, colour color.Color, value string) {
	drawer := font.Drawer{
		Dst:  frame,
		Src:  image.NewUniform(colour),
		Face: basicfont.Face7x13,
		Dot:  fixed.P(x, baseline),
	}
	drawer.DrawString(value)
}

func textRight(frame *image.RGBA, right, baseline int, colour color.Color, value string) {
	text(frame, right-textWidth(value), baseline, colour, value)
}

func textWidth(value string) int {
	return font.MeasureString(basicfont.Face7x13, value).Ceil()
}

func progress(frame *image.RGBA, x, y, width, height, percent int) {
	draw.Draw(frame, image.Rect(x, y, x+width, y+height), image.NewUniform(barTrack), image.Point{}, draw.Src)
	filled := width * min(max(percent, 0), 100) / 100
	colour := green
	if percent >= 70 {
		colour = yellow
	}
	if percent >= 90 {
		colour = red
	}
	if filled > 0 {
		draw.Draw(frame, image.Rect(x, y, x+filled, y+height), image.NewUniform(colour), image.Point{}, draw.Src)
	}
}

func truncatePixels(value string, maximum int) string {
	value = strings.TrimSpace(value)
	if maximum <= 0 {
		return ""
	}
	if textWidth(value) <= maximum {
		return value
	}
	if textWidth("~") > maximum {
		return ""
	}
	runes := []rune(value)
	for len(runes) > 0 && textWidth(string(runes)+"~") > maximum {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "~"
}
