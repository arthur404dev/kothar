package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

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
