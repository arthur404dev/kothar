// Package cli is presentation and wiring for Kothar's control plane.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/arthur404dev/kothar/internal/acp"
	"github.com/arthur404dev/kothar/internal/config"
	"github.com/arthur404dev/kothar/internal/credentials"
	deploy "github.com/arthur404dev/kothar/internal/deploy/systemd"
	"github.com/arthur404dev/kothar/internal/engine"
	"github.com/arthur404dev/kothar/internal/framework"
	"github.com/arthur404dev/kothar/internal/inbound"
	"github.com/arthur404dev/kothar/internal/manifest"
	"github.com/arthur404dev/kothar/internal/records"
	"github.com/arthur404dev/kothar/internal/securefs"
	"github.com/arthur404dev/kothar/internal/xdg"
	"github.com/carapace-sh/carapace"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

type app struct {
	paths   xdg.Paths
	store   records.Store
	runner  deploy.Runner
	factory engine.Factory
}

type unavailableFactory struct{}

func (unavailableFactory) New(context.Context, engine.Agent, engine.Session) (engine.SessionRunner, error) {
	return nil, framework.NewError(framework.EngineUnavailable, "engine unavailable", nil)
}

func New(version string) *cobra.Command { return newCommand(version, unavailableFactory{}) }
func NewWithFactory(version string, factory engine.Factory) *cobra.Command {
	return newCommand(version, factory)
}
func newCommand(version string, factory engine.Factory) *cobra.Command {
	p, _ := xdg.Resolve()
	a := &app{p, records.Store{Config: p.Config, State: p.State}, deploy.ExecRunner{}, factory}
	if version == "" {
		version = "dev"
	}
	r := &cobra.Command{Use: "kothar", Short: "Forge autonomous agents from declarative intent", Version: version, SilenceUsage: true, SilenceErrors: true, RunE: help}
	r.AddCommand(a.agents(), a.engines(), a.adapters(), a.credentials(), a.config(), a.doctor(), completion(r))
	serve := group("serve", "Run protocol endpoints")
	serve.AddCommand(a.serveACP())
	r.AddCommand(serve)
	carapace.Gen(r).Standalone()
	return r
}
func (a *app) serveACP() *cobra.Command {
	c := &cobra.Command{Use: "acp <id>", Args: cobra.ExactArgs(1), RunE: func(c *cobra.Command, x []string) error {
		r, err := a.load(x[0], nil)
		if err != nil {
			return err
		}
		m := r.Manifest
		system := string(r.Resources[m.Behavior.SystemPrompt])
		svc := framework.New(engine.Agent{ID: m.ID, SystemPrompt: system, Models: engine.ModelPolicy{Primary: m.Models.Primary, Fallbacks: m.Models.Fallbacks, Thinking: m.Models.Thinking}, Tools: engine.ToolPolicy{Bundles: m.Tools.Bundles, Allow: m.Tools.Allow, Deny: m.Tools.Deny}}, a.factory)
		return (&acp.Server{In: c.InOrStdin(), Out: c.OutOrStdout(), Err: c.ErrOrStderr(), Service: svc}).Serve(c.Context())
	}}
	completeAgents(c, a)
	return c
}
func (a *app) agents() *cobra.Command {
	g := group("agent", "Manage agent records")
	create := &cobra.Command{Use: "create <id>", Args: cobra.ExactArgs(1), RunE: func(c *cobra.Command, x []string) error {
		cfg, err := config.Load(a.paths.Config)
		if err != nil {
			return err
		}
		return a.store.Create(x[0], defaultManifest(x[0], cfg))
	}}
	list := a.jsonCmd("list", cobra.NoArgs, func([]string) (any, error) { return a.store.List() })
	g.AddCommand(create, list, a.show(), a.edit(), a.validate(), a.render(), a.diff(), a.apply(), a.status(), a.logs())
	for _, action := range []string{"start", "stop", "restart"} {
		act := action
		g.AddCommand(a.agentCmd(act, func(c *cobra.Command, id string) error {
			_, e := deploy.Service(c.Context(), a.runner, id, act)
			return e
		}))
	}
	g.AddCommand(a.agentCmd("remove", func(c *cobra.Command, id string) error {
		receipt, e := a.receipt(id)
		if e != nil {
			return e
		}
		if receipt == nil {
			return nil
		}
		if _, e := deploy.Service(c.Context(), a.runner, id, "stop"); e != nil {
			return e
		}
		if _, e := deploy.Service(c.Context(), a.runner, id, "disable"); e != nil {
			return e
		}
		return os.RemoveAll(filepath.Dir(a.store.Receipt(id)))
	}))
	g.AddCommand(a.agentCmd("delete", func(_ *cobra.Command, id string) error { return a.store.Delete(id) }))
	return g
}
func (a *app) agentCmd(name string, fn func(*cobra.Command, string) error) *cobra.Command {
	c := &cobra.Command{Use: name + " <id>", Args: cobra.ExactArgs(1), RunE: func(c *cobra.Command, x []string) error { return fn(c, x[0]) }}
	completeAgents(c, a)
	return c
}
func (a *app) show() *cobra.Command {
	return a.jsonCmd("show <id>", cobra.ExactArgs(1), func(x []string) (any, error) {
		r, e := a.load(x[0], nil)
		if e != nil {
			return nil, e
		}
		return r.Manifest, nil
	})
}
func (a *app) edit() *cobra.Command {
	return a.agentCmd("edit", func(_ *cobra.Command, id string) error { return a.store.Edit(id, "") })
}
func (a *app) validate() *cobra.Command {
	c := a.agentCmd("validate", func(c *cobra.Command, id string) error {
		roots, _ := c.Flags().GetStringSlice("allow-mount-root")
		_, e := a.load(id, roots)
		if e == nil {
			fmt.Fprintln(c.OutOrStdout(), "valid")
		}
		return e
	})
	c.Flags().StringSlice("allow-mount-root", nil, "allowed host mount root")
	return c
}

