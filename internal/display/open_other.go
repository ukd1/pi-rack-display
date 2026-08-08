//go:build !linux

package display

import "fmt"

func Open(path string, address uint8) (*Device, error) {
	return nil, fmt.Errorf("display: Linux is required to open %s at 0x%02x", path, address)
}
