package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/bhargavakumark/llm-cli/cmd"
)

var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildDate = "unknown"
)

func main() {
	time.Local = time.UTC

	cmd.Version = Version
	cmd.GitCommit = GitCommit
	cmd.BuildDate = BuildDate

	if err := cmd.Execute(); err != nil {
		// An interrupt is not a failure of the tool, so it exits 130 the way
		// a shell expects, without an error banner.
		if errors.Is(err, context.Canceled) {
			fmt.Fprintln(os.Stderr, "cancelled")
			os.Exit(130)
		}

		fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
		os.Exit(1)
	}
}
