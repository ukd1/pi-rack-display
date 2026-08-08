package screen

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"path"
	"strings"

	"github.com/ukd1/pi-rack-display/internal/display"
	"github.com/ukd1/pi-rack-display/internal/model"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

const PageCount = 4

var (
	black    = color.RGBA{0x00, 0x00, 0x00, 0xff}
	white    = color.RGBA{0xf5, 0xf7, 0xfa, 0xff}
	dim      = color.RGBA{0x92, 0xa2, 0xb6, 0xff}
	cyan     = color.RGBA{0x31, 0xd7, 0xf4, 0xff}
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
	draw.Draw(frame, image.Rect(0, 0, display.Width, 16), &image.Uniform{header}, image.Point{}, draw.Src)

	titles := []string{"NODE", "USAGE", "PODS", "CLUSTER"}
	text(frame, 3, 12, cyan, titles[pageNumber])
	textRight(frame, display.Width-3, 12, dim, fmt.Sprintf("%d/%d", pageNumber+1, PageCount))

	switch pageNumber {
	case 0:
		renderNode(frame, snapshot)
	case 1:
		renderUsage(frame, snapshot)
	case 2:
		renderPods(frame, snapshot)
	case 3:
		renderCluster(frame, snapshot)
	}
	return frame
}

func renderNode(frame *image.RGBA, snapshot model.Snapshot) {
	text(frame, 3, 31, white, truncate(snapshot.NodeName, 22))
	statusColor := red
	status := "NOT READY"
	if !snapshot.HasNode {
		statusColor = yellow
		status = "UNKNOWN"
	} else if snapshot.Ready {
		statusColor = green
		status = "READY"
	}
	text(frame, 3, 48, statusColor, status)
	schedule := "schedulable"
	if snapshot.Cordoned {
		schedule = "cordoned"
	}
	textRight(frame, 157, 48, cordonedColor(snapshot), schedule)
	text(frame, 3, 66, dim, truncate(snapshot.NodeIP, 22))
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
}

func renderPods(frame *image.RGBA, snapshot model.Snapshot) {
	statusColor := green
	if !snapshot.HasPods || snapshot.PodsReady != snapshot.PodsTotal {
		statusColor = yellow
	}
	label := fmt.Sprintf("Ready %d/%d", snapshot.PodsReady, snapshot.PodsTotal)
	if !snapshot.HasPods {
		label = "Ready --/--"
	}
	text(frame, 3, 31, statusColor, label)
	if len(snapshot.PodsBad) == 0 {
		text(frame, 3, 49, dim, "All active pods ready")
		return
	}
	for index, name := range snapshot.PodsBad {
		if index >= 2 {
			break
		}
		text(frame, 3, 49+index*16, red, "! "+truncate(path.Base(name), 20))
	}
}

func renderCluster(frame *image.RGBA, snapshot model.Snapshot) {
	statusColor := green
	if !snapshot.HasCluster || snapshot.ClusterReady != snapshot.ClusterTotal || snapshot.ClusterTotal == 0 {
		statusColor = yellow
	}
	label := fmt.Sprintf("Nodes %d/%d ready", snapshot.ClusterReady, snapshot.ClusterTotal)
	if !snapshot.HasCluster {
		label = "Nodes --/-- ready"
	}
	text(frame, 3, 31, statusColor, label)
	text(frame, 3, 49, white, "This: "+truncate(snapshot.NodeName, 16))
	footer := "updated --:--"
	if !snapshot.CollectedAt.IsZero() {
		footer = "updated " + snapshot.CollectedAt.Format("15:04")
	}
	if len(snapshot.Warnings) > 0 {
		text(frame, 3, 67, yellow, fmt.Sprintf("%d warning(s)", len(snapshot.Warnings)))
	} else {
		text(frame, 3, 67, dim, footer)
	}
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
	width := font.MeasureString(basicfont.Face7x13, value).Ceil()
	text(frame, right-width, baseline, colour, value)
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

func truncate(value string, maximum int) string {
	value = strings.TrimSpace(value)
	if len(value) <= maximum {
		return value
	}
	if maximum <= 1 {
		return value[:maximum]
	}
	return value[:maximum-1] + "~"
}

func cordonedColor(snapshot model.Snapshot) color.Color {
	if snapshot.Cordoned {
		return yellow
	}
	return dim
}
