// Package formatter writes results. For chat, stdout carries the answer and
// nothing else, so that a redirect or a pipe captures exactly the model's
// text.
package formatter

import (
	"bufio"
	"fmt"
	"io"

	"github.com/bhargavakumark/llm-cli/pkg/domain"
)

// StreamPrinter writes deltas to stdout as they arrive and flushes after each
// one, because a buffered writer that never flushes shows nothing until the
// stream ends.
type StreamPrinter struct {
	w           *bufio.Writer
	onReasoning func(string)
}

// NewStreamPrinter wraps out. onReasoning receives reasoning deltas, or may
// be nil to discard them.
func NewStreamPrinter(out io.Writer, onReasoning func(string)) *StreamPrinter {
	return &StreamPrinter{
		w:           bufio.NewWriter(out),
		onReasoning: onReasoning,
	}
}

// Write handles one delta. A delta carrying only reasoning produces no stdout
// output.
func (p *StreamPrinter) Write(chunk domain.Chunk) error {
	if chunk.Reasoning != "" && p.onReasoning != nil {
		p.onReasoning(chunk.Reasoning)
	}
	if chunk.Content == "" {
		return nil
	}

	if _, err := p.w.WriteString(chunk.Content); err != nil {
		return fmt.Errorf("write response: %w", err)
	}
	return p.w.Flush()
}

// Finish terminates the answer with a newline.
func (p *StreamPrinter) Finish() error {
	if _, err := p.w.WriteString("\n"); err != nil {
		return fmt.Errorf("write response: %w", err)
	}
	return p.w.Flush()
}

// Text writes a complete, non-streamed answer.
func Text(out io.Writer, result domain.Result) error {
	if _, err := fmt.Fprintln(out, result.Message.Content); err != nil {
		return fmt.Errorf("write response: %w", err)
	}
	return nil
}
