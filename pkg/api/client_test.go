package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bhargavakumark/llm-cli/pkg/domain"
	openai "github.com/sashabaranov/go-openai"
)

// sseChunk renders one server-sent event carrying a chat completion chunk.
func sseChunk(t *testing.T, payload map[string]interface{}) string {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal chunk: %v", err)
	}
	return "data: " + string(body) + "\n\n"
}

func contentChunk(content string) map[string]interface{} {
	return map[string]interface{}{
		"id": "chunk", "object": "chat.completion.chunk", "created": 0, "model": "test",
		"choices": []map[string]interface{}{
			{"index": 0, "delta": map[string]interface{}{"content": content}, "finish_reason": nil},
		},
	}
}

func usageChunk(prompt, completion, total, cached, reasoning int) map[string]interface{} {
	return map[string]interface{}{
		"id": "chunk", "object": "chat.completion.chunk", "created": 0, "model": "test",
		"choices": []map[string]interface{}{},
		"usage": map[string]interface{}{
			"prompt_tokens":             prompt,
			"completion_tokens":         completion,
			"total_tokens":              total,
			"prompt_tokens_details":     map[string]interface{}{"cached_tokens": cached},
			"completion_tokens_details": map[string]interface{}{"reasoning_tokens": reasoning},
		},
	}
}

// streamServer answers a POST with the given event bodies, then closes.
func streamServer(t *testing.T, bodies ...string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		for _, body := range bodies {
			if _, err := io.WriteString(w, body); err != nil {
				return
			}
			w.(http.Flusher).Flush()
		}
	}))
}

func newTestClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	client, err := NewClient(server.URL, "sk-test", "")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	return client
}

func TestNewClientValidation(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		wantErr string
	}{
		{name: "valid", baseURL: "https://api.deepseek.com"},
		{name: "valid with path", baseURL: "http://127.0.0.1:6203/v1"},
		{name: "empty", baseURL: "", wantErr: "base_url is empty"},
		{name: "blank", baseURL: "   ", wantErr: "base_url is empty"},
		{name: "no scheme", baseURL: "api.deepseek.com", wantErr: "needs a scheme and host"},
		{name: "unparseable", baseURL: "http://[::1", wantErr: "invalid base_url"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewClient(test.baseURL, "sk-test", "")

			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("NewClient() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("NewClient() error = %v, want it to contain %q", err, test.wantErr)
			}
		})
	}
}

func TestNewClientKeepsBaseURLVerbatim(t *testing.T) {
	// No /v1 is added and no trailing slash is trimmed: the value is used as
	// given, since go-openai joins it with the endpoint suffix directly.
	for _, baseURL := range []string{
		"https://api.deepseek.com",
		"http://127.0.0.1:6203/v1",
		"https://host/openai/",
	} {
		client, err := NewClient(baseURL, "sk-test", "")
		if err != nil {
			t.Fatalf("NewClient(%q) error = %v", baseURL, err)
		}
		if client.BaseURL != baseURL {
			t.Errorf("BaseURL = %q, want %q", client.BaseURL, baseURL)
		}
	}
}

func TestTimeoutsAreConfigured(t *testing.T) {
	if RequestTimeout != 300*time.Second {
		t.Errorf("RequestTimeout = %v, want 300s", RequestTimeout)
	}
	if DialTimeout != 10*time.Second {
		t.Errorf("DialTimeout = %v, want 10s", DialTimeout)
	}

	client := newHTTPClient("")
	if client.Timeout != RequestTimeout {
		t.Errorf("http.Client.Timeout = %v, want %v", client.Timeout, RequestTimeout)
	}

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport is %T, want *http.Transport", client.Transport)
	}
	if transport.TLSHandshakeTimeout != DialTimeout {
		t.Errorf("TLSHandshakeTimeout = %v, want %v", transport.TLSHandshakeTimeout, DialTimeout)
	}
	if transport.DialContext == nil {
		t.Error("DialContext is nil, so dialing is unbounded")
	}
}

