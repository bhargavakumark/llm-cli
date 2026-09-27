package main

import (
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
		fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
		os.Exit(1)
	}
}
