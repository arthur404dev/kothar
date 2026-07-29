package pi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arthur404dev/kothar/internal/engine"
)

func fixture(t *testing.T, body string) *Factory {
	t.Helper()
	root := t.TempDir()
	p := filepath.Join(root, "pi")
	b := []byte("#!/bin/sh\n" + body)
	if err := os.WriteFile(p, b, 0700); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(b)
	return &Factory{Root: filepath.Join(root, "state"), Executable: p, ExpectedHash: hex.EncodeToString(h[:])}
}
func agent(models ...string) engine.Agent {
	return engine.Agent{ID: "test", Models: engine.ModelPolicy{Primary: models[0], Fallbacks: models[1:], Thinking: "off", MaxAttempts: len(models)}}
}
func session(t *testing.T, f *Factory, a engine.Agent) engine.SessionRunner {
	t.Helper()
	r, e := f.New(context.Background(), a, engine.Session{ID: "s", CWD: t.TempDir()})
	if e != nil {
		t.Fatal(e)
	}
	return r
}

func TestSuccessAndCleanup(t *testing.T) {
	f := fixture(t, `while IFS= read -r line; do
case "$line" in *get_state*|*get_available_models*) printf '%s\n' '{"type":"response","success":true}' ;; *prompt*) printf '%s\n' '{"type":"response","success":true}' '{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"OK"}}' '{"type":"agent_settled"}' ;; esac
done
`)
	r := session(t, f, agent("local/test"))
	defer r.Close()
	var got string
	stop, err := r.Prompt(context.Background(), engine.Request{Content: []engine.Content{{Type: "text", Text: "hi"}}}, func(e engine.Event) error { got += e.Text; return nil })
	if err != nil || stop != engine.EndTurn || got != "OK" {
		t.Fatalf("%v %s %q", err, stop, got)
	}
}
func TestMalformedProtocol(t *testing.T) {
	f := fixture(t, `while IFS= read -r line; do printf '%s\n' 'not-json'; done
`)
	r := session(t, f, agent("local/test"))
	defer r.Close()
	_, err := r.Prompt(context.Background(), engine.Request{Content: []engine.Content{{Type: "text", Text: "x"}}}, func(engine.Event) error { return nil })
	if err == nil {
		t.Fatal("expected error")
	}
}
func TestCancellation(t *testing.T) {
	f := fixture(t, `while IFS= read -r line; do
case "$line" in *get_state*|*get_available_models*) printf '%s\n' '{"type":"response","success":true}' ;; *abort*) printf '%s\n' '{"type":"agent_settled"}' ;; esac
done
`)
	r := session(t, f, agent("local/test"))
	defer r.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	stop, err := r.Prompt(ctx, engine.Request{Content: []engine.Content{{Type: "text", Text: "x"}}}, func(engine.Event) error { return nil })
	if err != nil || stop != engine.Cancelled {
		t.Fatalf("%v %s", err, stop)
	}
}
func TestVisibleOutputNeverRetries(t *testing.T) {
	count := filepath.Join(t.TempDir(), "count")
	f := fixture(t, `while IFS= read -r line; do
case "$line" in *get_state*|*get_available_models*) printf '%s\n' '{"type":"response","success":true}' ;; *prompt*) printf x >> "`+count+`"; printf '%s\n' '{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"x"}}' '{"type":"response","success":false,"error":"provider unavailable"}' ;; esac
done
`)
	r := session(t, f, agent("local/a", "local/b"))
	defer r.Close()
	_, err := r.Prompt(context.Background(), engine.Request{Content: []engine.Content{{Type: "text", Text: "x"}}}, func(engine.Event) error { return nil })
	if err == nil {
		t.Fatal("expected error")
	}
	b, _ := os.ReadFile(count)
	if string(b) != "x" {
		t.Fatalf("replayed: %q", b)
	}
}
func TestAuthIsTerminalAndRedacted(t *testing.T) {
	f := fixture(t, `while IFS= read -r line; do
case "$line" in *get_state*|*get_available_models*) printf '%s\n' '{"type":"response","success":true}' ;; *prompt*) printf '%s\n' '{"type":"response","success":false,"error":"authentication required secret-token"}' ;; esac
done
`)
	r := session(t, f, agent("local/a", "local/b"))
	defer r.Close()
	_, err := r.Prompt(context.Background(), engine.Request{Content: []engine.Content{{Type: "text", Text: "x"}}}, func(engine.Event) error { return nil })
	if err == nil || strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("unsafe error: %v", err)
	}
	var e *engine.Failure
	if !errors.As(err, &e) || e.Class != "auth" {
		t.Fatalf("class: %v", err)
	}
}
