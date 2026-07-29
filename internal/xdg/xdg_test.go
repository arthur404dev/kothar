package xdg

import (
	"path/filepath"
	"testing"
)

func TestResolve(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("KOTHAR_CONFIG_DIR", "")
	p, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if p.Config != filepath.Join(home, ".config", "kothar") || p.State != filepath.Join(home, ".local/state", "kothar") || p.Cache != filepath.Join(home, ".cache", "kothar") {
		t.Fatalf("paths: %+v", p)
	}
	override := filepath.Join(home, "test-config")
	t.Setenv("KOTHAR_CONFIG_DIR", override)
	p, err = Resolve()
	if err != nil || p.Config != override {
		t.Fatalf("override: %+v %v", p, err)
	}
}
func TestRelativeOverrideRejected(t *testing.T) {
	t.Setenv("KOTHAR_CONFIG_DIR", "relative")
	if _, err := Resolve(); err == nil {
		t.Fatal("relative override accepted")
	}
}
