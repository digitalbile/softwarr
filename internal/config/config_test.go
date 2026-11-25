package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/digitalbile/softwarr/internal/config"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestFromEnvDefaults(t *testing.T) {
	c, err := config.FromEnv(func(string) string { return "" })
	require.NoError(t, err)
	assert.Equal(t, config.Config{Port: 7979, DataDir: "/config", URLBase: ""}, c)
}

func TestFromEnvNormalizesURLBase(t *testing.T) {
	for in, want := range map[string]string{"softwarr": "/softwarr", "/softwarr/": "/softwarr", "/": ""} {
		c, _ := config.FromEnv(env(map[string]string{"SOFTWARR_URL_BASE": in}))
		assert.Equal(t, want, c.URLBase, in)
	}
}

func TestFromEnvRejectsBadPort(t *testing.T) {
	_, err := config.FromEnv(env(map[string]string{"SOFTWARR_PORT": "abc"}))
	assert.Error(t, err)
}
