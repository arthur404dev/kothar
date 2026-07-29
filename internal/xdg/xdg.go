// Package xdg resolves Kothar's user directories without creating them.
package xdg

import (
	"fmt"
	"os"
	"path/filepath"
)

type Paths struct{ Config, State, Cache string }

func Resolve() (Paths, error) {
	if override := os.Getenv("KOTHAR_CONFIG_DIR"); override != "" {
		if !filepath.IsAbs(override) {
			return Paths{}, fmt.Errorf("KOTHAR_CONFIG_DIR must be absolute")
		}
		p, err := roots()
		p.Config = filepath.Clean(override)
		return p, err
	}
	return roots()
}

func roots() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	root := func(env, fallback string) string {
		if value := os.Getenv(env); value != "" {
			return filepath.Join(value, "kothar")
		}
		return filepath.Join(home, fallback, "kothar")
	}
	return Paths{root("XDG_CONFIG_HOME", ".config"), root("XDG_STATE_HOME", ".local/state"), root("XDG_CACHE_HOME", ".cache")}, nil
}
