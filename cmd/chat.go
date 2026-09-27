package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/bhargavakumark/llm-cli/pkg/api"
	"github.com/bhargavakumark/llm-cli/pkg/config"
	"github.com/bhargavakumark/llm-cli/pkg/domain"
	"github.com/bhargavakumark/llm-cli/pkg/formatter"
	"github.com/spf13/cobra"
)

var (
	chatSystem        string
	chatFile          string
	chatNoStream      bool
	chatShowReasoning bool
)

func newChatCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "chat [prompt]",
		Short: "Send a prompt and print the reply to stdout",
		Long: `Send a prompt to the selected LLM target and print the reply.

The prompt comes from the arguments, from --file, or from stdin. The reply
is the only thing written to stdout. Streaming is on by default, so the
answer appears as the model produces it; --no-stream waits for the whole
response.

    llm-cli chat --llm ds "explain this error"
    llm-cli chat --llm ds -s "be terse" "$(cat notes.md)"
    cat notes.md | llm-cli chat --llm ds "summarise"`,
		Args: cobra.ArbitraryArgs,
		RunE: runChat,
		// The prompt is free text, so filenames are never suggested.
		ValidArgsFunction: cobra.NoFileCompletions,
	}

	fl := cmd.Flags()
	fl.StringVarP(&chatSystem, "system", "s", "", "System prompt")
	fl.StringVarP(&chatFile, "file", "f", "", "Read the prompt from a file")
	fl.BoolVar(&chatNoStream, "no-stream", false, "Wait for the full response instead of streaming")
	fl.BoolVar(&chatShowReasoning, "show-reasoning", false, "Print reasoning deltas to stderr")

	return cmd
}

func runChat(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	name, target, err := cfg.Resolve(llm)
	if err != nil {
		return err
	}

	prompt, err := readPrompt(cmd, args)
	if err != nil {
		return err
	}

	messages := make([]domain.Message, 0, 2)
	if strings.TrimSpace(chatSystem) != "" {
		messages = append(messages, domain.Message{Role: domain.RoleSystem, Content: chatSystem})
	}
	messages = append(messages, domain.Message{Role: domain.RoleUser, Content: prompt})

	client, err := api.NewClient(target.BaseURL, target.APIKeyValue())
	if err != nil {
		return err
	}
	client.LogRequests = logRequests
	client.Logger = func(dump string) { infofGrey("%s", dump) }

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if chatNoStream {
		result, err := client.Chat(ctx, target.Model, messages)
		if err != nil {
			return cancelled(ctx, err)
		}
		infofGrey("%s -> %s", name, target.Model)
		return formatter.Text(os.Stdout, result)
	}

	printer := formatter.NewStreamPrinter(os.Stdout, reasoningWriter())
	result, err := client.ChatStream(ctx, target.Model, messages, printer.Write)
	if err != nil {
		return cancelled(ctx, hintNoContent(err))
	}
	if err := printer.Finish(); err != nil {
		return err
	}

	infofGrey("%s -> %s (%d chunks)", name, target.Model, result.Chunks)
	return nil
}

// reasoningWriter returns a function that colours reasoning deltas on stderr,
// or nil when the user did not ask for them.
func reasoningWriter() func(string) {
	if !chatShowReasoning {
		return nil
	}
	return func(text string) {
		fmt.Fprint(os.Stderr, grey+text+reset)
	}
}

// hintNoContent adds the likely cause when a stream produced nothing: the
// endpoint may have answered with a plain body instead of server-sent events,
// which --no-stream handles.
func hintNoContent(err error) error {
	if errors.Is(err, api.ErrNoContent) {
		return fmt.Errorf("%w; the endpoint may be ignoring stream=true, retry with --no-stream", err)
	}
	return err
}

// cancelled turns an interrupt into context.Canceled, which main reports as
// exit 130, while leaving every other error untouched.
func cancelled(ctx context.Context, err error) error {
	if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
		return ctx.Err()
	}
	return err
}

// readPrompt resolves the prompt from the file flag, the arguments, or stdin,
// in that order.
func readPrompt(cmd *cobra.Command, args []string) (string, error) {
	if chatFile != "" {
		data, err := os.ReadFile(chatFile)
		if err != nil {
			return "", fmt.Errorf("read prompt file: %w", err)
		}
		if strings.TrimSpace(string(data)) == "" {
			return "", fmt.Errorf("prompt file %s is empty", chatFile)
		}
		return string(data), nil
	}

	if len(args) > 0 {
		prompt := strings.Join(args, " ")
		if strings.TrimSpace(prompt) == "" {
			return "", errors.New("prompt is empty")
		}
		return prompt, nil
	}

	if stdinIsTerminal() {
		return "", errors.New("no prompt: pass it as an argument, via --file, or on stdin")
	}

	data, err := io.ReadAll(cmd.InOrStdin())
	if err != nil {
		return "", fmt.Errorf("read stdin: %w", err)
	}
	if strings.TrimSpace(string(data)) == "" {
		return "", errors.New("prompt from stdin is empty")
	}
	return string(data), nil
}

func stdinIsTerminal() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