func TestInterfaceIPv4(t *testing.T) {
	t.Run("an unknown interface is an error naming it", func(t *testing.T) {
		_, err := InterfaceIPv4("no-such-interface-xyz")
		if err == nil || !strings.Contains(err.Error(), "no-such-interface-xyz") {
			t.Fatalf("InterfaceIPv4() error = %v, want it to name the interface", err)
		}
	})

	t.Run("loopback resolves to an IPv4 address", func(t *testing.T) {
		ip, err := InterfaceIPv4("lo0")
		if err != nil {
			t.Fatalf("InterfaceIPv4(lo0) error = %v", err)
		}
		if ip.To4() == nil {
			t.Errorf("InterfaceIPv4(lo0) = %v, want an IPv4 address", ip)
		}
		if !ip.IsLoopback() {
			t.Errorf("InterfaceIPv4(lo0) = %v, want a loopback address", ip)
		}
	})

	t.Run("an interface without an IPv4 address is an error", func(t *testing.T) {
		// Machine dependent: skip when every interface has an IPv4 address.
		interfaces, err := net.Interfaces()
		if err != nil {
			t.Fatalf("list interfaces: %v", err)
		}
		found := ""
		for _, iface := range interfaces {
			if _, err := InterfaceIPv4(iface.Name); err != nil &&
				strings.Contains(err.Error(), "no IPv4 address") {
				found = iface.Name
				break
			}
		}
		if found == "" {
			t.Skip("every interface on this machine has an IPv4 address")
		}

		_, err = InterfaceIPv4(found)
		if err == nil || !strings.Contains(err.Error(), "no IPv4 address") {
			t.Fatalf("InterfaceIPv4(%s) error = %v, want a missing-address error", found, err)
		}
	})
}

func TestBindInterfaceSendsRequestsOverThatInterface(t *testing.T) {
	// A bind to loopback must not break a request to a loopback test server,
	// which is the one case a binding can be exercised without a second
	// network path.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "sk-test", "lo0")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if client.BaseURL != server.URL {
		t.Errorf("BaseURL = %q, want the base URL kept verbatim", client.BaseURL)
	}

	response, err := client.Chat(context.Background(), "m", []domain.Message{{Role: domain.RoleUser, Content: "hi"}})
	if err == nil {
		t.Fatalf("Chat() = %+v, want a decode failure because the test server does not answer like an endpoint", response)
	}
	if strings.Contains(err.Error(), "bind interface") {
		t.Errorf("Chat() error = %v, want the request to reach the server rather than a bind failure", err)
	}
}

func TestBindInterfaceFailureNamesTheInterface(t *testing.T) {
	client, err := NewClient("http://127.0.0.1:1", "sk-test", "no-such-interface-xyz")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.ListModels(context.Background())
	if err == nil {
		t.Fatal("ListModels() error = nil, want the dial to fail")
	}
	if !strings.Contains(err.Error(), "no-such-interface-xyz") {
		t.Errorf("ListModels() error = %v, want it to name the interface", err)
	}
}

func TestChatStreamAssemblesContent(t *testing.T) {
	server := streamServer(t,
		sseChunk(t, contentChunk("Hello, ")),
		sseChunk(t, contentChunk("world.")),
		sseChunk(t, map[string]interface{}{
			"choices": []map[string]interface{}{
				{"index": 0, "delta": map[string]interface{}{}, "finish_reason": "stop"},
			},
		}),
		"data: [DONE]\n\n",
	)
	defer server.Close()

	client := newTestClient(t, server)

	var seen []string
	result, err := client.ChatStream(context.Background(), "test-model", []domain.Message{
		{Role: domain.RoleUser, Content: "hi"},
	}, func(chunk domain.Chunk) error {
		seen = append(seen, chunk.Content)
		return nil
	})
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}

	if result.Message.Content != "Hello, world." {
		t.Errorf("content = %q, want %q", result.Message.Content, "Hello, world.")
	}
	if result.Model != "test-model" {
		t.Errorf("model = %q, want test-model", result.Model)
	}
	if result.Chunks != 3 {
		t.Errorf("chunks = %d, want 3", result.Chunks)
	}
	if want := []string{"Hello, ", "world.", ""}; strings.Join(seen, "|") != strings.Join(want, "|") {
		t.Errorf("deltas = %v, want %v", seen, want)
	}
	if result.Usage.Reported() {
		t.Errorf("usage = %+v, want nothing reported", result.Usage)
	}
}

