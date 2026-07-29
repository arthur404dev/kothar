package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

func completion(root *cobra.Command) *cobra.Command {
	return &cobra.Command{Use: "completion [bash|zsh|fish|powershell]", Short: "Generate shell completion", Args: cobra.ExactArgs(1), ValidArgs: []string{"bash", "zsh", "fish", "powershell"}, RunE: func(cmd *cobra.Command, args []string) error {
		return writeCompletion(root, args[0], cmd.OutOrStdout())
	}}
}

func writeCompletion(root *cobra.Command, shell string, out io.Writer) error {
	switch shell {
	case "bash":
		return root.GenBashCompletion(out)
	case "zsh":
		return root.GenZshCompletion(out)
	case "fish":
		return root.GenFishCompletion(out, true)
	case "powershell":
		return root.GenPowerShellCompletion(out)
	default:
		return fmt.Errorf("unsupported shell %q", shell)
	}
}
