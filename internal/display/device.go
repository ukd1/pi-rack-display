package display

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"io"
	"sync"
	"time"
)

const (
	Width      = 160
	Height     = 80
	YOffset    = 24
	I2CAddress = 0x18

	registerColumn = 0x2a
	registerRow    = 0x2b
	registerMemory = 0x2c
	registerBurst  = 0x01
	registerSync   = 0x03

	maximumBurstBytes = 160
	fullRefreshEvery  = 24
)

type writeCloser interface {
	io.Writer
	io.Closer
}

// Device writes complete RGB565 frames to the I2C display bridge used by the
// UCTRONICS Pi Rack Pro. It owns bus and must be closed by the caller.
type Device struct {
	mu             sync.Mutex
	bus            writeCloser
	sleep          func(time.Duration)
	previous       []byte
	drawsSinceFull int
}

func New(bus writeCloser) *Device {
	return &Device{bus: bus, sleep: time.Sleep}
}

func (d *Device) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.bus.Close()
}

// Draw writes the 160x80 image as a complete frame. The bridge accepts at most
// 160 data bytes per I2C write while burst mode is active.
func (d *Device) Draw(frame image.Image) (returnErr error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if frame == nil {
		return errors.New("display: nil frame")
	}
	if frame.Bounds().Dx() != Width || frame.Bounds().Dy() != Height {
		return fmt.Errorf("display: frame is %dx%d; want %dx%d", frame.Bounds().Dx(), frame.Bounds().Dy(), Width, Height)
	}

	pixels := rgb565(frame)
	forceFull := len(d.previous) != len(pixels) || d.drawsSinceFull >= fullRefreshEvery
	changed := image.Rect(0, 0, Width, Height)
	if !forceFull {
		changed = changedRectangle(d.previous, pixels)
	}
	if changed.Empty() {
		d.drawsSinceFull++
		return nil
	}
	payload := rectanglePixels(pixels, changed)
	commands := [][]byte{
		{registerColumn, byte(changed.Min.X), byte(changed.Max.X - 1)},
		{registerRow, byte(YOffset + changed.Min.Y), byte(YOffset + changed.Max.Y - 1)},
		{registerMemory, 0, 0},
		{registerSync, 0, 1},
	}
	for _, command := range commands {
		if err := writeExact(d.bus, command); err != nil {
			return fmt.Errorf("display: write setup command 0x%02x: %w", command[0], err)
		}
		d.sleep(10 * time.Microsecond)
	}
	if err := writeExact(d.bus, []byte{registerBurst, 0, 1}); err != nil {
		_ = d.finishBurst()
		return fmt.Errorf("display: enable burst: %w", err)
	}
	d.sleep(10 * time.Microsecond)

	burstEnabled := true
	defer func() {
		if !burstEnabled {
			return
		}
		cleanupErr := d.finishBurst()
		if returnErr == nil && cleanupErr != nil {
			returnErr = cleanupErr
		}
	}()

	for offset := 0; offset < len(payload); offset += maximumBurstBytes {
		end := min(offset+maximumBurstBytes, len(payload))
		if err := writeExact(d.bus, payload[offset:end]); err != nil {
			return fmt.Errorf("display: write pixels at byte %d: %w", offset, err)
		}
		d.sleep(700 * time.Microsecond)
	}

	if err := d.finishBurst(); err != nil {
		return err
	}
	burstEnabled = false
	d.previous = bytes.Clone(pixels)
	if forceFull {
		d.drawsSinceFull = 0
	} else {
		d.drawsSinceFull++
	}
	return nil
}

func (d *Device) finishBurst() error {
	if err := writeExact(d.bus, []byte{registerBurst, 0, 0}); err != nil {
		return fmt.Errorf("display: disable burst: %w", err)
	}
	d.sleep(10 * time.Microsecond)
	if err := writeExact(d.bus, []byte{registerSync, 0, 1}); err != nil {
		return fmt.Errorf("display: sync frame: %w", err)
	}
	d.sleep(10 * time.Microsecond)
	return nil
}

func rgb565(frame image.Image) []byte {
	bounds := frame.Bounds()
	pixels := make([]byte, 0, Width*Height*2)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := frame.At(x, y).RGBA()
			value := uint16((r>>11)<<11 | (g>>10)<<5 | b>>11)
			pixels = append(pixels, byte(value>>8), byte(value))
		}
	}
	return pixels
}

func changedRectangle(previous, current []byte) image.Rectangle {
	if len(previous) != len(current) {
		return image.Rect(0, 0, Width, Height)
	}
	minimumX, minimumY := Width, Height
	maximumX, maximumY := -1, -1
	for pixel := 0; pixel < Width*Height; pixel++ {
		offset := pixel * 2
		if previous[offset] == current[offset] && previous[offset+1] == current[offset+1] {
			continue
		}
		x, y := pixel%Width, pixel/Width
		minimumX = min(minimumX, x)
		minimumY = min(minimumY, y)
		maximumX = max(maximumX, x)
		maximumY = max(maximumY, y)
	}
	if maximumX < 0 {
		return image.Rectangle{}
	}
	return image.Rect(minimumX, minimumY, maximumX+1, maximumY+1)
}

func rectanglePixels(frame []byte, rectangle image.Rectangle) []byte {
	rowBytes := rectangle.Dx() * 2
	payload := make([]byte, 0, rowBytes*rectangle.Dy())
	for y := rectangle.Min.Y; y < rectangle.Max.Y; y++ {
		start := (y*Width + rectangle.Min.X) * 2
		payload = append(payload, frame[start:start+rowBytes]...)
	}
	return payload
}

func writeExact(writer io.Writer, data []byte) error {
	written, err := writer.Write(data)
	if err != nil {
		return err
	}
	if written != len(data) {
		return io.ErrShortWrite
	}
	return nil
}