func TestChatStreamCapturesUsageFromFinalChunk(t *testing.T) {
	server := streamServer(t,
		sseChunk(t, contentChunk("ok")),
		sseChunk(t, usageChunk(11, 4, 15, 3, 2)),
		"data: [DONE]\n\n",
	)
	defer server.Close()

	client := newTestClient(t, server)
	client.IncludeUsage = true

	result, err := client.ChatStream(context.Background(), "test-model",
		[]domain.Message{{Role: domain.RoleUser, Content: "hi"}}, func(domain.Chunk) error { return nil })
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}

	want := domain.Usage{PromptTokens: 11, CompletionTokens: 4, TotalTokens: 15, CachedTokens: 3, ReasoningTokens: 2}
	if result.Usage != want {
		t.Errorf("usage = %+v, want %+v", result.Usage, want)
	}
}

func TestChatStreamRequestBody(t *testing.T) {
	tests := []struct {
		name         string
		includeUsage bool
		wantFields   []string
		denyFields   []string
	}{
		{
			name:         "usage off sends only model, messages and stream",
			includeUsage: false,
			wantFields:   []string{`"model":"test-model"`, `"stream":true`},
			denyFields:   []string{"stream_options", "include_usage", "temperature", "max_tokens", "tools"},
		},
		{
			name:         "usage on adds stream_options",
			includeUsage: true,
			wantFields:   []string{`"stream_options":{"include_usage":true}`},
			denyFields:   []string{"temperature", "max_tokens", "tools"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var got string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				got = string(body)
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				io.WriteString(w, sseChunk(t, contentChunk("ok")))
				io.WriteString(w, "data: [DONE]\n\n")
			}))
			defer server.Close()

			client := newTestClient(t, server)
			client.IncludeUsage = test.includeUsage

			if _, err := client.ChatStream(context.Background(), "test-model", []domain.Message{
				{Role: domain.RoleSystem, Content: "be terse"},
				{Role: domain.RoleUser, Content: "hi"},
			}, func(domain.Chunk) error { return nil }); err != nil {
				t.Fatalf("ChatStream() error = %v", err)
			}

			for _, field := range test.wantFields {
				if !strings.Contains(got, field) {
					t.Errorf("body %s does not contain %s", got, field)
				}
			}
			for _, field := range test.denyFields {
				if strings.Contains(got, field) {
					t.Errorf("body %s should not contain %s", got, field)
				}
			}
			if !strings.Contains(got, `{"role":"system","content":"be terse"}`) {
				t.Errorf("body %s lost the system message", got)
			}
		})
	}
}

func TestChatStreamStripsUsageFromLaterChunks(t *testing.T) {
	// Chunks carry "usage": null until the last one, and a null must not wipe
	// the report that follows.
	server := streamServer(t,
		sseChunk(t, map[string]interface{}{
			"choices": []map[string]interface{}{
				{"index": 0, "delta": map[string]interface{}{"content": "a"}, "finish_reason": nil},
			},
			"usage": nil,
		}),
		sseChunk(t, usageChunk(5, 1, 6, 0, 0)),
		"data: [DONE]\n\n",
	)
	defer server.Close()

	client := newTestClient(t, server)
	result, err := client.ChatStream(context.Background(), "test-model",
		[]domain.Message{{Role: domain.RoleUser, Content: "hi"}}, func(domain.Chunk) error { return nil })
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}

	if result.Usage.TotalTokens != 6 {
		t.Errorf("usage = %+v, want 6 total tokens", result.Usage)
	}
}

