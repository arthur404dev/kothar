package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/arthur404dev/kothar/internal/config"
	deploy "github.com/arthur404dev/kothar/internal/deploy/systemd"
	"github.com/arthur404dev/kothar/internal/engine"
	"github.com/arthur404dev/kothar/internal/records"
	"github.com/arthur404dev/kothar/internal/xdg"
	"github.com/spf13/cobra"
)

func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := New("1.2.3")
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	return out.String(), err
}
func TestHelpVersionAndTree(t *testing.T) {
	out, err := run(t, "--help")
	if err != nil || !strings.Contains(out, "Forge autonomous agents") {
		t.Fatalf("help: %q %v", out, err)
	}
	out, err = run(t, "--version")
	if err != nil || strings.TrimSpace(out) != "kothar version 1.2.3" {
		t.Fatalf("version: %q %v", out, err)
	}
	want := map[string][]string{"agent": {"create", "list", "show", "edit", "validate", "render", "diff", "apply", "status", "logs", "start", "stop", "restart", "remove", "delete"}, "engine": {"list", "show"}, "adapter": {"list", "show"}, "credential": {"set", "list", "remove"}, "config": {"path", "show", "edit"}, "serve": {"acp"}}
	root := New("dev")
	for parent, children := range want {
		p, _, e := root.Find([]string{parent})
		if e != nil {
			t.Fatal(e)
		}
		for _, child := range children {
			c, _, e := root.Find([]string{parent, child})
			if e != nil || c.Parent() != p {
				t.Fatalf("missing %s %s", parent, child)
			}
		}
	}
}
func TestNounHelpAndErrorsDeterministic(t *testing.T) {
	out, err := run(t, "agent")
	if err != nil || !strings.Contains(out, "Available Commands") {
		t.Fatalf("noun help: %q %v", out, err)
	}
	_, err = run(t, "agent", "nope")
	if err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("error: %v", err)
	}
}

type cliFakeFactory struct{}
type cliFakeRunner struct{}

func (cliFakeFactory) New(context.Context, engine.Agent, engine.Session) (engine.SessionRunner, error) {
	return cliFakeRunner{}, nil
}
func (cliFakeRunner) Prompt(_ context.Context, _ engine.Request, emit func(engine.Event) error) (engine.StopReason, error) {
	_ = emit(engine.Event{Type: "agent_message_chunk", Text: "OK"})
	return engine.EndTurn, nil
}
func (cliFakeRunner) Close() error { return nil }
func TestServeACPComposition(t *testing.T) {
	configDir, stateDir := t.TempDir(), t.TempDir()
	t.Setenv("KOTHAR_CONFIG_DIR", configDir)
	t.Setenv("KOTHAR_STATE_DIR", stateDir)
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{"agent_defaults":{},"host_policy":{"allowed_mount_roots":[]}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, "agent", "create", "alpha"); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(configDir, "agents", "alpha")
	for _, name := range []string{"SYSTEM.md", "AGENTS.md", "CONSTRAINTS.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("safe\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "workspace"), 0700); err != nil {
		t.Fatal(err)
	}
	cmd := NewWithFactory("test", cliFakeFactory{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetIn(strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"session/new","params":{"cwd":"/tmp","mcpServers":[]}}` + "\n" + `{"jsonrpc":"2.0","id":2,"method":"session/prompt","params":{"sessionId":"s-1","prompt":[{"type":"text","text":"go"}]}}` + "\n"))
	cmd.SetArgs([]string{"serve", "acp", "alpha"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"text":"OK"`) || !strings.Contains(out.String(), `"stopReason":"end_turn"`) {
		t.Fatalf("stdout=%s", out.String())
	}
}
func TestServeACPHelp(t *testing.T) {
	out, err := run(t, "serve", "acp", "--help")
	if err != nil || !strings.Contains(out, "Usage:") {
		t.Fatalf("serve acp help: %q %v", out, err)
	}
}

func TestCompletion(t *testing.T) {
	out, err := run(t, "completion", "bash")
	if err != nil || !strings.Contains(out, "bash completion") {
		t.Fatalf("completion: %d %v", len(out), err)
	}
	if cmd, _, err := New("dev").Find([]string{"_carapace"}); err != nil || cmd.Name() != "_carapace" {
		t.Fatalf("Carapace standalone command missing: %v", err)
	}
}

