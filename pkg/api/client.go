// Package api is the low-level client for OpenAI-compatible endpoints. It
// wraps github.com/sashabaranov/go-openai, which is a transport and typed
// decoder only: it does not add a system prompt, tools or any other context
// to a request, and does not rewrite a response.
package api

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	openai "github.com/sashabaranov/go-openai"
)

// Client talks to one endpoint.
type Client struct {
	c       *openai.Client
	BaseURL string
}

// NewClient builds a client for baseURL. The base URL is stored exactly as
// given: no /v1 is appended and no path is rewritten, because
// go-openai joins it with the endpoint suffix verbatim.
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

	return &Client{c: openai.NewClientWithConfig(cfg), BaseURL: baseURL}, nil
}

// ListModels returns the model ids the endpoint advertises.
func (cl *Client) ListModels(ctx context.Context) ([]string, error) {
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