func TestChatStreamPassesReasoningThrough(t *testing.T) {
	server := streamServer(t,
		sseChunk(t, map[string]interface{}{
			"choices": []map[string]interface{}{
				{"index": 0, "delta": map[string]interface{}{"reasoning_content": "thinking"}, "finish_reason": nil},
			},
		}),
		sseChunk(t, contentChunk("done")),
		"data: [DONE]\n\n",
	)
	defer server.Close()

	client := newTestClient(t, server)

	var reasoning string
	if _, err := client.ChatStream(context.Background(), "test-model",
		[]domain.Message{{Role: domain.RoleUser, Content: "hi"}},
		func(chunk domain.Chunk) error {
			reasoning += chunk.Reasoning
			return nil
		}); err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}

	if reasoning != "thinking" {
		t.Errorf("reasoning = %q, want thinking", reasoning)
	}
}

func TestChatStreamWithNoContentIsAnError(t *testing.T) {
	server := streamServer(t, "data: [DONE]\n\n")
	defer server.Close()

	client := newTestClient(t, server)
	_, err := client.ChatStream(context.Background(), "test-model",
		[]domain.Message{{Role: domain.RoleUser, Content: "hi"}}, func(domain.Chunk) error { return nil })

	if !errors.Is(err, ErrNoContent) {
		t.Fatalf("ChatStream() error = %v, want ErrNoContent", err)
	}
	if !strings.Contains(err.Error(), server.URL) || !strings.Contains(err.Error(), "test-model") {
		t.Errorf("error %q should name the endpoint and the model", err)
	}
}

func TestChatStreamReportsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":{"message":"bad key","type":"authentication_error"}}`)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	_, err := client.ChatStream(context.Background(), "test-model",
		[]domain.Message{{Role: domain.RoleUser, Content: "hi"}}, func(domain.Chunk) error { return nil })

	if err == nil {
		t.Fatal("ChatStream() error = nil, want a failure")
	}
	for _, want := range []string{server.URL, "HTTP 401", "bad key"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should contain %q", err, want)
		}
	}
}

func TestChatStreamReportsErrorFrame(t *testing.T) {
	// An error delivered as a server-sent event is surfaced with its message.
	server := streamServer(t, "data: {\"error\":{\"message\":\"model overloaded\"}}\n\n")
	defer server.Close()

	client := newTestClient(t, server)
	_, err := client.ChatStream(context.Background(), "test-model",
		[]domain.Message{{Role: domain.RoleUser, Content: "hi"}}, func(domain.Chunk) error { return nil })

	if err == nil || !strings.Contains(err.Error(), "model overloaded") {
		t.Fatalf("ChatStream() error = %v, want the server error", err)
	}
}

