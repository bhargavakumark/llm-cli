package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bhargavakumark/llm-cli/pkg/config"
)

// modelsServer answers GET <prefix>/models with the given ids and records the
// path and Authorization header it was asked for.
func modelsServer(t *testing.T, prefix string, ids []string) (*httptest.Server, *string, *string) {
	t.Helper()
	var path, auth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		auth = r.Header.Get("Authorization")

		models := make([]map[string]string, 0, len(ids))
		for _, id := range ids {
			models = append(models, map[string]string{"id": id, "object": "model", "owned_by": "test"})
		}
		body, err := json.Marshal(map[string]interface{}{"object": "list", "data": models})
		if err != nil {
			t.Errorf("marshal models: %v", err)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if prefix != "" && r.URL.Path != prefix+"/models" {
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"error":{"message":"wrong path"}}`)
			return
		}
		w.Write(body)
	}))
	t.Cleanup(server.Close)

	return server, &path, &auth
}

func TestModelsListsOneIDPerLine(t *testing.T) {
	useTempHome(t)
	server, path, _ := modelsServer(t, "", []string{"deepseek-flash", "deepseek-v4-pro"})
	targetConfig(t, "ds", server.URL, false)

	stdout, stderr, err := runRootCaptured(t, false, "models", "--llm", "ds")
	if err != nil {
		t.Fatalf("models error = %v", err)
	}

	if stdout != "deepseek-flash\ndeepseek-v4-pro\n" {
		t.Errorf("stdout = %q, want one id per line in the endpoint's order", stdout)
	}
	if *path != "/models" {
		t.Errorf("requested path = %q, want /models", *path)
	}
	if !strings.Contains(stderr, "ds -> "+server.URL+" (2 models)") {
		t.Errorf("stderr = %q, want a grey summary line", stderr)
	}
	if strings.Contains(stdout, "\x1b[") {
		t.Errorf("stdout contains escape codes: %q", stdout)
	}
}

func TestModelsKeepsTheBaseURLPath(t *testing.T) {
	useTempHome(t)
	server, path, _ := modelsServer(t, "/v1", []string{"m"})
	targetConfig(t, "local", server.URL+"/v1", false)

	if _, _, err := runRoot(t, "", "models", "--llm", "local"); err != nil {
		t.Fatalf("models error = %v", err)
	}

	if *path != "/v1/models" {
		t.Errorf("requested path = %q, want the base URL path kept and /models appended", *path)
	}
}

func TestModelsSendsTheConfiguredCredential(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want string
	}{
		{name: "literal key", key: "sk-test", want: "Bearer sk-test"},
		{name: "no key", key: "", want: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			useTempHome(t)
			server, _, auth := modelsServer(t, "", []string{"m"})
			saveConfig(t, &config.Config{
				DefaultLLM: "t",
				LLMs: map[string]config.Target{
					"t": {BaseURL: server.URL, Model: "m", APIKey: test.key},
				},
			})

			if _, _, err := runRoot(t, "", "models", "--llm", "t"); err != nil {
				t.Fatalf("models error = %v", err)
			}

			if *auth != test.want {
				t.Errorf("Authorization = %q, want %q", *auth, test.want)
			}
		})
	}
}

func TestModelsResolvesAliasesAndTheDefault(t *testing.T) {
	useTempHome(t)
	server, _, _ := modelsServer(t, "", []string{"m"})
	saveConfig(t, &config.Config{
		DefaultLLM: "ds",
		LLMs: map[string]config.Target{
			"ds": {Aliases: []string{"deepseek"}, BaseURL: server.URL, Model: "m"},
		},
	})

	t.Run("by alias", func(t *testing.T) {
		stdout, _, err := runRoot(t, "", "models", "--llm", "deepseek")
		if err != nil {
			t.Fatalf("models error = %v", err)
		}
		if stdout != "m\n" {
			t.Errorf("stdout = %q, want m", stdout)
		}
	})

	t.Run("the default when --llm is omitted", func(t *testing.T) {
		stdout, _, err := runRoot(t, "", "models")
		if err != nil {
			t.Fatalf("models error = %v", err)
		}
		if stdout != "m\n" {
			t.Errorf("stdout = %q, want m", stdout)
		}
	})
}

func TestModelsEmptyListIsSuccess(t *testing.T) {
	useTempHome(t)
	server, _, _ := modelsServer(t, "", nil)
	targetConfig(t, "ds", server.URL, false)

	stdout, stderr, err := runRootCaptured(t, false, "models", "--llm", "ds")
	if err != nil {
		t.Fatalf("models error = %v, want an advertised empty list to succeed", err)
	}

	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
	}
	if !strings.Contains(stderr, "(0 models)") {
		t.Errorf("stderr = %q, want the count", stderr)
	}
}

func TestModelsQuietPrintsOnlyTheList(t *testing.T) {
	useTempHome(t)
	server, _, _ := modelsServer(t, "", []string{"m"})
	targetConfig(t, "ds", server.URL, false)

	stdout, stderr, err := runRootCaptured(t, true, "models", "--llm", "ds", "--quiet")
	if err != nil {
		t.Fatalf("models error = %v", err)
	}

	if stdout != "m\n" {
		t.Errorf("stdout = %q, want m", stdout)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing when quiet", stderr)
	}
}

func TestModelsErrors(t *testing.T) {
	t.Run("unknown target", func(t *testing.T) {
		useTempHome(t)
		saveConfig(t, &config.Config{
			LLMs: map[string]config.Target{"ds": {BaseURL: "http://127.0.0.1:1", Model: "m"}},
		})

		stdout, _, err := runRoot(t, "", "models", "--llm", "ghost")
		if err == nil || !strings.Contains(err.Error(), "unknown llm") {
			t.Fatalf("models error = %v, want an unknown-target failure", err)
		}
		if stdout != "" {
			t.Errorf("stdout = %q, want nothing on failure", stdout)
		}
	})

	t.Run("no config", func(t *testing.T) {
		useTempHome(t)

		if _, _, err := runRoot(t, "", "models", "--llm", "ds"); err == nil ||
			!strings.Contains(err.Error(), "no config") {
			t.Fatalf("models error = %v, want a missing-config failure", err)
		}
	})

	t.Run("http error names the url", func(t *testing.T) {
		useTempHome(t)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"error":{"message":"bad key"}}`)
		}))
		defer server.Close()
		targetConfig(t, "ds", server.URL, false)

		stdout, _, err := runRoot(t, "", "models", "--llm", "ds")
		if err == nil {
			t.Fatal("models error = nil, want a failure")
		}
		for _, want := range []string{"HTTP 401", "bad key", server.URL} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q should contain %q", err, want)
			}
		}
		if stdout != "" {
			t.Errorf("stdout = %q, want nothing on failure", stdout)
		}
	})

	t.Run("unreachable endpoint", func(t *testing.T) {
		useTempHome(t)
		// A closed port on the loopback address, so the dial fails at once.
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		server.Close()
		targetConfig(t, "ds", server.URL, false)

		_, _, err := runRoot(t, "", "models", "--llm", "ds")
		if err == nil {
			t.Fatal("models error = nil, want a connection failure")
		}
		if !strings.Contains(err.Error(), server.URL) {
			t.Errorf("error %q should name the endpoint", err)
		}
	})
}

