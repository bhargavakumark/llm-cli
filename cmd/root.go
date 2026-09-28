package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

const (
	grey  = "\033[90m"
	reset = "\033[0m"
)

var (
	// Version, GitCommit and BuildDate are injected by main.
	Version   = "dev"
	GitCommit = "unknown"
	BuildDate = "unknown"

	llm         string
	quiet       bool
	logRequests bool
)

// rootCmd is built once for the real binary. Tests build their own with
// newRootCmd so that flag state never leaks between runs.
var rootCmd = newRootCmd()

// newRootCmd assembles the root command, its global flags and its subcommands.
func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "llm-cli",
		Short: "Chat with OpenAI-compatible LLM endpoints",
		Long: `llm-cli talks to any OpenAI-compatible chat completions endpoint.

Targets are named endpoints stored in ~/.config/llm-cli/config.json. Each
target carries a base URL, a model id, and either a literal API key or the
name of an environment variable holding that key. Every command accepts
--llm to pick a target by name or alias.

The response is the only thing written to stdout, so it composes:

    llm-cli chat --llm ds "summarise this" > answer.md

Progress, prompts and errors go to stderr.`,
		SilenceErrors: true,
		SilenceUsage:  true,
		CompletionOptions: cobra.CompletionOptions{
			DisableDescriptions: true,
		},
	}

	pf := cmd.PersistentFlags()
	pf.StringVar(&llm, "llm", "", "LLM target name or alias (defaults to the configured default)")
	pf.BoolVarP(&quiet, "quiet", "q", false, "Suppress progress messages")
	pf.BoolVar(&logRequests, "log-requests", false, "Print each outgoing request to stderr in grey")

	if err := cmd.RegisterFlagCompletionFunc("llm", completeLLM); err != nil {
		panic(fmt.Sprintf("register --llm completion: %v", err))
	}

	cmd.AddCommand(newAuthCmd())
	cmd.AddCommand(newChatCmd())
	cmd.AddCommand(newCodeCmd())
	cmd.AddCommand(newModelsCmd())

	return cmd
}

// Execute runs the root command.
func Execute() error {
	rootCmd.Version = Version
	return rootCmd.Execute()
}

// info writes a human-facing message to stderr unless --quiet is set.
func info(msg string) {
	if !quiet {
		fmt.Fprintln(os.Stderr, msg)
	}
}

// infof writes a formatted human-facing message to stderr unless --quiet is set.
func infof(format string, args ...interface{}) {
	if !quiet {
		fmt.Fprintf(os.Stderr, format+"\n", args...)
	}
}

// infofGrey writes a low-priority progress message to stderr unless --quiet is set.
func infofGrey(format string, args ...interface{}) {
	if !quiet {
		fmt.Fprintf(os.Stderr, grey+format+reset+"\n", args...)
	}
}
