// Package engine defines the contract implemented by execution engines.
package engine

import "context"

type Agent struct {
	ID, SystemPrompt   string
	Models             ModelPolicy
	Tools              ToolPolicy
	Resources          map[string][]byte
	Skills, Extensions []string
	Credentials        CredentialPolicy
}
type CredentialPolicy struct {
	Mode, HostAuth string
	Overrides      map[string]string
	StoreRoot      string
	Refresh        bool
}
type ModelPolicy struct {
	Primary     string
	Fallbacks   []string
	Thinking    string
	MaxAttempts int
}
type ToolPolicy struct{ Bundles, Allow, Deny []string }
type Session struct{ ID, CWD, SystemPrompt string }
type Request struct {
	SessionID string
	Content   []Content
}
type Content struct{ Type, Text string }
type Event struct{ Type, Text, ToolCallID, Status, Model string }

type Failure struct {
	Class string
	Safe  string
	Err   error
}

func (e *Failure) Error() string { return e.Safe }
func (e *Failure) Unwrap() error { return e.Err }

type StopReason string

const (
	EndTurn         StopReason = "end_turn"
	Cancelled       StopReason = "cancelled"
	MaxTokens       StopReason = "max_tokens"
	MaxTurnRequests StopReason = "max_turn_requests"
	Refusal         StopReason = "refusal"
)

type SessionRunner interface {
	Prompt(context.Context, Request, func(Event) error) (StopReason, error)
	Close() error
}
type Factory interface {
	New(context.Context, Agent, Session) (SessionRunner, error)
}

type Capability struct {
	Name, Command, Version    string
	Providers, Tools, Bundles []string
}

const PiVersion = "0.82.1"

func Lookup(name string) (Capability, bool) {
	if name != "pi" {
		return Capability{}, false
	}
	return Capability{Name: name, Command: "/usr/local/libexec/kothar/pi", Version: PiVersion,
		Providers: []string{"anthropic", "openai", "openai-codex", "google", "github-copilot", "ollama"},
		Bundles:   []string{"buzz", "workspace", "git"}, Tools: []string{"read", "write", "edit", "bash", "grep", "find", "web_search", "fetch_content"}}, true
}