func TestModelsRejectsArguments(t *testing.T) {
	useTempHome(t)
	server, _, _ := modelsServer(t, "", []string{"m"})
	targetConfig(t, "ds", server.URL, false)

	if _, _, err := runRoot(t, "", "models", "--llm", "ds", "extra"); err == nil {
		t.Fatal("models error = nil, want a rejection of arguments")
	}
}

func TestModelsLogRequestsGoesToStderr(t *testing.T) {
	useTempHome(t)
	server, _, _ := modelsServer(t, "", []string{"m"})
	targetConfig(t, "ds", server.URL, false)

	stdout, stderr, err := runRootCaptured(t, false, "models", "--llm", "ds", "--log-requests")
	if err != nil {
		t.Fatalf("models error = %v", err)
	}

	if stdout != "m\n" {
		t.Errorf("stdout = %q, want only the list", stdout)
	}
	if !strings.Contains(stderr, "GET "+server.URL+"/models") {
		t.Errorf("stderr %q should contain the outgoing request", stderr)
	}
}

func TestModelsWriteErrorsPropagate(t *testing.T) {
	useTempHome(t)
	server, _, _ := modelsServer(t, "", []string{"m"})
	targetConfig(t, "ds", server.URL, false)

	cmd := newRootCmd()
	quiet = true
	cmd.SetOut(failingWriter{})
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"models", "--llm", "ds"})

	if err := cmd.Execute(); err == nil {
		t.Fatal("models error = nil, want a write failure to propagate")
	}
}

// failingWriter fails every write, standing in for a closed pipe.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, fmt.Errorf("pipe closed") }
