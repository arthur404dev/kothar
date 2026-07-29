package pi

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arthur404dev/kothar/internal/engine"
)

func TestStartupAndOversizeFailures(t *testing.T) {
	f := &Factory{Root: t.TempDir(), Executable: "/does/not/exist", ExpectedHash: CLIHash}
	r := session(t, f, agent("local/test"))
	defer r.Close()
	if _, err := r.Prompt(context.Background(), engine.Request{Content: []engine.Content{{Type: "text", Text: "x"}}}, func(engine.Event) error { return nil }); err == nil {
		t.Fatal("expected startup failure")
	}

	f = fixture(t, `python3 - <<'PY'
print('x' * (10485761))
PY
`)
	r = session(t, f, agent("local/test"))
	defer r.Close()
	if _, err := r.Prompt(context.Background(), engine.Request{Content: []engine.Content{{Type: "text", Text: "x"}}}, func(engine.Event) error { return nil }); err == nil {
		t.Fatal("expected oversize failure")
	}
}

func TestToolSideEffectAndExhaustionAreBounded(t *testing.T) {
	count := filepath.Join(t.TempDir(), "count")
	f := fixture(t, `while IFS= read -r line; do
case "$line" in *get_state*|*get_available_models*) printf '%s\n' '{"type":"response","success":true}' ;; *prompt*) printf x >> "`+count+`"; printf '%s\n' '{"type":"tool_execution_start","id":"tool-1"}' '{"type":"response","success":false,"error":"provider unavailable"}' ;; esac
done
`)
	r := session(t, f, agent("local/a", "local/b"))
	defer r.Close()
	if _, err := r.Prompt(context.Background(), engine.Request{Content: []engine.Content{{Type: "text", Text: "x"}}}, func(engine.Event) error { return nil }); err == nil {
		t.Fatal("expected error")
	}
	if b, _ := os.ReadFile(count); string(b) != "x" {
		t.Fatalf("tool replayed: %q", b)
	}

	count = filepath.Join(t.TempDir(), "attempts")
	f = fixture(t, strings.ReplaceAll(`while IFS= read -r line; do
case "$line" in *get_state*|*get_available_models*) printf '%s\n' '{"type":"response","success":true}' ;; *prompt*) printf x >> "COUNT"; printf '%s\n' '{"type":"response","success":false,"error":"provider unavailable"}' ;; esac
done
`, "COUNT", count))
	r = session(t, f, agent("local/a", "local/b"))
	defer r.Close()
	if _, err := r.Prompt(context.Background(), engine.Request{Content: []engine.Content{{Type: "text", Text: "x"}}}, func(engine.Event) error { return nil }); err == nil {
		t.Fatal("expected exhaustion")
	}
	if b, _ := os.ReadFile(count); string(b) != "xx" {
		t.Fatalf("attempts not bounded: %q", b)
	}
}
