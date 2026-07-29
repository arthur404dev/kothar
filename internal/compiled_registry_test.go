package internal_test

import (
	"testing"

	"github.com/arthur404dev/kothar/internal/engine"
	"github.com/arthur404dev/kothar/internal/inbound"
)

func TestCompiledSelectionsFailClosed(t *testing.T) {
	if adapter, ok := inbound.Lookup("buzz"); !ok || adapter.Name != "buzz" {
		t.Fatalf("compiled inbound buzz missing: %#v, %v", adapter, ok)
	}
	if capability, ok := engine.Lookup("pi"); !ok || capability.Name != "pi" {
		t.Fatalf("compiled engine pi missing: %#v, %v", capability, ok)
	}
	for _, name := range []string{"", "Buzz", "other"} {
		if _, ok := inbound.Lookup(name); ok {
			t.Fatalf("unexpected inbound %q", name)
		}
		if _, ok := engine.Lookup(name); ok {
			t.Fatalf("unexpected engine %q", name)
		}
	}
}
