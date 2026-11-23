//go:build unix

package importer

import (
	"errors"
	"syscall"
)

// isCrossDevice reports whether err means source and destination are on
// different filesystems.
func isCrossDevice(err error) bool {
	return errors.Is(err, syscall.EXDEV)
}
