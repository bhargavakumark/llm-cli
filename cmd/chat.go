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
	"time"

	"github.com/bhargavakumark/llm-cli/pkg/api"
	"github.com/bhargavakumark/llm-cli/pkg/config"
	"github.com/bhargavakumark/llm-cli/pkg/domain"
	"github.com/bhargavakumark/llm-cli/pkg/formatter"
	"github.com/spf13/cobra"
)

// chatSystemPrompt is always added to the chat system message. It asks for a
// crisp answer without emphasis markup, which models otherwise overuse.
const chatSystemPrompt = "Be crisp and concise. No filler, no restating the " +
	"question. Do not overuse bold or other emphasis markup."

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

The prompt comes from the arguments, from --file, or from stdin. When both a
prompt and a pipe are present, the piped contents are appended to the prompt.
The reply is the only thing written to stdout. Streaming is on by default, so
the answer appears as the model produces it; --no-stream waits for the whole
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

// chatMode adjusts how a chat-shaped command shapes its system message and
// renders the answer. The zero value is the plain chat command: the user's
// system prompt is sent untouched and the reply is written verbatim.
type chatMode struct {
	// systemPrompt is added to the system message by commands that need one.
	systemPrompt string
	// appendSystem adds systemPrompt after a user-supplied --system instead of
	// replacing it, so extra context does not lose the command's instruction.
	appendSystem bool
	// rawCode removes one wrapping markdown fence from the answer.
	rawCode bool
}

func runChat(cmd *cobra.Command, args []string) error {
	return runChatMode(cmd, args, chatMode{
		systemPrompt: chatSystemPrompt,
		appendSystem: true,
	})
}

func runChatMode(cmd *cobra.Command, args []string, mode chatMode) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	name, target, err := cfg.Resolve(llm)
	if err != nil {
		return err
	}

	prompt, err := readPrompt(cmd.InOrStdin(), args, chatFile)
	if err != nil {
		return err
	}

	system := chatSystem
	if mode.systemPrompt != "" {
		switch {
		case strings.TrimSpace(system) == "":
			system = mode.systemPrompt
		case mode.appendSystem:
			system = strings.TrimRight(system, "\n") + "\n\n" + mode.systemPrompt
		}
	}

	messages := make([]domain.Message, 0, 2)
	if strings.TrimSpace(system) != "" {
		messages = append(messages, domain.Message{Role: domain.RoleSystem, Content: system})
	}
	messages = append(messages, domain.Message{Role: domain.RoleUser, Content: prompt})

	client, err := api.NewClient(target.BaseURL, target.APIKeyValue(), target.BindInterface)
	if err != nil {
		return err
	}
	client.IncludeUsage = target.IncludeUsage
	client.LogRequests = logRequests
	client.Logger = func(dump string) { infofGrey("%s", dump) }

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if chatNoStream {
		start := time.Now()
		result, err := client.Chat(ctx, target.Model, messages)
		if err != nil {
			return cancelled(ctx, err)
		}
		if err := writeResult(os.Stdout, mode, result); err != nil {
			return err
		}
		infofGrey("%s -> %s (%s)", name, target.Model, elapsed(start))
		reportUsage(result.Usage)
		return nil
	}

	printer := newStreamWriter(mode, reasoningWriter())
	start := time.Now()
	result, err := client.ChatStream(ctx, target.Model, messages, printer.Write)
	if err != nil {
		return cancelled(ctx, hintNoContent(err))
	}
	if err := printer.Finish(); err != nil {
		return err
	}

	infofGrey("%s -> %s (%d chunks, %s)", name, target.Model, result.Chunks, elapsed(start))
	reportUsage(result.Usage)
	return nil
}

// answerWriter is what a command streams deltas through. Both the plain and
// the raw-code printer satisfy it.
type answerWriter interface {
	Write(domain.Chunk) error
	Finish() error
}

// newStreamWriter picks the printer for a mode. Raw code uses a printer that
// removes a wrapping fence; everything else writes deltas verbatim.
func newStreamWriter(mode chatMode, onReasoning func(string)) answerWriter {
	if mode.rawCode {
		return formatter.NewCodePrinter(os.Stdout, onReasoning)
	}
	return formatter.NewStreamPrinter(os.Stdout, onReasoning)
}

// writeResult writes a complete, non-streamed answer. Plain chat writes it
// verbatim; raw code runs it through the fence-stripping printer so that a
// non-streamed reply is cleaned the same way a streamed one is.
func writeResult(out io.Writer, mode chatMode, result domain.Result) error {
	if !mode.rawCode {
		return formatter.Text(out, result)
	}

	printer := formatter.NewCodePrinter(out, nil)
	if result.Message.Content != "" {
		if err := printer.Write(domain.Chunk{Content: result.Message.Content}); err != nil {
			return err
		}
	}
	return printer.Finish()
}

// reportUsage writes token accounting on stderr in grey, and writes nothing
// when the endpoint reported none.
func reportUsage(usage domain.Usage) {
	if summary := formatter.UsageSummary(usage); summary != "" {
		infofGrey("%s", summary)
	}
}

// elapsed renders how long an API call took, rounded to something a human
// reads quickly.
func elapsed(start time.Time) string {
	return time.Since(start).Round(time.Millisecond).String()
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

// readPrompt resolves the prompt from the file, the arguments, or stdin. A
// prompt given as an argument or a file is the instruction; when stdin is a
// pipe, its contents are appended to that instruction rather than discarded.
func readPrompt(in io.Reader, args []string, file string) (string, error) {
	var prompt string
	switch {
	case file != "":
		data, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("read prompt file: %w", err)
		}
		if strings.TrimSpace(string(data)) == "" {
			return "", fmt.Errorf("prompt file %s is empty", file)
		}
		prompt = string(data)
	case len(args) > 0:
		prompt = strings.Join(args, " ")
		if strings.TrimSpace(prompt) == "" {
			return "", errors.New("prompt is empty")
		}
	}

	if stdinIsTerminal() {
		if prompt == "" {
			return "", errors.New("no prompt: pass it as an argument, via --file, or on stdin")
		}
		return prompt, nil
	}

	data, err := io.ReadAll(in)
	if err != nil {
		return "", fmt.Errorf("read stdin: %w", err)
	}
	piped := string(data)

	if prompt == "" {
		if strings.TrimSpace(piped) == "" {
			return "", errors.New("prompt from stdin is empty")
		}
		return piped, nil
	}
	if strings.TrimSpace(piped) == "" {
		return prompt, nil
	}

	// An instruction followed by the piped data, so the model reads what to do
	// before it reads what to do it to.
	return strings.TrimRight(prompt, "\n") + "\n" + piped, nil
}

// stdinIsTerminal is a variable so tests can decide whether stdin looks like
// a terminal, which is what distinguishes "no prompt given" from "a pipe".
var stdinIsTerminal = func() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
