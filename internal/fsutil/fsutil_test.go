package fsutil

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWithin(t *testing.T) {
	root := filepath.FromSlash("/lib/root")
	cases := []struct {
		target       string
		within, orEq bool
	}{
		{"/lib/root", false, true},
		{"/lib/root/", false, true},
		{"/lib/root/a", true, true},
		{"/lib/root/a/b", true, true},
		{"/lib/root/../root/a", true, true},
		{"/lib", false, false},
		{"/lib/rootx", false, false},
		{"/lib/root/../other", false, false},
		{"/elsewhere", false, false},
	}
	for _, c := range cases {
		target := filepath.FromSlash(c.target)
		assert.Equal(t, c.within, Within(root, target), "Within %s", c.target)
		assert.Equal(t, c.orEq, WithinOrEqual(root, target), "WithinOrEqual %s", c.target)
	}
}

func TestWithinRelativePaths(t *testing.T) {
	assert.True(t, Within("lib", filepath.Join("lib", "a")))
	assert.False(t, Within("lib", "lib"))
	assert.True(t, WithinOrEqual("lib", "lib"))
	assert.False(t, WithinOrEqual("lib", filepath.Join("lib", "..", "x")))
}
