package display

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"testing"
	"time"
)

type recordingBus struct {
	writes  [][]byte
	closed  bool
	calls   int
	shortAt int
}

func (b *recordingBus) Write(data []byte) (int, error) {
	b.calls++
	b.writes = append(b.writes, bytes.Clone(data))
	if b.calls == b.shortAt {
		return len(data) - 1, nil
	}
	return len(data), nil
}

func (b *recordingBus) Close() error {
	b.closed = true
	return nil
}

func TestDrawProtocolAndRGB565(t *testing.T) {
	bus := &recordingBus{}
	device := New(bus)
	device.sleep = func(time.Duration) {}

	frame := image.NewRGBA(image.Rect(0, 0, Width, Height))
	frame.Set(0, 0, color.RGBA{R: 255, A: 255})
	frame.Set(1, 0, color.RGBA{G: 255, A: 255})
	frame.Set(2, 0, color.RGBA{B: 255, A: 255})

	if err := device.Draw(frame); err != nil {
		t.Fatal(err)
	}

	if got, want := len(bus.writes), 5+(Width*Height*2/maximumBurstBytes)+2; got != want {
		t.Fatalf("writes = %d, want %d", got, want)
	}
	wantCommands := [][]byte{
		{registerColumn, 0, Width - 1},
		{registerRow, YOffset, YOffset + Height - 1},
		{registerMemory, 0, 0},
		{registerSync, 0, 1},
		{registerBurst, 0, 1},
	}
	for index, want := range wantCommands {
		if !bytes.Equal(bus.writes[index], want) {
			t.Errorf("command %d = %v, want %v", index, bus.writes[index], want)
		}
	}
	if got, want := bus.writes[5][:6], []byte{0xf8, 0x00, 0x07, 0xe0, 0x00, 0x1f}; !bytes.Equal(got, want) {
		t.Errorf("first pixels = %x, want %x", got, want)
	}
	if got := bus.writes[len(bus.writes)-2]; !bytes.Equal(got, []byte{registerBurst, 0, 0}) {
		t.Errorf("disable command = %v", got)
	}
	if got := bus.writes[len(bus.writes)-1]; !bytes.Equal(got, []byte{registerSync, 0, 1}) {
		t.Errorf("sync command = %v", got)
	}
}

func TestDrawRejectsWrongSize(t *testing.T) {
	device := New(&recordingBus{})
	if err := device.Draw(image.NewRGBA(image.Rect(0, 0, 1, 1))); err == nil {
		t.Fatal("Draw returned nil error for the wrong frame size")
	}
}

func TestDrawSkipsUnchangedFrame(t *testing.T) {
	bus := &recordingBus{}
	device := New(bus)
	device.sleep = func(time.Duration) {}
	frame := image.NewRGBA(image.Rect(0, 0, Width, Height))
	if err := device.Draw(frame); err != nil {
		t.Fatal(err)
	}
	writes := len(bus.writes)
	if err := device.Draw(frame); err != nil {
		t.Fatal(err)
	}
	if got := len(bus.writes); got != writes {
		t.Fatalf("unchanged frame added %d writes", got-writes)
	}
}

func TestDrawPeriodicallyForcesFullRefresh(t *testing.T) {
	bus := &recordingBus{}
	device := New(bus)
	device.sleep = func(time.Duration) {}
	frame := image.NewRGBA(image.Rect(0, 0, Width, Height))
	if err := device.Draw(frame); err != nil {
		t.Fatal(err)
	}
	bus.writes = nil
	device.drawsSinceFull = fullRefreshEvery
	if err := device.Draw(frame); err != nil {
		t.Fatal(err)
	}
	if got, want := bus.writes[0], []byte{registerColumn, 0, Width - 1}; !bytes.Equal(got, want) {
		t.Errorf("forced column command = %v, want %v", got, want)
	}
	if got, want := len(bus.writes), 5+(Width*Height*2/maximumBurstBytes)+2; got != want {
		t.Fatalf("forced refresh used %d writes, want %d", got, want)
	}
}

func TestDrawUsesChangedRectangle(t *testing.T) {
	bus := &recordingBus{}
	device := New(bus)
	device.sleep = func(time.Duration) {}
	frame := image.NewRGBA(image.Rect(0, 0, Width, Height))
	if err := device.Draw(frame); err != nil {
		t.Fatal(err)
	}
	bus.writes = nil
	frame.Set(10, 20, color.White)
	if err := device.Draw(frame); err != nil {
		t.Fatal(err)
	}
	if got, want := bus.writes[0], []byte{registerColumn, 10, 10}; !bytes.Equal(got, want) {
		t.Errorf("column command = %v, want %v", got, want)
	}
	if got, want := bus.writes[1], []byte{registerRow, YOffset + 20, YOffset + 20}; !bytes.Equal(got, want) {
		t.Errorf("row command = %v, want %v", got, want)
	}
	if got, want := bus.writes[5], []byte{0xff, 0xff}; !bytes.Equal(got, want) {
		t.Errorf("pixel payload = %x, want %x", got, want)
	}
}

func TestDrawCleansUpAndRetriesAfterShortWrite(t *testing.T) {
	for _, shortAt := range []int{5, 6} {
		t.Run(fmt.Sprintf("write-%d", shortAt), func(t *testing.T) {
			bus := &recordingBus{shortAt: shortAt}
			device := New(bus)
			device.sleep = func(time.Duration) {}
			frame := image.NewRGBA(image.Rect(0, 0, Width, Height))
			if err := device.Draw(frame); err == nil {
				t.Fatal("Draw returned nil after a short I2C transaction")
			}
			if got := bus.writes[len(bus.writes)-2]; !bytes.Equal(got, []byte{registerBurst, 0, 0}) {
				t.Errorf("cleanup burst command = %v", got)
			}
			if got := bus.writes[len(bus.writes)-1]; !bytes.Equal(got, []byte{registerSync, 0, 1}) {
				t.Errorf("cleanup sync command = %v", got)
			}

			bus.shortAt = 0
			bus.calls = 0
			bus.writes = nil
			if err := device.Draw(frame); err != nil {
				t.Fatalf("retry Draw: %v", err)
			}
			if got, want := len(bus.writes), 5+(Width*Height*2/maximumBurstBytes)+2; got != want {
				t.Fatalf("retry used %d writes, want full redraw with %d", got, want)
			}
		})
	}
}
