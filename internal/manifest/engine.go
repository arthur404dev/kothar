package manifest

import "strings"

// EngineCapability is compiled into kothar; manifests select it by name and
// cannot provide commands, executable paths, or capabilities.
type Executable struct {
	Identity string
	Command  string
	Version  string
	Revision string
}

type EngineCapability struct {
	Executables     map[string]Executable
	Providers       map[string]bool // true means credentials are required
	Unauthenticated map[string]bool
	Bundles         map[string]bool
	Tools           map[string]bool
	SafeFailover    bool
	MaxAttempts     int
}

var engines = map[string]EngineCapability{
	"pi": {
		Executables: map[string]Executable{
			"pi": {Identity: "pi", Command: "/usr/local/libexec/kothar/pi", Version: "0.82.1"},
		},
		Providers:       set("anthropic", "openai", "openai-codex", "google", "github-copilot", "ollama"),
		Unauthenticated: set("ollama"),
		Bundles:         set("buzz", "workspace", "git"),
		Tools:           set("read", "write", "edit", "bash", "grep", "find", "web_search", "fetch_content"),
		SafeFailover:    true,
		MaxAttempts:     10,
	},
}

func set(values ...string) map[string]bool {
	m := make(map[string]bool, len(values))
	for _, value := range values {
		m[value] = true
	}
	return m
}

func modelProvider(model string) (string, bool) {
	provider, name, ok := strings.Cut(model, "/")
	return provider, ok && safeID.MatchString(provider) && modelName.MatchString(name)
}
