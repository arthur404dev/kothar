// Package pi implements the reviewed Pi 0.82.1 NDJSON RPC boundary.
package pi

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/arthur404dev/kothar/internal/engine"
)

const (
	Version  = engine.PiVersion
	CLIHash  = "af302f231437eaf6f37691bce4b34234fcb626bcb5eb3910d4fc3f6519bf78ca"
	NodeHash = "41a74efb34cbde5c7632cdac0cf8bd1a14d0b8d73dc1e82755014d9a9ce70f5c"
	maxLine  = 10 << 20
)

type Factory struct {
	Root, Interpreter, Executable, BuzzPath, ExpectedHash string
}

func NewFactory(root string) *Factory {
	return &Factory{Root: root, Interpreter: "/usr/local/libexec/kothar/node", Executable: "/usr/local/libexec/kothar/pi", BuzzPath: "/usr/local/libexec/kothar/buzz", ExpectedHash: CLIHash}
}
func Capability() engine.Capability { capability, _ := engine.Lookup("pi"); return capability }

func (f *Factory) New(_ context.Context, a engine.Agent, s engine.Session) (engine.SessionRunner, error) {
	if f.Root == "" || !filepath.IsAbs(f.Executable) || (f.BuzzPath != "" && !filepath.IsAbs(f.BuzzPath)) {
		return nil, fail("policy", "invalid Pi runtime configuration", nil)
	}
	root := filepath.Join(f.Root, "agents", a.ID)
	if err := os.MkdirAll(filepath.Join(root, "sessions"), 0700); err != nil {
		return nil, fail("engine_unavailable", "cannot prepare Pi state", err)
	}
	if err := os.Chmod(root, 0700); err != nil {
		return nil, fail("engine_unavailable", "cannot protect Pi state", err)
	}
	if err := Prepare(root, a, f.BuzzPath); err != nil {
		return nil, fail("engine_unavailable", "cannot prepare isolated Pi state", err)
	}
	return &runner{factory: f, agent: a, session: s, root: root}, nil
}

type runner struct {
	factory *Factory
	agent   engine.Agent
	session engine.Session
	root    string
	mu      sync.Mutex
	child   *child
}
type child struct {
	cmd         *exec.Cmd
	in          io.WriteCloser
	lines       chan line
	done        chan error
	cancel      context.CancelFunc
	initialized bool
}
type line struct {
	raw []byte
	err error
}

type packet struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Command   string          `json:"command"`
	Success   bool            `json:"success"`
	Error     string          `json:"error"`
	Data      json.RawMessage `json:"data"`
	Assistant json.RawMessage `json:"assistantMessageEvent"`
	Message   struct {
		Role         string `json:"role"`
		StopReason   string `json:"stopReason"`
		ErrorMessage string `json:"errorMessage"`
	} `json:"message"`
}

func (r *runner) Prompt(ctx context.Context, req engine.Request, emit func(engine.Event) error) (engine.StopReason, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	models := append([]string{r.agent.Models.Primary}, r.agent.Models.Fallbacks...)
	attempts := r.agent.Models.MaxAttempts
	if attempts < 1 {
		attempts = 1
	}
	if attempts > len(models) {
		attempts = len(models)
	}
	var last error
	for i := 0; i < attempts; i++ {
		if ctx.Err() != nil {
			return engine.Cancelled, nil
		}
		if err := emit(engine.Event{Type: "attempt", Model: models[i], Status: "in_progress"}); err != nil {
			return "", err
		}
		stop, visible, sideEffect, err := r.attempt(ctx, models[i], req, emit)
		if err == nil {
			_ = emit(engine.Event{Type: "attempt", Model: models[i], Status: "completed"})
			return stop, nil
		}
		_ = emit(engine.Event{Type: "attempt", Model: models[i], Status: "failed"})
		last = err
		if visible || sideEffect || !retryable(err) || errors.Is(err, context.Canceled) {
			break
		}
		r.stop()
	}
	return "", last
}

