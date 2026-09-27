package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bhargavakumark/llm-cli/pkg/config"
)

// chunkLine renders one server-sent event for a chat completion chunk.
func chunkLine(t *testing.T, payload map[string]interface{}) string {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal chunk: %v", err)
	}
	return "data: " + string(body) + "\n\n"
}

func deltaChunk(content string) map[string]interface{} {
	return map[string]interface{}{
		"choices": []map[string]interface{}{
			{"index": 0, "delta": map[string]interface{}{"content": content}, "finish_reason": nil},
		},
	}
}

func usageLine(prompt, completion, total int) map[string]interface{} {
	return map[string]interface{}{
		"choices": []map[string]interface{}{},
		"usage": map[string]interface{}{
			"prompt_tokens":     prompt,
			"completion_tokens": completion,
			"total_tokens":      total,
		},
	}
}

// sseServer answers with the given events and records the last request body.
func sseServer(t *testing.T, events ...string) (*httptest.Server, *string) {
	t.Helper()
	var body string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		for _, event := range events {
			io.WriteString(w, event)
			w.(http.Flusher).Flush()
		}
	}))
	t.Cleanup(server.Close)

	return server, &body
}

// targetConfig writes a config with one target pointing at baseURL.
func targetConfig(t *testing.T, name, baseURL string, includeUsage bool) {
	t.Helper()
	saveConfig(t, &config.Config{
		DefaultLLM: name,
		LLMs: map[string]config.Target{
			name: {BaseURL: baseURL, Model: "test-model", APIKey: "sk-test", IncludeUsage: includeUsage},
		},
	})
}

// runChatCommand runs chat with os.Stdout and os.Stderr captured, which is what
// chat writes to.
func runChatCommand(t *testing.T, quietMode bool, args ...string) (string, string, error) {
	t.Helper()

	var runErr error
	stdout, stderr := captureStdio(t, func() {
		cmd := newRootCmd()
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SetIn(strings.NewReader(""))
		cmd.SetArgs(args)

		quiet = quietMode
		runErr = cmd.Execute()
	})
	return stdout, stderr, runErr
}

// usageAwareServer sends a usage chunk only when the request asked for one,
// the way a real endpoint behaves.
func usageAwareServer(t *testing.T) (*httptest.Server, *string) {
	t.Helper()
	var body string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, chunkLine(t, deltaChunk("answer")))
		if strings.Contains(body, `"include_usage":true`) {
			io.WriteString(w, chunkLine(t, usageLine(11, 4, 15)))
		}
		io.WriteString(w, "data: [DONE]\n\n")
		w.(http.Flusher).Flush()
	}))
	t.Cleanup(server.Close)

	return server, &body
}

func TestChatWritesOnlyTheAnswerToStdout(t *testing.T) {
	useTempHome(t)
	server, _ := sseServer(t,
		chunkLine(t, deltaChunk("Hello, ")),
		chunkLine(t, deltaChunk("world.")),
		"data: [DONE]\n\n",
	)
	targetConfig(t, "stub", server.URL, false)

	stdout, stderr, err := runChatCommand(t, false, "chat", "--llm", "stub", "hi")
	if err != nil {
		t.Fatalf("chat error = %v", err)
	}

	if stdout != "Hello, world.\n" {
		t.Errorf("stdout = %q, want exactly the answer", stdout)
	}
	if !strings.Contains(stderr, "stub -> test-model") || !strings.Contains(stderr, "chunks") {
		t.Errorf("stderr = %q, want the target, model, chunk count and elapsed time", stderr)
	}
	if strings.Contains(stdout, "\x1b[") {
		t.Errorf("stdout contains escape codes: %q", stdout)
	}
}

