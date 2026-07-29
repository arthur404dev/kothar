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

func TestRetryThenSuccessIsOrdered(t *testing.T) {
	count := filepath.Join(t.TempDir(), "count")
	f := fixture(t, `while IFS= read -r line; do
case "$line" in *get_state*|*get_available_models*) printf '%s\n' '{"type":"response","success":true}' ;; *prompt*) if [ -f "`+count+`" ]; then printf '%s\n' '{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"OK"}}' '{"type":"message_end","message":{"stopReason":"stop"}}' '{"type":"agent_settled"}'; else touch "`+count+`"; printf '%s\n' '{"type":"response","success":false,"error":"provider unavailable"}'; fi ;; esac
done
`)
	r := session(t, f, agent("local/a", "local/b"))
	defer r.Close()
	var events []string
	stop, err := r.Prompt(context.Background(), engine.Request{Content: []engine.Content{{Type: "text", Text: "x"}}}, func(e engine.Event) error {
		if e.Type == "attempt" {
			events = append(events, e.Model+":"+e.Status)
		}
		return nil
	})
	if err != nil || stop != engine.EndTurn || strings.Join(events, ",") != "local/a:in_progress,local/a:failed,local/b:in_progress,local/b:completed" {
		t.Fatalf("stop=%s err=%v events=%v", stop, err, events)
	}
}

func TestThoughtOutputNeverRetries(t *testing.T) {
	count := filepath.Join(t.TempDir(), "count")
	f := fixture(t, `while IFS= read -r line; do
case "$line" in *get_state*|*get_available_models*) printf '%s\n' '{"type":"response","success":true}' ;; *prompt*) printf x >> "`+count+`"; printf '%s\n' '{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","delta":"x"}}' '{"type":"response","success":false,"error":"provider unavailable"}' ;; esac
done
`)
	r := session(t, f, agent("local/a", "local/b"))
	defer r.Close()
	_, err := r.Prompt(context.Background(), engine.Request{Content: []engine.Content{{Type: "text", Text: "x"}}}, func(engine.Event) error { return nil })
	if err == nil {
		t.Fatal("expected error")
	}
	if b, _ := os.ReadFile(count); string(b) != "x" {
		t.Fatalf("thought replayed: %q", b)
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
