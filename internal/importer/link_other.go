//go:build !unix

package importer

// isCrossDevice always reports false off unix, so link errors fail the import.
func isCrossDevice(err error) bool { return false }
