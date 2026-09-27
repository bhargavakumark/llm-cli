package cmd

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bhargavakumark/llm-cli/pkg/api"
	"github.com/bhargavakumark/llm-cli/pkg/config"
	"github.com/spf13/cobra"
)

// useTempHome points the config path at a temporary directory so tests never
// touch the real one.
func useTempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

// runRoot executes a fresh root command quietly.
func runRoot(t *testing.T, stdin string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	return runRootMode(t, true, stdin, args...)
}

// runRootMode executes a fresh root command with the given arguments. Flag
// values are set after newRootCmd, since building the command rebinds every
// flag variable to its default.
func runRootMode(t *testing.T, quietMode bool, stdin string, args ...string) (stdout, stderr string, err error) {
	t.Helper()

	original := stdinIsTerminal
	t.Cleanup(func() { stdinIsTerminal = original })

	cmd := newRootCmd()
	quiet = quietMode

	outBuf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	cmd.SetOut(outBuf)
	cmd.SetErr(errBuf)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)

	err = cmd.Execute()
	return outBuf.String(), errBuf.String(), err
}

// runRootCaptured runs a command with os.Stdout and os.Stderr replaced, since
// the grey progress lines are written to the process streams rather than to the
// command's writers.
func runRootCaptured(t *testing.T, quietMode bool, args ...string) (string, string, error) {
	t.Helper()

	var runErr error
	stdout, stderr := captureStdio(t, func() {
		cmd := newRootCmd()
		// Point the command's writers at the captured streams, so a command
		// that uses cmd.OutOrStdout and one that writes to os.Stdout both land
		// in the same place.
		cmd.SetOut(os.Stdout)
		cmd.SetErr(os.Stderr)
		cmd.SetIn(strings.NewReader(""))
		cmd.SetArgs(args)

		quiet = quietMode
		runErr = cmd.Execute()
	})
	return stdout, stderr, runErr
}

// captureStdio runs fn with os.Stdout and os.Stderr replaced, which is what
// chat writes to.
func captureStdio(t *testing.T, fn func()) (string, string) {
	t.Helper()

	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}

	savedOut, savedErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW
	defer func() { os.Stdout, os.Stderr = savedOut, savedErr }()

	done := make(chan [2]string, 1)
	go func() {
		var out, errBuf bytes.Buffer
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); io.Copy(&out, outR) }()
		go func() { defer wg.Done(); io.Copy(&errBuf, errR) }()
		wg.Wait()
		done <- [2]string{out.String(), errBuf.String()}
	}()

	fn()
	outW.Close()
	errW.Close()

	select {
	case captured := <-done:
		return captured[0], captured[1]
	case <-time.After(10 * time.Second):
		t.Fatal("timed out capturing output")
		return "", ""
	}
}