func TestCreateCopiesDefaultsOnceAndRejectsAliasedRecord(t *testing.T) {
	configDir, stateDir := t.TempDir(), t.TempDir()
	t.Setenv("KOTHAR_CONFIG_DIR", configDir)
	t.Setenv("KOTHAR_STATE_DIR", stateDir)
	cfg := `{"agent_defaults":{"models":{"primary":"openai/gpt-5","fallbacks":[],"thinking":"low","max_attempts":1}},"host_policy":{"allowed_mount_roots":[]}}`
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(cfg), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, "agent", "create", "alpha"); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(configDir, "agents", "alpha", "agent.json")
	before, _ := os.ReadFile(manifestPath)
	if !bytes.Contains(before, []byte(`"primary": "openai/gpt-5"`)) {
		t.Fatal("defaults not copied")
	}
	cfg = `{"agent_defaults":{"models":{"primary":"ollama/local","fallbacks":[],"thinking":"off","max_attempts":1}},"host_policy":{"allowed_mount_roots":[]}}`
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(cfg), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, "agent", "show", "alpha", "--json"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(manifestPath)
	if !bytes.Equal(before, after) {
		t.Fatal("existing record received defaults overlay")
	}
	if err := os.Symlink(filepath.Join(configDir, "agents", "alpha"), filepath.Join(configDir, "agents", "alias")); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, "agent", "show", "alias", "--json"); err == nil {
		t.Fatal("symlinked agent directory accepted")
	}
}

type countingRunner struct{ calls int }

func (r *countingRunner) Run(context.Context, string, ...string) ([]byte, error) {
	r.calls++
	return []byte("ActiveState=active\n"), nil
}

func TestReceiptMismatchFailsClosedForAgentCommands(t *testing.T) {
	root := t.TempDir()
	paths := xdg.Paths{Config: filepath.Join(root, "config"), State: filepath.Join(root, "state"), Cache: filepath.Join(root, "cache")}
	store := records.Store{Config: paths.Config, State: paths.State}
	if err := store.Create("agent", defaultManifest("agent", config.Config{})); err != nil {
		t.Fatal(err)
	}
	receipt := store.Receipt("agent")
	if err := os.MkdirAll(filepath.Dir(receipt), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(receipt, records.Marshal(deploy.Receipt{AgentID: "other", Artifacts: []deploy.Artifact{}}), 0600); err != nil {
		t.Fatal(err)
	}
	runner := &countingRunner{}
	a := &app{paths: paths, store: store, runner: runner}
	for _, name := range []string{"diff", "apply", "status", "remove", "delete"} {
		cmd := a.agents()
		cmd.SetOut(new(bytes.Buffer))
		cmd.SetArgs([]string{name, "agent"})
		if err := cmd.ExecuteContext(context.Background()); err == nil || !strings.Contains(err.Error(), "does not match requested agent") {
			t.Fatalf("%s accepted mismatched receipt: %v", name, err)
		}
	}
	if runner.calls != 0 {
		t.Fatalf("service mutated before receipt validation: %d calls", runner.calls)
	}
	if _, err := os.Stat(filepath.Join(paths.Config, "agents", "agent")); err != nil {
		t.Fatal("record deleted before receipt validation")
	}
}

func TestCredentialFileRejectsSymlink(t *testing.T) {
	t.Setenv("KOTHAR_CONFIG_DIR", t.TempDir())
	t.Setenv("KOTHAR_STATE_DIR", t.TempDir())
	d := t.TempDir()
	real := filepath.Join(d, "secret")
	link := filepath.Join(d, "link")
	if err := os.WriteFile(real, []byte("value"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, "credential", "set", "test", "--file", link); err == nil {
		t.Fatal("symlink credential accepted")
	}
}

func TestAgentIDCompletionFromXDGRecords(t *testing.T) {
	config := t.TempDir()
	t.Setenv("KOTHAR_CONFIG_DIR", config)
	for _, id := range []string{"zeta", "atlas", "not-a-record"} {
		if err := os.MkdirAll(filepath.Join(config, "agents", id), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"zeta", "atlas"} {
		if err := os.WriteFile(filepath.Join(config, "agents", id, "agent.json"), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd, _, err := New("dev").Find([]string{"agent", "show"})
	if err != nil {
		t.Fatal(err)
	}
	got, directive := cmd.ValidArgsFunction(cmd, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp || !reflect.DeepEqual(got, []string{"atlas", "zeta"}) {
		t.Fatalf("completion = %v, %v", got, directive)
	}
	got, _ = cmd.ValidArgsFunction(cmd, nil, "z")
	if !reflect.DeepEqual(got, []string{"zeta"}) {
		t.Fatalf("prefix completion = %v", got)
	}
}
