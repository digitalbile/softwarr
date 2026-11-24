package db

import "strings"

// BoolInt stores a bool in an SQLite INTEGER column.
func BoolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// IsUniqueViolation reports whether err is SQLite rejecting a write that
// breaks a UNIQUE constraint or unique index.
func IsUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