func TestReadPromptFromArguments(t *testing.T) {
	useTempHome(t)

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "one word", args: []string{"hello"}, want: "hello"},
		{name: "several words are joined", args: []string{"what", "is", "this"}, want: "what is this"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := readPrompt(strings.NewReader(""), test.args, "")
			if err != nil {
				t.Fatalf("readPrompt() error = %v", err)
			}
			if got != test.want {
				t.Errorf("readPrompt() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestReadPromptRejectsABlankArgument(t *testing.T) {
	useTempHome(t)

	if _, err := readPrompt(strings.NewReader(""), []string{"   "}, ""); err == nil {
		t.Fatal("readPrompt() error = nil, want a failure for a blank prompt")
	}
}

func TestReadPromptFromFile(t *testing.T) {
	home := useTempHome(t)

	path := filepath.Join(home, "prompt.md")
	if err := os.WriteFile(path, []byte("from the file\n"), 0600); err != nil {
		t.Fatalf("write prompt file: %v", err)
	}
	got, err := readPrompt(strings.NewReader(""), nil, path)
	if err != nil {
		t.Fatalf("readPrompt() error = %v", err)
	}
	if got != "from the file\n" {
		t.Errorf("readPrompt() = %q, want the file contents", got)
	}
}

func TestReadPromptFileWinsOverArguments(t *testing.T) {
	home := useTempHome(t)

	path := filepath.Join(home, "prompt.md")
	if err := os.WriteFile(path, []byte("from the file"), 0600); err != nil {
		t.Fatalf("write prompt file: %v", err)
	}
	got, err := readPrompt(strings.NewReader(""), []string{"ignored"}, path)
	if err != nil {
		t.Fatalf("readPrompt() error = %v", err)
	}
	if got != "from the file" {
		t.Errorf("readPrompt() = %q, want the file contents", got)
	}
}

func TestReadPromptMissingOrEmptyFile(t *testing.T) {
	home := useTempHome(t)

	t.Run("missing", func(t *testing.T) {
		if _, err := readPrompt(strings.NewReader(""), nil, filepath.Join(home, "absent.md")); err == nil ||
			!strings.Contains(err.Error(), "read prompt file") {
			t.Fatalf("readPrompt() error = %v, want a read failure", err)
		}
	})

	t.Run("empty", func(t *testing.T) {
		path := filepath.Join(home, "empty.md")
		if err := os.WriteFile(path, []byte("\n  \n"), 0600); err != nil {
			t.Fatalf("write prompt file: %v", err)
		}
		if _, err := readPrompt(strings.NewReader(""), nil, path); err == nil ||
			!strings.Contains(err.Error(), "is empty") {
			t.Fatalf("readPrompt() error = %v, want an empty-file failure", err)
		}
	})
}

func TestReadPromptFromStdin(t *testing.T) {
	useTempHome(t)
	stdinIsTerminal = func() bool { return false }

	got, err := readPrompt(strings.NewReader("from a pipe"), nil, "")
	if err != nil {
		t.Fatalf("readPrompt() error = %v", err)
	}
	if got != "from a pipe" {
		t.Errorf("readPrompt() = %q, want %q", got, "from a pipe")
	}
}

func TestReadPromptEmptyStdin(t *testing.T) {
	useTempHome(t)
	stdinIsTerminal = func() bool { return false }

	if _, err := readPrompt(strings.NewReader("   \n"), nil, ""); err == nil ||
		!strings.Contains(err.Error(), "stdin is empty") {
		t.Fatalf("readPrompt() error = %v, want an empty-stdin failure", err)
	}
}

func TestReadPromptWithNothingToRead(t *testing.T) {
	useTempHome(t)
	stdinIsTerminal = func() bool { return true }

	_, err := readPrompt(strings.NewReader(""), nil, "")
	if err == nil || !strings.Contains(err.Error(), "--file") {
		t.Fatalf("readPrompt() error = %v, want a message describing the ways to pass a prompt", err)
	}
}

func TestAsk(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		current string
		want    string
	}{
		{name: "keeps the current value on a blank line", input: "\n", current: "old", want: "old"},
		{name: "takes a new value", input: "new\n", current: "old", want: "new"},
		{name: "trims whitespace", input: "  new  \n", current: "", want: "new"},
		{name: "end of input keeps the current value", input: "", current: "old", want: "old"},
		{name: "first of several lines", input: "first\nsecond\n", current: "", want: "first"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			scanner := bufio.NewScanner(strings.NewReader(test.input))

			if got := ask(io.Discard, scanner, "Label", test.current); got != test.want {
				t.Errorf("ask() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestAskBool(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		current bool
		want    bool
		wantErr string
	}{
		{name: "true", input: "true\n", want: true},
		{name: "false", input: "false\n", want: false},
		{name: "blank keeps the current value", input: "\n", current: true, want: true},
		{name: "end of input keeps the current value", input: "", current: true, want: true},
		{name: "rejects a non boolean", input: "maybe\n", current: true, want: true, wantErr: "is not true or false"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			scanner := bufio.NewScanner(strings.NewReader(test.input))

			got, err := askBool(io.Discard, scanner, "Include usage", test.current)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("askBool() error = %v, want it to contain %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("askBool() error = %v", err)
			}
			if got != test.want {
				t.Errorf("askBool() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestElapsed(t *testing.T) {
	start := time.Now().Add(-1500 * time.Millisecond)

	got := elapsed(start)
	if got == "" {
		t.Fatal("elapsed() returned an empty string")
	}
	duration, err := time.ParseDuration(got)
	if err != nil {
		t.Fatalf("elapsed() = %q, which is not a duration: %v", got, err)
	}
	if duration < time.Second {
		t.Errorf("elapsed() = %v, want at least a second for a 1.5s start", duration)
	}
}

func TestCancelled(t *testing.T) {
	t.Run("a cancellation becomes context.Canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		if err := cancelled(ctx, fmt.Errorf("stream aborted: %w", ctx.Err())); !errors.Is(err, context.Canceled) {
			t.Errorf("cancelled() = %v, want context.Canceled", err)
		}
	})

	t.Run("an unrelated error passes through", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		sentinel := errors.New("HTTP 500")

		if err := cancelled(ctx, sentinel); !errors.Is(err, sentinel) {
			t.Errorf("cancelled() = %v, want the original error", err)
		}
	})

	t.Run("a live context passes the error through", func(t *testing.T) {
		sentinel := errors.New("boom")

		if err := cancelled(context.Background(), sentinel); !errors.Is(err, sentinel) {
			t.Errorf("cancelled() = %v, want the original error", err)
		}
	})
}

func TestHintNoContent(t *testing.T) {
	base := fmt.Errorf("http://host %w for model %q", api.ErrNoContent, "m")

	got := hintNoContent(base)
	if !strings.Contains(got.Error(), "--no-stream") {
		t.Errorf("hintNoContent() = %q, want a --no-stream hint", got)
	}
	if !errors.Is(got, api.ErrNoContent) {
		t.Errorf("hintNoContent() = %q, want the wrapped error preserved", got)
	}

	sentinel := errors.New("HTTP 401")
	if other := hintNoContent(sentinel); other != sentinel {
		t.Errorf("hintNoContent() = %v, want unrelated errors untouched", other)
	}
}

func TestCompleteLLM(t *testing.T) {
	useTempHome(t)

	saveConfig(t, &config.Config{
		DefaultLLM: "deepseek",
		LLMs: map[string]config.Target{
			"deepseek": {Aliases: []string{"ds"}, BaseURL: "https://api.deepseek.com", Model: "deepseek-flash"},
			"gpt6":     {BaseURL: "http://gpt6", Model: "gpt-6-luna"},
		},
	})

	got, directive := completeLLM(nil, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("directive = %v, want ShellCompDirectiveNoFileComp", directive)
	}

	joined := strings.Join(got, "\n")
	for _, want := range []string{"deepseek\t", "ds\t", "gpt6\t", "default, deepseek-flash"} {
		if !strings.Contains(joined, want) {
			t.Errorf("completions %q should contain %q", joined, want)
		}
	}
}

func TestCompleteLLMWithoutConfig(t *testing.T) {
	useTempHome(t)

	got, directive := completeLLM(nil, nil, "")
	if len(got) != 0 {
		t.Errorf("completions = %v, want none", got)
	}
	if directive&cobra.ShellCompDirectiveError == 0 {
		t.Errorf("directive = %v, want the error directive", directive)
	}
}

func TestCompleteLLMWithAnInvalidConfig(t *testing.T) {
	useTempHome(t)

	// Two targets sharing an alias: validation fails, so completion refuses
	// rather than suggesting an ambiguous name.
	saveConfig(t, &config.Config{LLMs: map[string]config.Target{
		"a": {Aliases: []string{"shared"}},
		"b": {Aliases: []string{"shared"}},
	}})

	got, directive := completeLLM(nil, nil, "")
	if len(got) != 0 || directive&cobra.ShellCompDirectiveError == 0 {
		t.Errorf("completions = %v, directive = %v, want none and the error directive", got, directive)
	}
}

func saveConfig(t *testing.T, cfg *config.Config) {
	t.Helper()
	if err := config.Save(cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
}
