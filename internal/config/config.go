// Package config reads softwarr's startup configuration from the environment.
package config

import (
	"fmt"
	"strconv"
	"strings"
)

// Config holds settings that require a restart to change.
type Config struct {
	Port    int
	DataDir string
	URLBase string
}

// FromEnv builds a Config from getenv, applying defaults and normalization.
func FromEnv(getenv func(string) string) (Config, error) {
	c := Config{Port: 7979, DataDir: "/config"}
	if v := getenv("SOFTWARR_PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil || p < 1 || p > 65535 {
			return Config{}, fmt.Errorf("invalid SOFTWARR_PORT %q", v)
		}
		c.Port = p
	}
	if v := getenv("SOFTWARR_DATA_DIR"); v != "" {
		c.DataDir = v
	}
	if b := strings.Trim(getenv("SOFTWARR_URL_BASE"), "/"); b != "" {
		c.URLBase = "/" + b
	}
	return c, nil
}