func TestChatNoStreamWritesTheWholeAnswer(t *testing.T) {
	useTempHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"model":"test-model","choices":[{"index":0,`+
			`"message":{"role":"assistant","content":"Hello, world."},"finish_reason":"stop"}],`+
			`"usage":{"prompt_tokens":8,"completion_tokens":5,"total_tokens":13}}`)
	}))
	defer server.Close()
	targetConfig(t, "stub", server.URL, false)

	stdout, stderr, err := runChatCommand(t, false, "chat", "--llm", "stub", "--no-stream", "hi")
	if err != nil {
		t.Fatalf("chat error = %v", err)
	}

	if stdout != "Hello, world.\n" {
		t.Errorf("stdout = %q, want exactly the answer", stdout)
	}
	// A non-streamed reply carries usage without include_usage being set.
	if !strings.Contains(stderr, "tokens: 8 prompt + 5 completion = 13 total") {
		t.Errorf("stderr = %q, want the token line", stderr)
	}
}

func TestChatUsageLineRequiresTheTargetSetting(t *testing.T) {
	tests := []struct {
		name         string
		includeUsage bool
		wantLine     bool
	}{
		{name: "include_usage on", includeUsage: true, wantLine: true},
		{name: "include_usage off", includeUsage: false, wantLine: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			useTempHome(t)
			server, _ := usageAwareServer(t)
			targetConfig(t, "stub", server.URL, test.includeUsage)

			stdout, stderr, err := runChatCommand(t, false, "chat", "--llm", "stub", "hi")
			if err != nil {
				t.Fatalf("chat error = %v", err)
			}
			if stdout != "answer\n" {
				t.Errorf("stdout = %q, want only the answer", stdout)
			}

			hasLine := strings.Contains(stderr, "tokens: 11 prompt + 4 completion = 15 total")
			if hasLine != test.wantLine {
				t.Errorf("token line present = %v, want %v (stderr %q)", hasLine, test.wantLine, stderr)
			}
		})
	}
}

func TestChatSendsStreamOptionsOnlyWhenConfigured(t *testing.T) {
	tests := []struct {
		name         string
		includeUsage bool
		want         string
		absent       string
	}{
		{name: "on", includeUsage: true, want: `"stream_options":{"include_usage":true}`, absent: ""},
		{name: "off", includeUsage: false, want: `"stream":true`, absent: "stream_options"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			useTempHome(t)
			server, body := sseServer(t, chunkLine(t, deltaChunk("ok")), "data: [DONE]\n\n")
			targetConfig(t, "stub", server.URL, test.includeUsage)

			if _, _, err := runChatCommand(t, false, "chat", "--llm", "stub", "hi"); err != nil {
				t.Fatalf("chat error = %v", err)
			}

			if !strings.Contains(*body, test.want) {
				t.Errorf("request body %s should contain %s", *body, test.want)
			}
			if test.absent != "" && strings.Contains(*body, test.absent) {
				t.Errorf("request body %s should not contain %s", *body, test.absent)
			}
		})
	}
}

func TestChatReportsAnHTTPError(t *testing.T) {
	useTempHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":{"message":"bad key","type":"authentication_error"}}`)
	}))
	defer server.Close()
	targetConfig(t, "stub", server.URL, false)

	stdout, _, err := runChatCommand(t, false, "chat", "--llm", "stub", "hi")

	if err == nil {
		t.Fatal("chat error = nil, want a failure")
	}
	for _, want := range []string{"HTTP 401", "bad key"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should contain %q", err, want)
		}
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing on failure", stdout)
	}
}

func TestChatHintsAtNoStreamForAnEmptyStream(t *testing.T) {
	useTempHome(t)
	server, _ := sseServer(t, "data: [DONE]\n\n")
	targetConfig(t, "stub", server.URL, false)

	stdout, _, err := runChatCommand(t, false, "chat", "--llm", "stub", "hi")

	if err == nil || !strings.Contains(err.Error(), "--no-stream") {
		t.Fatalf("chat error = %v, want a --no-stream hint", err)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
	}
}

func TestChatUnknownTarget(t *testing.T) {
	useTempHome(t)
	saveConfig(t, &config.Config{
		LLMs: map[string]config.Target{"stub": {BaseURL: "http://127.0.0.1:1", Model: "m"}},
	})

	_, _, err := runChatCommand(t, false, "chat", "--llm", "ghost", "hi")

	if err == nil || !strings.Contains(err.Error(), "unknown llm") {
		t.Fatalf("chat error = %v, want an unknown-target failure", err)
	}
}

func TestChatLogRequestsGoesToStderr(t *testing.T) {
	useTempHome(t)
	server, _ := sseServer(t, chunkLine(t, deltaChunk("answer")), "data: [DONE]\n\n")
	targetConfig(t, "stub", server.URL, false)

	stdout, stderr, err := runChatCommand(t, false, "chat", "--llm", "stub", "--log-requests", "hi")
	if err != nil {
		t.Fatalf("chat error = %v", err)
	}

	if stdout != "answer\n" {
		t.Errorf("stdout = %q, want only the answer", stdout)
	}
	if !strings.Contains(stderr, "POST "+server.URL+"/chat/completions") {
		t.Errorf("stderr %q should contain the outgoing request", stderr)
	}
	if !strings.Contains(stderr, `"content":"hi"`) {
		t.Errorf("stderr %q should contain the request body", stderr)
	}
}

func TestChatSystemPromptIsSent(t *testing.T) {
	useTempHome(t)
	server, body := sseServer(t, chunkLine(t, deltaChunk("ok")), "data: [DONE]\n\n")
	targetConfig(t, "stub", server.URL, false)

	if _, _, err := runChatCommand(t, false, "chat", "--llm", "stub", "-s", "be terse", "hi"); err != nil {
		t.Fatalf("chat error = %v", err)
	}

	if !strings.Contains(*body, `{"role":"system","content":"be terse"}`) {
		t.Errorf("request body %s should start with the system message", *body)
	}
}

func TestChatQuietSuppressesProgress(t *testing.T) {
	useTempHome(t)
	server, _ := sseServer(t, chunkLine(t, deltaChunk("answer")), "data: [DONE]\n\n")
	targetConfig(t, "stub", server.URL, false)

	stdout, stderr, err := runChatCommand(t, true, "chat", "--llm", "stub", "hi")
	if err != nil {
		t.Fatalf("chat error = %v", err)
	}

	if stdout != "answer\n" {
		t.Errorf("stdout = %q, want the answer", stdout)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing when quiet", stderr)
	}
}
