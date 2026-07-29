package main

import (
	"fmt"
	"os"

	"github.com/arthur404dev/kothar/cmd/kothar/internal/cli"
)

var version = "dev"

func main() {
	if err := cli.Execute(version); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