type loadedRecord struct {
	Manifest  *manifest.Manifest
	Effective []byte
	Resources map[string][]byte
}

func (a *app) load(id string, restrictions []string) (*loadedRecord, error) {
	b, e := a.store.Read(id)
	if e != nil {
		return nil, e
	}
	m, e := manifest.DecodeBytes(b)
	if e != nil {
		return nil, e
	}
	if m.ID != id {
		return nil, fmt.Errorf("record id does not match directory")
	}
	d, _ := a.store.Dir(id)
	cfg, e := config.Load(a.paths.Config)
	if e != nil {
		return nil, e
	}
	if e = m.ValidateResources(d, config.RestrictRoots(cfg.HostPolicy.AllowedMountRoots, restrictions)); e != nil {
		return nil, e
	}
	resources := map[string][]byte{}
	for _, rel := range append(append(append([]string{m.Behavior.SystemPrompt}, m.Behavior.ContextFiles...), m.Behavior.Skills...), m.Behavior.Extensions...) {
		if _, exists := resources[rel]; exists {
			continue
		}
		resources[rel], _, e = securefs.ReadFile(d, filepath.FromSlash(rel), manifest.MaxBytes)
		if e != nil {
			return nil, e
		}
	}
	effective, _ := json.Marshal(m)
	return &loadedRecord{m, effective, resources}, nil
}
func (a *app) render() *cobra.Command {
	return a.jsonCmd("render <id>", cobra.ExactArgs(1), func(x []string) (any, error) {
		r, e := a.load(x[0], nil)
		if e != nil {
			return nil, e
		}
		p, e := a.plan(x[0], r, nil, false)
		return map[string]any{"effective": r.Manifest, "plan": p}, e
	})
}
func (a *app) receipt(id string) (*deploy.Receipt, error) {
	r, err := deploy.LoadReceipt(a.store.Receipt(id), id)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return r, err
}
func (a *app) plan(id string, r *loadedRecord, prev *deploy.Receipt, refresh bool) (deploy.Plan, error) {
	arts := []deploy.Artifact{{Path: "etc/kothar/agents/" + id + "/agent.json", Mode: 0600, Category: "config", Content: r.Effective}}
	for name, b := range r.Resources {
		arts = append(arts, deploy.Artifact{Path: "etc/kothar/agents/" + id + "/" + filepath.ToSlash(name), Mode: 0600, Category: "behavior", Content: b})
	}
	arts = append(arts, deploy.Artifact{Path: "etc/systemd/system/" + deploy.Unit(id) + ".d/10-kothar.conf", Mode: 0644, Category: "service", Content: []byte("# runtime artifacts supplied by tasks 4-6\n")})
	return deploy.Build(id, r.Effective, prev, refresh, arts), nil
}
func (a *app) diff() *cobra.Command {
	return a.jsonCmd("diff <id>", cobra.ExactArgs(1), func(x []string) (any, error) {
		r, e := a.load(x[0], nil)
		if e != nil {
			return nil, e
		}
		prev, e := a.receipt(x[0])
		if e != nil {
			return nil, e
		}
		return a.plan(x[0], r, prev, false)
	})
}
func (a *app) apply() *cobra.Command {
	c := a.agentCmd("apply", func(c *cobra.Command, id string) error {
		if _, e := a.receipt(id); e != nil {
			return e
		}
		roots, _ := c.Flags().GetStringSlice("allow-mount-root")
		r, e := a.load(id, roots)
		if e != nil {
			return e
		}
		m := r.Manifest
		items, e := (credentials.Store{Root: filepath.Join(a.paths.State, "credentials")}).List()
		if e != nil {
			return e
		}
		have := map[string]bool{}
		for _, item := range items {
			have[item.Name] = true
		}
		refs := []string{m.Inbound.Options.IdentityCredential}
		for _, ref := range m.Engine.Credentials.Overrides {
			refs = append(refs, ref)
		}
		for _, ref := range m.Tools.Credentials {
			refs = append(refs, ref)
		}
		for _, ref := range refs {
			if !have[ref] {
				return fmt.Errorf("required credential %q is not installed", ref)
			}
		}
		return deploy.ErrRuntimeIncomplete
	})
	c.Flags().Bool("refresh-credentials", false, "reseed inherited provider credentials")
	c.Flags().StringSlice("allow-mount-root", nil, "allowed host mount root")
	return c
}
func (a *app) status() *cobra.Command {
	return a.jsonCmd("status <id>", cobra.ExactArgs(1), func(x []string) (any, error) {
		if _, e := a.receipt(x[0]); e != nil {
			return nil, e
		}
		out, e := deploy.Service(context.Background(), a.runner, x[0], "status")
		if e != nil {
			return nil, e
		}
		v := map[string]string{"agent_id": x[0], "unit": deploy.Unit(x[0])}
		for _, line := range strings.Split(string(out), "\n") {
			if k, z, ok := strings.Cut(line, "="); ok {
				v[strings.ToLower(k)] = z
			}
		}
		return v, nil
	})
}
func (a *app) logs() *cobra.Command {
	var lines int
	var since string
	c := a.agentCmd("logs", func(c *cobra.Command, id string) error {
		b, e := deploy.Logs(c.Context(), a.runner, id, lines, since)
		if e == nil {
			_, e = c.OutOrStdout().Write(b)
		}
		return e
	})
	c.Flags().IntVarP(&lines, "lines", "n", 200, "number of lines (1..10000)")
	c.Flags().StringVar(&since, "since", "", "journal time expression")
	return c
}
func (a *app) engines() *cobra.Command {
	g := group("engine", "Inspect engines")
	g.AddCommand(a.jsonCmd("list", cobra.NoArgs, func([]string) (any, error) { return []string{"pi"}, nil }), a.jsonCmd("show <name>", cobra.ExactArgs(1), func(x []string) (any, error) {
		v, ok := engine.Lookup(x[0])
		if !ok {
			return nil, fmt.Errorf("unknown engine")
		}
		return v, nil
	}))
	return g
}
func (a *app) adapters() *cobra.Command {
	g := group("adapter", "Inspect adapters")
	g.AddCommand(a.jsonCmd("list", cobra.NoArgs, func([]string) (any, error) { return []string{"buzz"}, nil }), a.jsonCmd("show <name>", cobra.ExactArgs(1), func(x []string) (any, error) {
		v, ok := inbound.Lookup(x[0])
		if !ok {
			return nil, fmt.Errorf("unknown adapter")
		}
		return v, nil
	}))
	return g
}
func (a *app) credentials() *cobra.Command {
	g := group("credential", "Manage credentials")
	s := credentials.Store{Root: filepath.Join(a.paths.State, "credentials")}
	var file string
	set := &cobra.Command{Use: "set <name>", Args: cobra.ExactArgs(1), RunE: func(c *cobra.Command, x []string) error {
		var r io.Reader = c.InOrStdin()
		if file != "" {
			base, name := filepath.Dir(file), filepath.Base(file)
			b, fi, e := securefs.ReadFile(base, name, securefs.MaxFile)
			if e != nil {
				return e
			}
			st, ok := fi.Sys().(*syscall.Stat_t)
			if fi.Mode().Perm() != 0600 || !ok || int(st.Uid) != os.Geteuid() {
				return fmt.Errorf("credential file must be owned by the current user and protected (0600)")
			}
			r = bytes.NewReader(b)
		} else if r == os.Stdin && term.IsTerminal(int(os.Stdin.Fd())) {
			b, e := term.ReadPassword(int(os.Stdin.Fd()))
			if e != nil {
				return e
			}
			r = bytes.NewReader(b)
		}
		return s.Set(x[0], r)
	}}
	set.Flags().StringVar(&file, "file", "", "read protected credential file")
	g.AddCommand(set, a.jsonCmd("list", cobra.NoArgs, func([]string) (any, error) { return s.List() }), &cobra.Command{Use: "remove <name>", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, x []string) error { return s.Remove(x[0]) }})
	return g
}
func (a *app) config() *cobra.Command {
	g := group("config", "Inspect configuration")
	path := &cobra.Command{Use: "path", Args: cobra.NoArgs, RunE: func(c *cobra.Command, _ []string) error { fmt.Fprintln(c.OutOrStdout(), a.paths.Config); return nil }}
	show := a.jsonCmd("show", cobra.NoArgs, func([]string) (any, error) { return config.Load(a.paths.Config) })
	edit := &cobra.Command{Use: "edit", Args: cobra.NoArgs, RunE: func(c *cobra.Command, _ []string) error {
		p := filepath.Join(a.paths.Config, "config.json")
		if _, e := os.Stat(p); errors.Is(e, os.ErrNotExist) {
			if e = records.AtomicWrite(p, []byte("{\n  \"agent_defaults\": {},\n  \"host_policy\": {\"allowed_mount_roots\": []}\n}\n"), 0600); e != nil {
				return e
			}
		}
		return editJSONAtomic(p)
	}}
	g.AddCommand(path, show, edit)
	return g
}
func (a *app) doctor() *cobra.Command {
	return a.jsonCmd("doctor", cobra.NoArgs, func([]string) (any, error) {
		checks := map[string]any{"goos": runtime.GOOS, "config": a.paths.Config}
		for _, name := range []string{"systemctl", "journalctl", "pi", "buzz-acp"} {
			p, e := exec.LookPath(name)
			checks[name] = map[string]any{"available": e == nil, "path": p}
		}
		for k, p := range map[string]string{"config_writable": a.paths.Config, "state_writable": a.paths.State, "cache_writable": a.paths.Cache} {
			e := os.MkdirAll(p, 0700)
			checks[k] = e == nil
		}
		return checks, nil
	})
}
func (a *app) jsonCmd(use string, args cobra.PositionalArgs, fn func([]string) (any, error)) *cobra.Command {
	var jsonOut bool
	c := &cobra.Command{Use: use, Args: args, RunE: func(c *cobra.Command, x []string) error {
		v, e := fn(x)
		if e != nil {
			return e
		}
		if jsonOut {
			return json.NewEncoder(c.OutOrStdout()).Encode(v)
		}
		switch z := v.(type) {
		case []string:
			for _, s := range z {
				fmt.Fprintln(c.OutOrStdout(), s)
			}
		default:
			b, _ := json.MarshalIndent(v, "", "  ")
			fmt.Fprintln(c.OutOrStdout(), string(b))
		}
		return nil
	}}
	c.Flags().BoolVar(&jsonOut, "json", false, "emit deterministic JSON")
	if strings.Contains(use, "<id>") {
		completeAgents(c, a)
	}
	return c
}
func completeAgents(c *cobra.Command, a *app) {
	c.ValidArgsFunction = func(_ *cobra.Command, _ []string, prefix string) ([]string, cobra.ShellCompDirective) {
		ids, _ := a.store.List()
		out := []string{}
		for _, id := range ids {
			if strings.HasPrefix(id, prefix) {
				out = append(out, id)
			}
		}
		sort.Strings(out)
		return out, cobra.ShellCompDirectiveNoFileComp
	}
	carapace.Gen(c).PositionalCompletion(carapace.ActionCallback(func(ctx carapace.Context) carapace.Action {
		ids, _ := a.store.List()
		return carapace.ActionValues(ids...)
	}))
}
func group(name, short string) *cobra.Command {
	return &cobra.Command{Use: name, Short: short, RunE: help}
}
func help(c *cobra.Command, x []string) error {
	if len(x) > 0 {
		return fmt.Errorf("unknown command %q for %q", x[0], c.CommandPath())
	}
	return c.Help()
}
func editJSONAtomic(p string) error {
	b, _, err := securefs.ReadFile(filepath.Dir(p), filepath.Base(p), manifest.MaxBytes)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".config-editor-")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.Write(b)
	}
	tmp.Close()
	if err != nil {
		return err
	}
	if err = editFile(name); err != nil {
		return err
	}
	b, err = os.ReadFile(name)
	if err != nil {
		return err
	}
	if _, err = config.DecodeBytes(b); err != nil {
		return err
	}
	return records.AtomicWrite(p, b, 0600)
}
func editFile(p string) error {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		return fmt.Errorf("EDITOR is not set")
	}
	cmd := exec.Command(editor, p)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
