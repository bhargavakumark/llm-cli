package cmd

import "github.com/spf13/cobra"

// codeSystemPrompt tells the model to answer with code and nothing else. The
// CLI also strips a wrapping fence if one appears anyway, but the prompt is
// what stops the model writing commentary in the first place.
const codeSystemPrompt = "Answer with code only. Do not explain, do not add prose " +
	"before or after the code, and do not wrap the code in markdown fences or backticks."

func newCodeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "code [prompt]",
		Short: "Ask for code and print only the code",
		Long: `Ask the selected target for code and print the result.

This is chat with an instruction that the answer be raw code: no explanation,
no prose, no markdown fences. The instruction is added to the system message,
so --system supplies extra context without losing it. A piped input is appended
to the prompt, the same as chat. If the model fences the answer anyway, one
wrapping fence is removed.

    llm-cli code --llm ds "binary search in go"
    llm-cli code --llm ds -s "python 3.12, stdlib only" "parse a csv"
    cat schema.sql | llm-cli code --llm ds "write a query for monthly totals"`,
		Args:              cobra.ArbitraryArgs,
		RunE:              runCode,
		ValidArgsFunction: cobra.NoFileCompletions,
	}

	fl := cmd.Flags()
	fl.StringVarP(&chatSystem, "system", "s", "", "Extra system context, added before the raw-code instruction")
	fl.StringVarP(&chatFile, "file", "f", "", "Read the prompt from a file")
	fl.BoolVar(&chatNoStream, "no-stream", false, "Wait for the full response instead of streaming")
	fl.BoolVar(&chatShowReasoning, "show-reasoning", false, "Print reasoning deltas to stderr")

	return cmd
}

func runCode(cmd *cobra.Command, args []string) error {
	return runChatMode(cmd, args, chatMode{
		systemPrompt: codeSystemPrompt,
		appendSystem: true,
		rawCode:      true,
	})
}