func TestChatStreamPlainBodyYieldsNoContent(t *testing.T) {
	// A 200 with a plain JSON body is not server-sent events, so no choices are
	// seen and the stream ends empty. chat turns this into a hint about
	// --no-stream; here the contract is simply that it is an error.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, `{"model":"test-model","choices":[{"index":0,`+
			`"message":{"role":"assistant","content":"ignored"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	result, err := client.ChatStream(context.Background(), "test-model",
		[]domain.Message{{Role: domain.RoleUser, Content: "hi"}}, func(domain.Chunk) error { return nil })

	if !errors.Is(err, ErrNoContent) {
		t.Fatalf("ChatStream() error = %v, want ErrNoContent", err)
	}
	if result.Message.Content != "" {
		t.Errorf("content = %q, want nothing captured", result.Message.Content)
	}
}

func TestChatStreamPropagatesCallbackError(t *testing.T) {
	server := streamServer(t, sseChunk(t, contentChunk("ok")), "data: [DONE]\n\n")
	defer server.Close()

	client := newTestClient(t, server)
	sentinel := errors.New("stdout is closed")

	_, err := client.ChatStream(context.Background(), "test-model",
		[]domain.Message{{Role: domain.RoleUser, Content: "hi"}},
		func(domain.Chunk) error { return sentinel })

	if !errors.Is(err, sentinel) {
		t.Fatalf("ChatStream() error = %v, want the callback error unchanged", err)
	}
}

func TestChatStreamStopsOnCancelledContext(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, sseChunk(t, contentChunk("first")))
		w.(http.Flusher).Flush()
		<-release
	}))
	defer server.Close()
	defer close(release)

	ctx, cancel := context.WithCancel(context.Background())
	client := newTestClient(t, server)

	gotFirst := make(chan struct{})
	go func() {
		client.ChatStream(ctx, "test-model", []domain.Message{{Role: domain.RoleUser, Content: "hi"}},
			func(chunk domain.Chunk) error {
				if chunk.Content == "first" {
					close(gotFirst)
				}
				return nil
			})
	}()

	select {
	case <-gotFirst:
	case <-time.After(5 * time.Second):
		t.Fatal("no chunk arrived before the context was cancelled")
	}

	cancel()

	// The stream read must end once the context is cancelled rather than
	// waiting for the server forever.
	done := make(chan error, 1)
	go func() {
		_, err := client.ChatStream(ctx, "test-model",
			[]domain.Message{{Role: domain.RoleUser, Content: "hi"}}, func(domain.Chunk) error { return nil })
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Error("ChatStream() error = nil, want a cancellation failure")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ChatStream() did not return after the context was cancelled")
	}
}

func TestChatNonStreaming(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Errorf("Authorization = %q, want Bearer sk-test", got)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"model":"test-model","choices":[{"index":0,`+
			`"message":{"role":"assistant","content":"Hello, world."},"finish_reason":"stop"}],`+
			`"usage":{"prompt_tokens":8,"completion_tokens":5,"total_tokens":13}}`)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	result, err := client.Chat(context.Background(), "test-model",
		[]domain.Message{{Role: domain.RoleUser, Content: "hi"}})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	if result.Message.Content != "Hello, world." || result.Message.Role != "assistant" {
		t.Errorf("message = %+v, want the assistant reply", result.Message)
	}
	if result.Usage != (domain.Usage{PromptTokens: 8, CompletionTokens: 5, TotalTokens: 13}) {
		t.Errorf("usage = %+v, want 8/5/13", result.Usage)
	}
}

func TestChatNonStreamingWithNoChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"model":"test-model","choices":[]}`)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	_, err := client.Chat(context.Background(), "test-model",
		[]domain.Message{{Role: domain.RoleUser, Content: "hi"}})

	if err == nil || !strings.Contains(err.Error(), "no choices") {
		t.Fatalf("Chat() error = %v, want a no-choices failure", err)
	}
}

func TestListModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/models") {
			t.Errorf("path = %s, want a /models suffix", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"object":"list","data":[{"id":"b"},{"id":"a"}]}`)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	ids, err := client.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if strings.Join(ids, ",") != "b,a" {
		t.Errorf("ids = %v, want [b a]", ids)
	}
}

func TestListModelsReportsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, `{"error":{"message":"boom"}}`)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	_, err := client.ListModels(context.Background())

	if err == nil || !strings.Contains(err.Error(), "HTTP 500") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("ListModels() error = %v, want HTTP 500 boom", err)
	}
}

