// Package cli defines Kothar's stable command contract.
package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/arthur404dev/kothar/internal/xdg"
	"github.com/carapace-sh/carapace"
	"github.com/spf13/cobra"
)

func New(version string) *cobra.Command {
	if version == "" {
		version = "dev"
	}
	root := &cobra.Command{Use: "kothar", Short: "Forge autonomous agents from declarative intent", Version: version, SilenceUsage: true, SilenceErrors: true, RunE: help}
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error { return err })
	root.AddCommand(agentGroup())
	root.AddCommand(group("engine", "Inspect engine capabilities", []string{"list", "show"}))
	root.AddCommand(group("adapter", "Inspect inbound adapter capabilities", []string{"list", "show"}))
	root.AddCommand(group("credential", "Manage named credential references", []string{"set", "list", "remove"}))
	config := group("config", "Inspect Kothar configuration", []string{"show", "edit"})
	config.AddCommand(&cobra.Command{Use: "path", Short: "Print the configuration directory", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		p, err := xdg.Resolve()
		if err == nil {
			fmt.Fprintln(cmd.OutOrStdout(), p.Config)
		}
		return err
	}})
	root.AddCommand(config)
	root.AddCommand(stub("doctor"))
	serve := group("serve", "Run protocol endpoints", []string{"acp"})
	serve.Hidden = true
	root.AddCommand(serve)
	root.AddCommand(completion(root))
	carapace.Gen(root).Standalone()
	return root
}

func agentGroup() *cobra.Command {
	cmd := group("agent", "Manage agent records", []string{"create", "list", "show", "edit", "validate", "render", "diff", "apply", "status", "logs", "start", "stop", "restart", "remove", "delete"})
	for _, child := range cmd.Commands() {
		if child.Name() == "create" || child.Name() == "list" {
			continue
		}
		child.ValidArgsFunction = func(_ *cobra.Command, _ []string, prefix string) ([]string, cobra.ShellCompDirective) {
			return agentIDs(prefix), cobra.ShellCompDirectiveNoFileComp
		}
		carapace.Gen(child).PositionalCompletion(carapace.ActionCallback(func(c carapace.Context) carapace.Action {
			return carapace.ActionValues(agentIDs(c.Value)...)
		}))
	}
	return cmd
}

func agentIDs(prefix string) []string {
	paths, err := xdg.Resolve()
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(filepath.Join(paths.Config, "agents"))
	if err != nil {
		return nil
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), prefix) {
			if _, err := os.Stat(filepath.Join(paths.Config, "agents", entry.Name(), "agent.json")); err == nil {
				ids = append(ids, entry.Name())
			}
		}
	}
	return ids
}

func group(name, short string, children []string) *cobra.Command {
	cmd := &cobra.Command{Use: name, Short: short, RunE: help}
	for _, child := range children {
		cmd.AddCommand(stub(child))
	}
	return cmd
}

func stub(name string) *cobra.Command {
	cmd := &cobra.Command{Use: name, Short: strings.ToUpper(name[:1]) + name[1:], Args: cobra.ArbitraryArgs}
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return fmt.Errorf("%s: not implemented", cmd.CommandPath())
	}
	if name == "apply" {
		cmd.Flags().Bool("refresh-credentials", false, "reseed inherited provider credentials")
	}
	return cmd
}

func help(cmd *cobra.Command, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("unknown command %q for %q", args[0], cmd.CommandPath())
	}
	return cmd.Help()
}

func Execute(version string) error {
	err := New(version).ExecuteContext(context.Background())
	if err != nil && strings.Contains(err.Error(), "unknown command") {
		return fmt.Errorf("%w\nTry `kothar --help` for more context.", err)
	}
	return err
}
