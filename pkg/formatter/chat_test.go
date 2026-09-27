package formatter

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/bhargavakumark/llm-cli/pkg/domain"
)

// spyWriter records every write so a test can tell whether output was flushed
// before the stream ended.
type spyWriter struct {
	writes []string
	err    error
}

func (s *spyWriter) Write(p []byte) (int, error) {
	if s.err != nil {
		return 0, s.err
	}
	s.writes = append(s.writes, string(p))
	return len(p), nil
}

func (s *spyWriter) all() string {
	return strings.Join(s.writes, "")
}

func TestStreamPrinterWritesContentImmediately(t *testing.T) {
	spy := &spyWriter{}
	printer := NewStreamPrinter(spy, nil)

	if err := printer.Write(domain.Chunk{Content: "Hello, "}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	// Flushed per delta: the first chunk is already on the writer even though
	// the stream has not finished.
	if got := spy.all(); got != "Hello, " {
		t.Fatalf("after one chunk the writer holds %q, want %q", got, "Hello, ")
	}

	if err := printer.Write(domain.Chunk{Content: "world."}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if got := spy.all(); got != "Hello, world." {
		t.Fatalf("writer holds %q, want %q", got, "Hello, world.")
	}
}

func TestStreamPrinterFinishEndsTheLine(t *testing.T) {
	spy := &spyWriter{}
	printer := NewStreamPrinter(spy, nil)

	if err := printer.Write(domain.Chunk{Content: "answer"}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := printer.Finish(); err != nil {
		t.Fatalf("Finish() error = %v", err)
	}

	if got := spy.all(); got != "answer\n" {
		t.Errorf("output = %q, want %q", got, "answer\n")
	}
}

func TestStreamPrinterIgnoresEmptyContent(t *testing.T) {
	spy := &spyWriter{}
	printer := NewStreamPrinter(spy, nil)

	// Finish-reason-only and usage-only chunks carry no text.
	if err := printer.Write(domain.Chunk{FinishReason: "stop"}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if len(spy.writes) != 0 {
		t.Errorf("writer received %v, want nothing", spy.writes)
	}
}

func TestStreamPrinterRoutesReasoningToTheCallback(t *testing.T) {
	spy := &spyWriter{}
	var reasoning []string
	printer := NewStreamPrinter(spy, func(text string) { reasoning = append(reasoning, text) })

	if err := printer.Write(domain.Chunk{Reasoning: "thinking", Content: "answer"}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	if strings.Join(reasoning, "") != "thinking" {
		t.Errorf("reasoning = %v, want [thinking]", reasoning)
	}
	if got := spy.all(); got != "answer" {
		t.Errorf("stdout holds %q, want only the content", got)
	}
}

func TestStreamPrinterWithNilReasoningCallback(t *testing.T) {
	spy := &spyWriter{}
	printer := NewStreamPrinter(spy, nil)

	// Reasoning must not reach stdout when nobody asked for it.
	if err := printer.Write(domain.Chunk{Reasoning: "hidden", Content: "answer"}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if got := spy.all(); got != "answer" {
		t.Errorf("stdout holds %q, want only the content", got)
	}
}

func TestStreamPrinterReportsWriteFailure(t *testing.T) {
	sentinel := errors.New("broken pipe")
	printer := NewStreamPrinter(&spyWriter{err: sentinel}, nil)

	err := printer.Write(domain.Chunk{Content: "answer"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Write() error = %v, want the writer error", err)
	}
}

func TestText(t *testing.T) {
	var out bytes.Buffer
	result := domain.Result{Message: domain.Message{Role: "assistant", Content: "Hello, world."}}

	if err := Text(&out, result); err != nil {
		t.Fatalf("Text() error = %v", err)
	}
	if got := out.String(); got != "Hello, world.\n" {
		t.Errorf("Text() wrote %q, want %q", got, "Hello, world.\n")
	}
}

func TestTextReportsWriteFailure(t *testing.T) {
	sentinel := errors.New("closed")

	err := Text(&spyWriter{err: sentinel}, domain.Result{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Text() error = %v, want the writer error", err)
	}
}

func TestUsageSummary(t *testing.T) {
	tests := []struct {
		name  string
		usage domain.Usage
		want  string
	}{
		{
			name:  "nothing reported",
			usage: domain.Usage{},
			want:  "",
		},
		{
			name:  "prompt and completion",
			usage: domain.Usage{PromptTokens: 8, CompletionTokens: 5, TotalTokens: 13},
			want:  "tokens: 8 prompt + 5 completion = 13 total",
		},
		{
			name:  "with cached and reasoning counts",
			usage: domain.Usage{PromptTokens: 11, CompletionTokens: 4, TotalTokens: 15, CachedTokens: 3, ReasoningTokens: 2},
			want:  "tokens: 11 prompt (3 cached) + 4 completion (2 reasoning) = 15 total",
		},
		{
			name:  "missing total is computed",
			usage: domain.Usage{PromptTokens: 3, CompletionTokens: 4},
			want:  "tokens: 3 prompt + 4 completion = 7 total",
		},
		{
			name:  "partial report is still reported",
			usage: domain.Usage{CompletionTokens: 4, TotalTokens: 4},
			want:  "tokens: 0 prompt + 4 completion = 4 total",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := UsageSummary(test.usage); got != test.want {
				t.Errorf("UsageSummary() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestUsageReported(t *testing.T) {
	tests := []struct {
		name  string
		usage domain.Usage
		want  bool
	}{
		{name: "empty", usage: domain.Usage{}, want: false},
		{name: "prompt only", usage: domain.Usage{PromptTokens: 1}, want: true},
		{name: "completion only", usage: domain.Usage{CompletionTokens: 1}, want: true},
		{name: "total only", usage: domain.Usage{TotalTokens: 1}, want: true},
		{name: "details without counts", usage: domain.Usage{CachedTokens: 5}, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.usage.Reported(); got != test.want {
				t.Errorf("Reported() = %v, want %v", got, test.want)
			}
		})
	}
}