func TestLogRequestsDumpsTheBody(t *testing.T) {
	server := streamServer(t, sseChunk(t, contentChunk("ok")), "data: [DONE]\n\n")
	defer server.Close()

	client := newTestClient(t, server)
	var dumps []string
	client.LogRequests = true
	client.Logger = func(dump string) { dumps = append(dumps, dump) }

	if _, err := client.ChatStream(context.Background(), "test-model",
		[]domain.Message{{Role: domain.RoleUser, Content: "hi"}}, func(domain.Chunk) error { return nil }); err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}

	if len(dumps) != 1 {
		t.Fatalf("got %d dumps, want 1", len(dumps))
	}
	if !strings.Contains(dumps[0], "POST "+server.URL+"/chat/completions") {
		t.Errorf("dump %q should name the URL", dumps[0])
	}
	if !strings.Contains(dumps[0], `"content":"hi"`) {
		t.Errorf("dump %q should contain the request body", dumps[0])
	}
}

func TestLogRequestsOffByDefault(t *testing.T) {
	server := streamServer(t, sseChunk(t, contentChunk("ok")), "data: [DONE]\n\n")
	defer server.Close()

	client := newTestClient(t, server)
	called := false
	client.Logger = func(string) { called = true }

	if _, err := client.ChatStream(context.Background(), "test-model",
		[]domain.Message{{Role: domain.RoleUser, Content: "hi"}}, func(domain.Chunk) error { return nil }); err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}

	if called {
		t.Error("Logger was called with LogRequests off")
	}
}

func TestToUsageHandlesMissingDetails(t *testing.T) {
	got := toUsage(openai.Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3})

	if got.CachedTokens != 0 || got.ReasoningTokens != 0 {
		t.Errorf("usage = %+v, want zero details", got)
	}

	withDetails := toUsage(openai.Usage{
		PromptTokens:            1,
		CompletionTokens:        2,
		TotalTokens:             3,
		PromptTokensDetails:     &openai.PromptTokensDetails{CachedTokens: 7},
		CompletionTokensDetails: &openai.CompletionTokensDetails{ReasoningTokens: 9},
	})
	if withDetails.CachedTokens != 7 || withDetails.ReasoningTokens != 9 {
		t.Errorf("usage = %+v, want 7 cached and 9 reasoning", withDetails)
	}
}

func TestToOpenAIPreservesMessages(t *testing.T) {
	got := toOpenAI([]domain.Message{
		{Role: domain.RoleSystem, Content: "be terse"},
		{Role: domain.RoleUser, Content: "hi"},
	})

	if len(got) != 2 {
		t.Fatalf("got %d messages, want 2", len(got))
	}
	if got[0].Role != domain.RoleSystem || got[0].Content != "be terse" {
		t.Errorf("message[0] = %+v, want the system message", got[0])
	}
	if got[1].Role != domain.RoleUser || got[1].Content != "hi" {
		t.Errorf("message[1] = %+v, want the user message", got[1])
	}
}

func TestWrapAPIError(t *testing.T) {
	t.Run("api error keeps the status and message", func(t *testing.T) {
		err := wrapAPIError(&openai.APIError{
			HTTPStatusCode: 429,
			HTTPStatus:     "429 Too Many Requests",
			Message:        "slow down",
		}, "http://host")

		for _, want := range []string{"http://host", "HTTP 429", "slow down"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q should contain %q", err, want)
			}
		}
	})

	t.Run("request error keeps the endpoint", func(t *testing.T) {
		err := wrapAPIError(&openai.RequestError{
			HTTPStatusCode: 502,
			Err:            errors.New("connection refused"),
		}, "http://host")

		if !strings.Contains(err.Error(), "http://host") || !strings.Contains(err.Error(), "connection refused") {
			t.Errorf("error %q should name the endpoint and the cause", err)
		}
	})

	t.Run("other errors pass through", func(t *testing.T) {
		sentinel := fmt.Errorf("plain failure")

		if got := wrapAPIError(sentinel, "http://host"); got != sentinel {
			t.Errorf("wrapAPIError() = %v, want the original error", got)
		}
	})
}
