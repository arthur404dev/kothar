// Package framework orchestrates normalized agent sessions independently of transports and engines.
package framework

import (
	"context"
	"errors"
	"sync"

	"github.com/arthur404dev/kothar/internal/engine"
)

type Agent = engine.Agent
type ModelPolicy = engine.ModelPolicy
type ToolPolicy = engine.ToolPolicy
type Content = engine.Content
type Event = engine.Event
type StopReason = engine.StopReason

const (
	EndTurn         = engine.EndTurn
	Cancelled       = engine.Cancelled
	MaxTokens       = engine.MaxTokens
	MaxTurnRequests = engine.MaxTurnRequests
	Refusal         = engine.Refusal
)

type ErrorClass string

const (
	InvalidRequest    ErrorClass = "invalid_request"
	UnknownSession    ErrorClass = "unknown_session"
	Auth              ErrorClass = "auth"
	Policy            ErrorClass = "policy"
	EngineUnavailable ErrorClass = "engine_unavailable"
	Provider          ErrorClass = "provider"
	Timeout           ErrorClass = "timeout"
	CancelTimeout     ErrorClass = "cancel_timeout"
	Protocol          ErrorClass = "protocol"
	Internal          ErrorClass = "internal"
)

type Error struct {
	Class   ErrorClass
	Message string
	Cause   error
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.Cause }
func NewError(class ErrorClass, message string, cause error) error {
	return &Error{class, message, cause}
}

type NewSession struct {
	ID, CWD, SystemPrompt string
	MCPServers            int
}
type Request struct {
	SessionID string
	Content   []Content
}
type session struct {
	runner engine.SessionRunner
	turn   sync.Mutex
	mu     sync.Mutex
	cancel context.CancelFunc
}
type Service struct {
	Agent    Agent
	Factory  engine.Factory
	mu       sync.RWMutex
	sessions map[string]*session
}

func New(agent Agent, factory engine.Factory) *Service {
	return &Service{Agent: agent, Factory: factory, sessions: map[string]*session{}}
}
func (s *Service) NewSession(ctx context.Context, in NewSession) error {
	if in.ID == "" || in.CWD == "" {
		return NewError(InvalidRequest, "session id and cwd are required", nil)
	}
	if in.MCPServers != 0 {
		return NewError(Policy, "mcpServers are not supported", nil)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[in.ID]; ok {
		return NewError(InvalidRequest, "duplicate session", nil)
	}
	r, err := s.Factory.New(ctx, s.Agent, engine.Session{ID: in.ID, CWD: in.CWD, SystemPrompt: in.SystemPrompt})
	if err != nil {
		return normalize(err)
	}
	s.sessions[in.ID] = &session{runner: r}
	return nil
}
func (s *Service) Prompt(ctx context.Context, in Request, emit func(Event) error) (StopReason, error) {
	x := s.get(in.SessionID)
	if x == nil {
		return "", NewError(UnknownSession, "unknown session", nil)
	}
	if len(in.Content) == 0 {
		return "", NewError(InvalidRequest, "prompt content is required", nil)
	}
	x.turn.Lock()
	defer x.turn.Unlock()
	ctx, cancel := context.WithCancel(ctx)
	x.mu.Lock()
	x.cancel = cancel
	x.mu.Unlock()
	defer func() { cancel(); x.mu.Lock(); x.cancel = nil; x.mu.Unlock() }()
	stop, err := x.runner.Prompt(ctx, engine.Request{SessionID: in.SessionID, Content: in.Content}, emit)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return Cancelled, nil
		}
		return "", normalize(err)
	}
	switch stop {
	case EndTurn, Cancelled, MaxTokens, MaxTurnRequests, Refusal:
		return stop, nil
	}
	return "", NewError(Protocol, "engine returned invalid stop reason", nil)
}
func (s *Service) Cancel(id string) {
	x := s.get(id)
	if x == nil {
		return
	}
	x.mu.Lock()
	if x.cancel != nil {
		x.cancel()
	}
	x.mu.Unlock()
}
func (s *Service) CancelAll() {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, x := range s.sessions {
		x.mu.Lock()
		if x.cancel != nil {
			x.cancel()
		}
		x.mu.Unlock()
	}
}
func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var first error
	for id, x := range s.sessions {
		x.mu.Lock()
		if x.cancel != nil {
			x.cancel()
		}
		x.mu.Unlock()
		if err := x.runner.Close(); err != nil && first == nil {
			first = err
		}
		delete(s.sessions, id)
	}
	return first
}
func (s *Service) get(id string) *session { s.mu.RLock(); defer s.mu.RUnlock(); return s.sessions[id] }
func normalize(err error) error {
	var e *Error
	if errors.As(err, &e) {
		return err
	}
	return NewError(EngineUnavailable, "engine unavailable", err)
}
