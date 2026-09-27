package cmd

import (
	"github.com/bhargavakumark/llm-cli/pkg/config"
	"github.com/spf13/cobra"
)

// completeLLM suggests target names and aliases for --llm.
//
// A shell completion callback has no way to show a message, so a config that
// is missing or invalid yields the error directive rather than an explanation:
// the shell stops suggesting and the user finds out why the moment they run a
// command for real.
func completeLLM(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	cfg, err := config.Load()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError | cobra.ShellCompDirectiveNoFileComp
	}

	var completions []string
	for _, name := range cfg.Names() {
		target := cfg.LLMs[name]
		mark := ""
		if name == cfg.DefaultLLM {
			mark = "default, "
		}

		completions = append(completions, name+"\t"+mark+target.Model+" @ "+target.BaseURL)
		for _, alias := range target.Aliases {
			completions = append(completions, alias+"\talias for "+name+", "+mark+target.Model)
		}
	}

	return completions, cobra.ShellCompDirectiveNoFileComp
}
