// Package engine defines engine-independent execution and static capability contracts.
package engine

import "context"

type Request struct{ SessionID, Prompt string }
type Event struct{ Kind, Text string }
type Runner interface {
	Run(context.Context, Request, func(Event) error) error
	Cancel(string) error
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
		Providers: []string{"anthropic", "openai", "google", "github-copilot", "ollama"},
		Bundles:   []string{"buzz", "workspace", "git"},
		Tools:     []string{"read", "write", "edit", "bash", "grep", "find", "web_search", "fetch_content"}}, true
}
