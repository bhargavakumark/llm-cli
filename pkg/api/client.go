// Package api is the low-level client for OpenAI-compatible endpoints. It
// wraps github.com/sashabaranov/go-openai, which is a transport and typed
// decoder only: it does not add a system prompt, tools or any other context
// to a request, and does not rewrite a response.
//
// The library sends exactly the request body built here and returns exactly
// the content the server sent, so nothing in this package decides what the
// model sees.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bhargavakumark/llm-cli/pkg/domain"
	openai "github.com/sashabaranov/go-openai"
)

const (
	chatSuffix = "/chat/completions"

	// DialTimeout bounds connecting and the TLS handshake, so an unreachable
	// host fails in seconds instead of hanging.
	DialTimeout = 10 * time.Second

	// RequestTimeout bounds a whole request including reading the stream. It is
	// generous because a long generation is legitimate, but it still ends a
	// connection that goes quiet forever.
	RequestTimeout = 300 * time.Second
)

// ErrNoContent reports a stream that ended without any choices. An endpoint
// that ignores stream=true and answers with a plain JSON body looks the same
// from here, so callers that know about the flag can add that hint.
var ErrNoContent = errors.New("returned no content")

// Client talks to one endpoint.
type Client struct {
	c       *openai.Client
	BaseURL string

	// IncludeUsage asks the endpoint for token accounting during a streamed
	// reply, via the stream_options field. Off means the field is not sent at
	// all, so endpoints that reject it keep working.
	IncludeUsage bool

	// LogRequests dumps each outgoing request through Logger.
	LogRequests bool
	// Logger receives the dump. Nil means LogRequests does nothing.
	Logger func(string)
}

// NewClient builds a client for baseURL. The base URL is stored exactly as
// given: no /v1 is appended and no path is rewritten, because go-openai
// joins it with the endpoint suffix verbatim.
func NewClient(baseURL, apiKey string) (*Client, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, errors.New("api client: base_url is empty")
	}

	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base_url %q: %w", baseURL, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid base_url %q: needs a scheme and host", baseURL)
	}

	// DefaultConfig is the only way in: the auth token field is unexported.
	cfg := openai.DefaultConfig(apiKey)
	cfg.BaseURL = baseURL
	cfg.HTTPClient = newHTTPClient()

	return &Client{c: openai.NewClientWithConfig(cfg), BaseURL: baseURL}, nil
}

// newHTTPClient fails fast on an unreachable host and gives up on a request
// that never finishes, rather than hanging until the process is killed.
func newHTTPClient() *http.Client {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   DialTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout: DialTimeout,
		ForceAttemptHTTP2:   true,
	}
	return &http.Client{Transport: transport, Timeout: RequestTimeout}
}

// ChatStream runs a streaming completion, calling onChunk for every delta as
// it arrives. An empty response with no choices at all is an error rather
// than a silent success with nothing on stdout.
func (cl *Client) ChatStream(
	ctx context.Context,
	model string,
	messages []domain.Message,
	onChunk func(domain.Chunk) error,
) (domain.Result, error) {
	req := openai.ChatCompletionRequest{
		Model:    model,
		Messages: toOpenAI(messages),
	}
	if cl.IncludeUsage {
		req.StreamOptions = &openai.StreamOptions{IncludeUsage: true}
	}
	cl.logRequest(req)

	stream, err := cl.c.CreateChatCompletionStream(ctx, req)
	if err != nil {
		return domain.Result{}, wrapAPIError(err, cl.BaseURL)
	}
	defer func() {
		// Closing twice is harmless; the error is already reported above.
		_ = stream.Close()
	}()

	result := domain.Result{Model: model}
	for {
		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return result, fmt.Errorf("stream aborted: %w", wrapAPIError(err, cl.BaseURL))
		}

		for _, choice := range response.Choices {
			result.Chunks++
			delta := domain.Chunk{
				Content:      choice.Delta.Content,
				Reasoning:    choice.Delta.ReasoningContent,
				FinishReason: string(choice.FinishReason),
			}
			result.Message.Content += delta.Content
			if err := onChunk(delta); err != nil {
				return result, err
			}
		}

		// Every chunk before the last carries a null usage field, so only a
		// real report replaces what is already there.
		if response.Usage != nil {
			result.Usage = toUsage(*response.Usage)
		}
	}

	if result.Chunks == 0 {
		return result, fmt.Errorf("%s %w for model %q", cl.BaseURL, ErrNoContent, model)
	}
	return result, nil
}