func (r *runner) attempt(ctx context.Context, model string, req engine.Request, emit func(engine.Event) error) (engine.StopReason, bool, bool, error) {
	if r.child == nil {
		c, err := r.start(model)
		if err != nil {
			return "", false, false, err
		}
		r.child = c
	}
	if !r.child.initialized {
		for _, q := range []map[string]any{{"id": "state", "type": "get_state"}, {"id": "models", "type": "get_available_models"}} {
			if err := r.send(q); err != nil {
				return "", false, false, err
			}
			select {
			case l := <-r.child.lines:
				if l.err != nil {
					return "", false, false, fail("engine_unavailable", "Pi handshake failed", l.err)
				}
				var p packet
				if json.Unmarshal(l.raw, &p) != nil || p.Type != "response" || !p.Success {
					return "", false, false, fail("auth", "Pi handshake failed", nil)
				}
			case <-time.After(5 * time.Second):
				return "", false, false, fail("timeout", "Pi handshake timed out", nil)
			}
		}
		r.child.initialized = true
	}
	message := strings.Builder{}
	for _, c := range req.Content {
		if c.Type != "text" {
			return "", false, false, fail("policy", "unsupported prompt content", nil)
		}
		message.WriteString(c.Text)
	}
	id := fmt.Sprintf("prompt-%d", time.Now().UnixNano())
	if err := r.send(map[string]any{"id": id, "type": "prompt", "message": message.String()}); err != nil {
		return "", false, false, err
	}
	visible, sideEffect := false, false
	var lastStop engine.StopReason
	for {
		select {
		case <-ctx.Done():
			_ = r.send(map[string]any{"id": "abort", "type": "abort"})
			t := time.NewTimer(2 * time.Second)
			defer t.Stop()
			for {
				select {
				case l := <-r.child.lines:
					if l.err != nil {
						r.stop()
						return engine.Cancelled, visible, sideEffect, nil
					}
					var p packet
					if json.Unmarshal(l.raw, &p) == nil && p.Type == "agent_settled" {
						return engine.Cancelled, visible, sideEffect, nil
					}
				case <-t.C:
					r.stop()
					return engine.Cancelled, visible, sideEffect, nil
				}
			}
		case l := <-r.child.lines:
			if l.err != nil {
				r.stop()
				return "", visible, sideEffect, fail("provider", "Pi process ended", l.err)
			}
			var p packet
			if err := json.Unmarshal(l.raw, &p); err != nil {
				r.stop()
				return "", visible, sideEffect, fail("protocol", "invalid Pi protocol", err)
			}
			if p.Type == "response" && !p.Success {
				return "", visible, sideEffect, classify(p.Error)
			}
			switch p.Type {
			case "message_update":
				var e struct {
					Type       string `json:"type"`
					Delta      string `json:"delta"`
					ToolCallID string `json:"toolCallId"`
					Name       string `json:"name"`
					Status     string `json:"status"`
				}
				_ = json.Unmarshal(p.Assistant, &e)
				switch e.Type {
				case "text_delta":
					visible = true
					if err := emit(engine.Event{Type: "agent_message_chunk", Text: e.Delta}); err != nil {
						return "", visible, sideEffect, err
					}
				case "thinking_delta":
					visible = true
					if err := emit(engine.Event{Type: "agent_thought_chunk", Text: e.Delta}); err != nil {
						return "", visible, sideEffect, err
					}
				case "tool_execution_start", "tool_call_start":
					sideEffect = true
					_ = emit(engine.Event{Type: "tool_call", ToolCallID: e.ToolCallID, Status: "in_progress"})
				}
			case "tool_execution_start", "tool_call_start":
				sideEffect = true
				if err := emit(engine.Event{Type: "tool_call", ToolCallID: p.ID, Status: "in_progress"}); err != nil {
					return "", visible, sideEffect, err
				}
			case "tool_execution_update", "tool_call_update":
				if err := emit(engine.Event{Type: "tool_call_update", ToolCallID: p.ID, Status: "in_progress"}); err != nil {
					return "", visible, sideEffect, err
				}
			case "tool_execution_end", "tool_call_end":
				if err := emit(engine.Event{Type: "tool_call_update", ToolCallID: p.ID, Status: "completed"}); err != nil {
					return "", visible, sideEffect, err
				}
			case "message_end":
				if p.Message.Role == "assistant" || p.Message.Role == "" { // Empty role keeps fixture compatibility.
					var err error
					lastStop, err = stopReason(p.Message.StopReason)
					if err != nil {
						r.stop()
						return "", visible, sideEffect, err
					}
				}
			case "agent_settled":
				if lastStop == "" {
					return "", visible, sideEffect, fail("protocol", "Pi omitted stop reason", nil)
				}
				return lastStop, visible, sideEffect, nil
			}
		}
	}
}