func Execute(version string) error { return New(version).ExecuteContext(context.Background()) }
func defaultManifest(id string, cfg config.Config) []byte {
	title := strings.ToUpper(id[:1]) + id[1:]
	v := map[string]any{"version": 1, "id": id, "profile": map[string]any{"display_name": title, "description": "", "labels": []string{}}, "inbound": map[string]any{"name": "buzz", "options": map[string]any{"relay": "wss://buzz.4o4.one", "identity_credential": "buzz-" + id, "respond_to": map[string]any{"mode": "nobody", "pubkeys": []string{}}, "heartbeat_seconds": 300}}, "engine": map[string]any{"name": "pi", "credentials": map[string]any{"mode": "inherit", "overrides": map[string]string{}}, "options": map[string]any{"project_trust": "never", "telemetry": false, "update_checks": false}}, "models": map[string]any{"primary": "anthropic/claude-sonnet-4-6", "fallbacks": []string{}, "thinking": "high", "max_attempts": 2}, "behavior": map[string]any{"system_prompt": "SYSTEM.md", "context_files": []string{"AGENTS.md", "CONSTRAINTS.md"}, "skills": []string{}, "extensions": []string{}}, "tools": map[string]any{"bundles": []string{"buzz", "workspace", "git"}, "allow": []string{}, "deny": []string{}, "credentials": map[string]string{}}, "workspace": map[string]any{"root": "workspace", "mounts": []any{}}, "permissions": map[string]any{"network": map[string]any{"mode": "full"}, "resources": map[string]any{"memory_max_mb": 4096, "cpu_quota_percent": 200, "tasks_max": 512}}, "runtime": map[string]any{"driver": "systemd", "start_on_boot": true, "restart": "always", "workers": 1}}
	if cfg.AgentDefaults.Models != nil {
		v["models"] = cfg.AgentDefaults.Models
	}
	if cfg.AgentDefaults.Permissions != nil {
		v["permissions"] = cfg.AgentDefaults.Permissions
	}
	b, _ := json.MarshalIndent(v, "", "  ")
	return append(b, '\n')
}

var _ = strconv.Itoa
