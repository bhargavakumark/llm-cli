package formatter

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/bhargavakumark/llm-cli/pkg/domain"
)

// codeFence is the markdown sequence a model wraps code in when it ignores an
// instruction to answer with raw code.
const codeFence = "```"

// codeFenceWindow caps how much trailing text the printer holds back while it
// waits to see whether the answer ends with a closing fence. A longer run of
// whitespace is treated as answer text rather than fence padding.
const codeFenceWindow = 128

// CodePrinter writes a code answer to stdout, streaming it as it arrives but
// holding back the text that could still turn out to be a wrapping markdown
// fence. A model that ignores the raw-code instruction and fences its answer
// anyway therefore still produces raw code.
//
// Only the leading fence decision and the trailing whitespace-or-backticks run
// are held back, so an unfenced answer streams with no measurable delay.
type CodePrinter struct {
	w           *bufio.Writer
	onReasoning func(string)

	// pending is text received but not yet written.
	pending string
	// decided is true once we know whether the answer opens with a fence.
	decided bool
	// fenceSeen records that a leading fence was removed, so that only a
	// matching closing fence is stripped.
	fenceSeen bool
}

// NewCodePrinter wraps out. onReasoning receives reasoning deltas, or may be
// nil to discard them.
func NewCodePrinter(out io.Writer, onReasoning func(string)) *CodePrinter {
	return &CodePrinter{
		w:           bufio.NewWriter(out),
		onReasoning: onReasoning,
	}
}

// Write handles one delta. A delta carrying only reasoning produces no stdout
// output.
func (p *CodePrinter) Write(chunk domain.Chunk) error {
	if chunk.Reasoning != "" && p.onReasoning != nil {
		p.onReasoning(chunk.Reasoning)
	}
	if chunk.Content == "" {
		return nil
	}

	p.pending += chunk.Content
	if !p.decided {
		p.resolveLeading()
	}
	if !p.decided {
		return nil
	}
	return p.flush(false)
}

// Finish strips a closing fence if the answer was fenced, writes whatever is
// still held back, and terminates the answer with a newline.
func (p *CodePrinter) Finish() error {
	if err := p.flush(true); err != nil {
		return err
	}
	if _, err := p.w.WriteString("\n"); err != nil {
		return fmt.Errorf("write response: %w", err)
	}
	return p.w.Flush()
}

// resolveLeading decides whether the answer opens with a fence. It waits for
// enough text to tell, dropping the fence line when one is present.
func (p *CodePrinter) resolveLeading() {
	trimmed := strings.TrimLeft(p.pending, " \t\r\n")
	if trimmed == "" {
		// Nothing but whitespace so far; a fence may still follow it.
		return
	}
	if strings.HasPrefix(trimmed, codeFence) {
		newline := strings.Index(trimmed, "\n")
		if newline < 0 {
			// The fence line has not ended yet, so hold until it does.
			return
		}
		p.pending = trimmed[newline+1:]
		p.fenceSeen = true
	} else if strings.HasPrefix(codeFence, trimmed) {
		// A short run of backticks ("`" or "``") that may still grow into a
		// fence.
		return
	}
	p.decided = true
}

// flush writes pending text. A non-final flush keeps the trailing run of
// whitespace and backticks, because that run may yet become a closing fence.
func (p *CodePrinter) flush(final bool) error {
	text := p.pending
	if final && p.fenceSeen {
		text = stripTrailingFence(text)
	}

	var emit string
	if final {
		emit = text
		p.pending = ""
	} else {
		hold := trailingFenceHold(text)
		emit = text[:len(text)-hold]
		p.pending = text[len(text)-hold:]
	}

	if emit == "" {
		return nil
	}
	if _, err := p.w.WriteString(emit); err != nil {
		return fmt.Errorf("write response: %w", err)
	}
	return p.w.Flush()
}

// trailingFenceHold reports how many trailing bytes could be part of a closing
// fence: a run of whitespace and backticks. The run is released once it grows
// past codeFenceWindow, which means it is answer text rather than padding.
func trailingFenceHold(text string) int {
	i := len(text)
	for i > 0 && isFencePadding(text[i-1]) {
		i--
	}
	if len(text)-i > codeFenceWindow {
		return 0
	}
	return len(text) - i
}

// isFencePadding reports whether c is a byte a closing fence may be wrapped in.
func isFencePadding(c byte) bool {
	switch c {
	case '`', ' ', '\t', '\r', '\n':
		return true
	default:
		return false
	}
}

// stripTrailingFence removes one closing fence and the whitespace around it
// from the end of an answer.
func stripTrailingFence(text string) string {
	trimmed := strings.TrimRight(text, " \t\r\n")
	if !strings.HasSuffix(trimmed, codeFence) {
		return text
	}
	return strings.TrimRight(strings.TrimSuffix(trimmed, codeFence), " \t\r\n")
}
