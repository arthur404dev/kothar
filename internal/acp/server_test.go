package acp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arthur404dev/kothar/internal/engine"
	"github.com/arthur404dev/kothar/internal/framework"
)

type fakeFactory struct {
	active atomic.Int32
	closed atomic.Int32
}
type fakeRunner struct{ f *fakeFactory }

func (f *fakeFactory) New(context.Context, engine.Agent, engine.Session) (engine.SessionRunner, error) {
	return &fakeRunner{f}, nil
}
func (r *fakeRunner) Prompt(ctx context.Context, q engine.Request, emit func(engine.Event) error) (engine.StopReason, error) {
	r.f.active.Add(1)
	defer r.f.active.Add(-1)
	text := q.Content[0].Text
	if text == "wait" {
		<-ctx.Done()
		return engine.Cancelled, nil
	}
	if err := emit(engine.Event{Type: "agent_message_chunk", Text: "ACP"}); err != nil {
		return "", err
	}
	if err := emit(engine.Event{Type: "agent_message_chunk", Text: "_OK"}); err != nil {
		return "", err
	}
	return engine.EndTurn, nil
}
func (r *fakeRunner) Close() error { r.f.closed.Add(1); return nil }

func runServer(t *testing.T, input string) ([]map[string]any, string) {
	t.Helper()
	f := &fakeFactory{}
	var out, diag bytes.Buffer
	s := Server{In: strings.NewReader(input), Out: &out, Err: &diag, Service: framework.New(engine.Agent{ID: "test"}, f)}
	if err := s.Serve(context.Background()); err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	scan := bufio.NewScanner(&out)
	for scan.Scan() {
		var v map[string]any
		if json.Unmarshal(scan.Bytes(), &v) != nil {
			t.Fatalf("non-json stdout %q", scan.Text())
		}
		got = append(got, v)
	}
	return got, diag.String()
}
func TestPipeLifecycle(t *testing.T) {
	in := "" +
		`{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":2}}` + "\n" +
		`{"jsonrpc":"2.0","id":1,"method":"session/new","params":{"cwd":"/tmp","mcpServers":[]}}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"session/prompt","params":{"sessionId":"s-2","prompt":[{"type":"text","text":"go"}]}}` + "\n"
	got, diag := runServer(t, in)
	if diag != "" {
		t.Fatal(diag)
	}
	if len(got) != 5 {
		t.Fatalf("messages=%d %#v", len(got), got)
	}
	if got[0]["result"].(map[string]any)["protocolVersion"] != float64(2) {
		t.Fatal("initialize")
	}
	if got[3]["method"] != "session/update" || got[4]["result"].(map[string]any)["stopReason"] != "end_turn" {
		t.Fatalf("stream=%#v", got)
	}
}
func TestCancelAndEOFDrain(t *testing.T) {
	pr, pw := io.Pipe()
	var out bytes.Buffer
	f := &fakeFactory{}
	s := Server{In: pr, Out: &out, Err: io.Discard, Service: framework.New(engine.Agent{}, f)}
	done := make(chan error, 1)
	go func() { done <- s.Serve(context.Background()) }()
	io.WriteString(pw, `{"jsonrpc":"2.0","id":1,"method":"session/new","params":{"cwd":"/tmp","mcpServers":[]}}`+"\n")
	io.WriteString(pw, `{"jsonrpc":"2.0","id":2,"method":"session/prompt","params":{"sessionId":"s-1","prompt":[{"type":"text","text":"wait"}]}}`+"\n")
	deadline := time.Now().Add(time.Second)
	for f.active.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	io.WriteString(pw, `{"jsonrpc":"2.0","method":"session/cancel","params":{"sessionId":"s-1"}}`+"\n")
	pw.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("server leaked on EOF")
	}
	if f.active.Load() != 0 || f.closed.Load() != 1 {
		t.Fatalf("active=%d closed=%d", f.active.Load(), f.closed.Load())
	}
	if !strings.Contains(out.String(), `"stopReason":"cancelled"`) {
		t.Fatalf("output=%s", out.String())
	}
}
func TestFramingAndErrors(t *testing.T) {
	oversize := strings.Repeat("x", MaxLine+1) + "\n"
	in := oversize + "not json\n" + `{"jsonrpc":"2.0","id":1,"method":"nope","params":{}}` + "\n" + `{"jsonrpc":"2.0","id":2,"method":"session/new","params":{"cwd":"relative","mcpServers":[]}}` + "\n" + `{"jsonrpc":"2.0","id":3,"method":"session/new","params":{"cwd":"/tmp","mcpServers":[{}]}}` + "\n"
	got, diag := runServer(t, in)
	if len(got) != 5 {
		t.Fatalf("got %d", len(got))
	}
	want := []float64{-32700, -32700, -32601, -32602, -32602}
	for i, v := range got {
		if v["error"].(map[string]any)["code"] != want[i] {
			t.Fatalf("%d %#v", i, v)
		}
	}
	if strings.Contains(diag, "/tmp") {
		t.Fatalf("diagnostic leak: %s", diag)
	}
}
