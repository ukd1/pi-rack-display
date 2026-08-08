//go:build linux

package display

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

const i2cSlaveForce = 0x0706

// Open selects address on the Linux I2C character device and returns a display.
func Open(path string, address uint8) (*Device, error) {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("display: open %s: %w", path, err)
	}
	if err := unix.IoctlSetInt(int(file.Fd()), i2cSlaveForce, int(address)); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("display: select I2C address 0x%02x on %s: %w", address, path, err)
	}
	return New(file), nil
}
