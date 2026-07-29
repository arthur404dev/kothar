package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/arthur404dev/kothar/internal/framework"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}
type sessionParams struct {
	CWD          string           `json:"cwd"`
	MCPServers   *json.RawMessage `json:"mcpServers"`
	SystemPrompt string           `json:"systemPrompt"`
}
type promptParams struct {
	SessionID string              `json:"sessionId"`
	Prompt    []framework.Content `json:"prompt"`
}
type cancelParams struct {
	SessionID string `json:"sessionId"`
}

func (s *Server) serve(ctx context.Context) error {
	ctx, cancelAll := context.WithCancel(ctx)
	defer cancelAll()
	if s.RequestTimeout == 0 {
		s.RequestTimeout = 60 * time.Second
	}
	if s.TurnTimeout == 0 {
		s.TurnTimeout = 30 * time.Minute
	}
	c := newCodec(s.In, s.Out)
	var wg sync.WaitGroup
	var seq atomic.Uint64
	active := map[string]bool{}
	var mu sync.Mutex
	for {
		line, err := c.read()
		if err == io.EOF {
			cancelAll()
			s.Service.CancelAll()
			wg.Wait()
			return s.Service.Close()
		}
		if err != nil {
			c.write(response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{-32700, "parse error"}})
			if errors.Is(err, errOversize) {
				continue
			}
			s.Service.Close()
			return err
		}
		var req rpcRequest
		if len(line) == 0 || json.Unmarshal(line, &req) != nil {
			c.write(response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{-32700, "parse error"}})
			continue
		}
		if req.JSONRPC != "2.0" || req.Method == "" {
			s.replyError(c, req.ID, -32600, "invalid request")
			continue
		}
		if req.Method == "session/cancel" && len(req.ID) == 0 {
			var p cancelParams
			if json.Unmarshal(req.Params, &p) == nil {
				s.Service.Cancel(p.SessionID)
			}
			continue
		}
		if len(req.ID) == 0 {
			continue
		}
		key := string(req.ID)
		mu.Lock()
		duplicate := active[key]
		if !duplicate {
			active[key] = true
		}
		mu.Unlock()
		if duplicate {
			s.replyError(c, req.ID, -32600, "duplicate request id")
			continue
		}
		finish := func() { mu.Lock(); delete(active, key); mu.Unlock() }
		if req.Method == "session/prompt" {
			wg.Add(1)
			go func(r rpcRequest) { defer wg.Done(); defer finish(); s.prompt(ctx, c, r) }(req)
			continue
		}
		s.handle(ctx, c, req, seq.Add(1))
		finish()
	}
}
func (s *Server) handle(parent context.Context, c *codec, r rpcRequest, n uint64) {
	ctx, cancel := context.WithTimeout(parent, s.RequestTimeout)
	defer cancel()
	_ = ctx
	switch r.Method {
	case "initialize":
		var p struct {
			ProtocolVersion int `json:"protocolVersion"`
		}
		if json.Unmarshal(r.Params, &p) != nil || p.ProtocolVersion != 2 {
			s.replyError(c, r.ID, -32602, "unsupported protocol version")
			return
		}
		c.write(response{JSONRPC: "2.0", ID: r.ID, Result: map[string]any{"protocolVersion": 2, "agentInfo": map[string]string{"name": "kothar", "version": "dev"}, "agentCapabilities": map[string]bool{"loadSession": false}}})
	case "session/new":
		var p sessionParams
		if json.Unmarshal(r.Params, &p) != nil || p.MCPServers == nil || !filepath.IsAbs(p.CWD) {
			s.replyError(c, r.ID, -32602, "invalid session parameters")
			return
		}
		var servers []json.RawMessage
		if json.Unmarshal(*p.MCPServers, &servers) != nil {
			s.replyError(c, r.ID, -32602, "mcpServers must be an array")
			return
		}
		id := fmt.Sprintf("s-%d", n)
		if err := s.Service.NewSession(ctx, framework.NewSession{ID: id, CWD: p.CWD, SystemPrompt: p.SystemPrompt, MCPServers: len(servers)}); err != nil {
			s.frameworkError(c, r.ID, err)
			return
		}
		c.write(response{JSONRPC: "2.0", ID: r.ID, Result: map[string]string{"sessionId": id}})
	default:
		s.replyError(c, r.ID, -32601, "method not found")
	}
}
func (s *Server) prompt(parent context.Context, c *codec, r rpcRequest) {
	var p promptParams
	if json.Unmarshal(r.Params, &p) != nil || p.SessionID == "" || len(p.Prompt) == 0 {
		s.replyError(c, r.ID, -32602, "invalid prompt parameters")
		return
	}
	for _, b := range p.Prompt {
		if b.Type != "text" || b.Text == "" {
			s.replyError(c, r.ID, -32602, "unsupported prompt content")
			return
		}
	}
	ctx, cancel := context.WithTimeout(parent, s.TurnTimeout)
	defer cancel()
	stop, err := s.Service.Prompt(ctx, framework.Request{SessionID: p.SessionID, Content: p.Prompt}, func(e framework.Event) error { return c.write(mapEvent(p.SessionID, e)) })
	if err != nil {
		s.frameworkError(c, r.ID, err)
		return
	}
	c.write(response{JSONRPC: "2.0", ID: r.ID, Result: map[string]any{"stopReason": stop}})
}
func mapEvent(id string, e framework.Event) any {
	u := map[string]any{"sessionUpdate": e.Type}
	switch e.Type {
	case "agent_message_chunk", "agent_thought_chunk":
		u["content"] = map[string]string{"type": "text", "text": e.Text}
	case "attempt":
		u["model"] = e.Model
		u["status"] = e.Status
	case "tool_call", "tool_call_update":
		u["toolCallId"] = e.ToolCallID
		u["status"] = e.Status
		if e.Text != "" {
			u["title"] = e.Text
		}
	}
	return map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": id, "update": u}}
}
func (s *Server) frameworkError(c *codec, id json.RawMessage, err error) {
	var e *framework.Error
	code := -32603
	msg := "internal error"
	if errors.As(err, &e) && (e.Class == framework.InvalidRequest || e.Class == framework.UnknownSession || e.Class == framework.Policy) {
		code = -32602
		msg = e.Message
	}
	s.replyError(c, id, code, msg)
	if s.Err != nil {
		fmt.Fprintln(s.Err, "kothar acp:", msg)
	}
}
func (s *Server) replyError(c *codec, id json.RawMessage, code int, msg string) {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	c.write(response{JSONRPC: "2.0", ID: id, Error: &rpcError{code, msg}})
}