// Chat runs a single non-streaming completion.
func (cl *Client) Chat(
	ctx context.Context,
	model string,
	messages []domain.Message,
) (domain.Result, error) {
	req := openai.ChatCompletionRequest{
		Model:    model,
		Messages: toOpenAI(messages),
	}
	if cl.IncludeUsage {
		req.StreamOptions = &openai.StreamOptions{IncludeUsage: true}
	}
	cl.logRequest(req)

	response, err := cl.c.CreateChatCompletion(ctx, req)
	if err != nil {
		return domain.Result{}, wrapAPIError(err, cl.BaseURL)
	}
	if len(response.Choices) == 0 {
		return domain.Result{}, fmt.Errorf("%s returned no choices for model %q", cl.BaseURL, model)
	}

	answer := response.Choices[0].Message
	return domain.Result{
		Model: response.Model,
		Message: domain.Message{
			Role:    answer.Role,
			Content: answer.Content,
		},
		Usage: toUsage(response.Usage),
	}, nil
}

// ListModels returns the model ids the endpoint advertises.
func (cl *Client) ListModels(ctx context.Context) ([]string, error) {
	cl.logf("GET %s/models", cl.BaseURL)

	list, err := cl.c.ListModels(ctx)
	if err != nil {
		return nil, wrapAPIError(err, cl.BaseURL)
	}

	ids := make([]string, 0, len(list.Models))
	for _, model := range list.Models {
		ids = append(ids, model.ID)
	}
	return ids, nil
}

// logRequest dumps the exact body that goes on the wire. It is the same value
// the library marshals, so what is printed is what the endpoint receives.
func (cl *Client) logRequest(req openai.ChatCompletionRequest) {
	if !cl.LogRequests || cl.Logger == nil {
		return
	}

	body, err := json.Marshal(req)
	if err != nil {
		cl.logf("POST %s%s (request could not be encoded for logging: %v)", cl.BaseURL, chatSuffix, err)
		return
	}
	cl.logf("POST %s%s\n%s", cl.BaseURL, chatSuffix, body)
}

func (cl *Client) logf(format string, args ...interface{}) {
	if !cl.LogRequests || cl.Logger == nil {
		return
	}
	cl.Logger(fmt.Sprintf(format, args...))
}

func toOpenAI(messages []domain.Message) []openai.ChatCompletionMessage {
	out := make([]openai.ChatCompletionMessage, 0, len(messages))
	for _, message := range messages {
		out = append(out, openai.ChatCompletionMessage{
			Role:    message.Role,
			Content: message.Content,
		})
	}
	return out
}

// toUsage flattens token accounting into the fields reported on stderr. The
// detail blocks are optional, so an absent one leaves its field zero.
func toUsage(usage openai.Usage) domain.Usage {
	out := domain.Usage{
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		TotalTokens:      usage.TotalTokens,
	}
	if usage.PromptTokensDetails != nil {
		out.CachedTokens = usage.PromptTokensDetails.CachedTokens
	}
	if usage.CompletionTokensDetails != nil {
		out.ReasoningTokens = usage.CompletionTokensDetails.ReasoningTokens
	}
	return out
}

// wrapAPIError adds the endpoint to an error, and unwraps go-openai's error
// types so the HTTP status and the server's message are both visible.
func wrapAPIError(err error, baseURL string) error {
	var apiErr *openai.APIError
	if errors.As(err, &apiErr) {
		return fmt.Errorf("%s: HTTP %d %s: %s",
			baseURL, apiErr.HTTPStatusCode, apiErr.HTTPStatus, apiErr.Message)
	}

	var reqErr *openai.RequestError
	if errors.As(err, &reqErr) {
		return fmt.Errorf("%s: %s", baseURL, reqErr.Err)
	}

	return err
}
