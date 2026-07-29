// Package pi fixes the Pi process boundary and reviewed capabilities.
package pi

import "github.com/arthur404dev/kothar/internal/engine"

const Version = "0.82.1"

func Capability() engine.Capability {
	return engine.Capability{Name: "pi", Command: "/usr/local/libexec/kothar/pi", Version: Version,
		Providers: []string{"anthropic", "openai", "google", "github-copilot", "ollama"},
		Bundles:   []string{"buzz", "workspace", "git"},
		Tools:     []string{"read", "write", "edit", "bash", "grep", "find", "web_search", "fetch_content"}}
}