func (r *runner) start(model string) (*child, error) {
	if err := verify(r.factory.Executable, r.factory.ExpectedHash); err != nil {
		return nil, err
	}
	provider, name, ok := strings.Cut(model, "/")
	if !ok {
		return nil, fail("policy", "invalid model", nil)
	}
	home := filepath.Join(r.root, "home")
	piDir := filepath.Join(r.root, "pi")
	sessions := filepath.Join(r.root, "sessions")
	for _, d := range []string{home, piDir, sessions} {
		if err := os.MkdirAll(d, 0700); err != nil {
			return nil, fail("engine_unavailable", "cannot prepare Pi state", err)
		}
	}
	args := []string{"--provider", provider, "--model", name, "--thinking", r.agent.Models.Thinking, "--mode", "rpc", "--no-themes", "--no-extensions", "--no-skills", "--no-prompt-templates", "--no-context-files", "--no-approve", "--session-dir", sessions, "--system-prompt", r.agent.SystemPrompt}
	for _, p := range r.agent.Extensions {
		args = append(args, "--extension", filepath.Join(r.root, "resources", filepath.FromSlash(p)))
	}
	for _, p := range r.agent.Skills {
		args = append(args, "--skill", filepath.Join(r.root, "resources", filepath.FromSlash(p)))
	}
	if len(r.agent.Tools.Allow) == 0 {
		args = append(args, "--no-tools")
	} else {
		args = append(args, "--tools", strings.Join(r.agent.Tools.Allow, ","))
	}
	if len(r.agent.Tools.Deny) > 0 {
		args = append(args, "--exclude-tools", strings.Join(r.agent.Tools.Deny, ","))
	}
	command, commandArgs := r.factory.Executable, args
	if r.factory.Interpreter != "" {
		if err := verifyFile(r.factory.Interpreter, NodeHash); err != nil {
			return nil, err
		}
		command, commandArgs = r.factory.Interpreter, append([]string{r.factory.Executable}, args...)
	}
	cmd := exec.Command(command, commandArgs...)
	cmd.Dir = r.session.CWD
	cmd.Env = []string{"HOME=" + home, "PI_CODING_AGENT_DIR=" + piDir, "PI_TELEMETRY=0", "KOTHAR_BUZZ_CLI=" + r.factory.BuzzPath, "PATH=/usr/bin:/bin"}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &limitedRedacted{w: &stderr, n: 64 << 10}
	if err = cmd.Start(); err != nil {
		return nil, fail("engine_unavailable", "cannot start Pi", err)
	}
	c := &child{cmd: cmd, in: in, lines: make(chan line, 16), done: make(chan error, 1)}
	go scan(out, c.lines)
	go func() { c.done <- cmd.Wait() }()
	return c, nil
}
func scan(rd io.Reader, out chan<- line) {
	s := bufio.NewScanner(rd)
	s.Buffer(make([]byte, 64<<10), maxLine+1)
	for s.Scan() {
		if len(s.Bytes()) > maxLine {
			out <- line{err: fmt.Errorf("protocol line exceeds limit")}
			return
		}
		out <- line{raw: append([]byte(nil), s.Bytes()...)}
	}
	out <- line{err: s.Err()}
}
func (r *runner) send(v any) error {
	b, _ := json.Marshal(v)
	b = append(b, '\n')
	if _, err := r.child.in.Write(b); err != nil {
		return fail("provider", "Pi write failed", err)
	}
	return nil
}
func (r *runner) stop() {
	if r.child == nil {
		return
	}
	c := r.child
	_ = c.in.Close()
	done := false
	select {
	case <-c.done:
		done = true
	case <-time.After(100 * time.Millisecond):
	}
	// The process group may outlive an already-exited parent.
	_ = syscall.Kill(-c.cmd.Process.Pid, syscall.SIGTERM)
	if !done {
		select {
		case <-c.done:
			done = true
		case <-time.After(time.Second):
		}
	}
	if !done {
		_ = syscall.Kill(-c.cmd.Process.Pid, syscall.SIGKILL)
		<-c.done
	}
	r.child = nil
}
func (r *runner) Close() error { r.mu.Lock(); defer r.mu.Unlock(); r.stop(); return nil }
func verifyFile(path, expected string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fail("engine_unavailable", "reviewed Pi executable unavailable", err)
	}
	h := sha256.Sum256(b)
	if expected == "" {
		expected = CLIHash
	}
	if hex.EncodeToString(h[:]) != expected {
		return fail("policy", "Pi executable hash mismatch", nil)
	}
	return nil
}
func verify(path, expected string) error {
	if err := verifyFile(path, expected); err != nil {
		return err
	}
	if expected == CLIHash {
		asset := filepath.Join(filepath.Dir(path), "dist", "modes", "interactive", "theme", "dark.json")
		if fi, err := os.Stat(asset); err != nil || !fi.Mode().IsRegular() {
			return fail("engine_unavailable", "reviewed Pi installation incomplete", err)
		}
	}
	return nil
}
func stopReason(value string) (engine.StopReason, error) {
	switch strings.ToLower(strings.ReplaceAll(value, "-", "_")) {
	case "stop", "end_turn":
		return engine.EndTurn, nil
	case "max_tokens", "length":
		return engine.MaxTokens, nil
	case "max_turn_requests":
		return engine.MaxTurnRequests, nil
	case "refusal":
		return engine.Refusal, nil
	case "cancelled", "aborted":
		return engine.Cancelled, nil
	default:
		return "", fail("protocol", "Pi returned invalid stop reason", nil)
	}
}
func retryable(err error) bool {
	var f *engine.Failure
	return errors.As(err, &f) && (f.Class == "provider" || f.Class == "engine_unavailable")
}
func classify(s string) error {
	l := strings.ToLower(s)
	switch {
	case strings.Contains(l, "auth") || strings.Contains(l, "credential"):
		return fail("auth", "Pi authentication failed", nil)
	case strings.Contains(l, "model") || strings.Contains(l, "config"):
		return fail("policy", "Pi configuration failed", nil)
	default:
		return fail("provider", "Pi provider failed", nil)
	}
}
func fail(class, safe string, err error) error {
	return &engine.Failure{Class: class, Safe: safe, Err: err}
}

type limitedRedacted struct {
	w io.Writer
	n int
}

func (l *limitedRedacted) Write(p []byte) (int, error) {
	n := len(p)
	if l.n <= 0 {
		return n, nil
	}
	p = bytes.ReplaceAll(p, []byte("Bearer "), []byte("Bearer [REDACTED]"))
	if len(p) > l.n {
		p = p[:l.n]
	}
	l.n -= len(p)
	_, _ = l.w.Write(p)
	return n, nil
}
