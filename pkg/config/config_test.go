package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withHome points the config path at a temporary directory.
func withHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

func TestConfigPathUsesHome(t *testing.T) {
	home := withHome(t)

	want := filepath.Join(home, ".config", "llm-cli", "config.json")
	if got := ConfigPath(); got != want {
		t.Fatalf("ConfigPath() = %q, want %q", got, want)
	}
}

func TestLoadMissingFileReportsNoConfig(t *testing.T) {
	withHome(t)

	_, err := Load()
	if !errors.Is(err, ErrNoConfig) {
		t.Fatalf("Load() error = %v, want an error wrapping ErrNoConfig", err)
	}
	if !strings.Contains(err.Error(), "auth setup") {
		t.Errorf("error should point at auth setup, got %q", err)
	}
}

func TestLoadRejectsInvalidJSON(t *testing.T) {
	home := withHome(t)
	writeFile(t, filepath.Join(home, ".config", "llm-cli", "config.json"), "{not json")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "parse") {
		t.Fatalf("Load() error = %v, want a parse failure", err)
	}
}

func TestLoadValidatesAliases(t *testing.T) {
	home := withHome(t)
	path := filepath.Join(home, ".config", "llm-cli", "config.json")
	writeFile(t, path, `{"llms":{"a":{"base_url":"http://a","model":"m","aliases":["b"]},`+
		`"b":{"base_url":"http://b","model":"m"}}}`)

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "collides") {
		t.Fatalf("Load() error = %v, want an alias collision", err)
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error should name the config file, got %q", err)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	withHome(t)

	saved := &Config{
		DefaultLLM: "deepseek",
		LLMs: map[string]Target{
			"deepseek": {
				Aliases:      []string{"ds"},
				BaseURL:      "https://api.deepseek.com",
				Model:        "deepseek-flash",
				APIKeyEnv:    "DEEPSEEK_API_KEY",
				IncludeUsage: true,
			},
			"local": {BaseURL: "http://127.0.0.1:6203/v1", Model: "qwen3"},
		},
	}
	if err := Save(saved); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.DefaultLLM != "deepseek" {
		t.Errorf("DefaultLLM = %q, want deepseek", loaded.DefaultLLM)
	}
	if got := loaded.LLMs["deepseek"].IncludeUsage; !got {
		t.Error("IncludeUsage did not survive the round trip")
	}
	if got := loaded.LLMs["deepseek"].Aliases; len(got) != 1 || got[0] != "ds" {
		t.Errorf("Aliases = %v, want [ds]", got)
	}
	// A field left at its zero value stays out of the file.
	if strings.Contains(readFile(t, ConfigPath()), "include_usage\": true,\n    \"local\"") {
		t.Error("local should not have an include_usage key")
	}
}

func TestSaveUsesOwnerOnlyPermissions(t *testing.T) {
	home := withHome(t)

	if err := Save(&Config{LLMs: map[string]Target{}}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	dirInfo, err := os.Stat(filepath.Join(home, ".config", "llm-cli"))
	if err != nil {
		t.Fatalf("stat config dir: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != 0700 {
		t.Errorf("dir mode = %v, want 0700", got)
	}

	fileInfo, err := os.Stat(ConfigPath())
	if err != nil {
		t.Fatalf("stat config file: %v", err)
	}
	if got := fileInfo.Mode().Perm(); got != 0600 {
		t.Errorf("file mode = %v, want 0600", got)
	}
}

func TestSaveWritesTrailingNewline(t *testing.T) {
	withHome(t)

	if err := Save(&Config{LLMs: map[string]Target{}}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	contents := readFile(t, ConfigPath())
	if !strings.HasSuffix(contents, "\n") {
		t.Errorf("config should end with a newline, got %q", contents)
	}
	if !json.Valid([]byte(contents)) {
		t.Errorf("config is not valid JSON: %q", contents)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr string
	}{
		{
			name: "valid",
			config: Config{
				DefaultLLM: "a",
				LLMs: map[string]Target{
					"a": {BaseURL: "http://a", Model: "m", Aliases: []string{"x"}},
					"b": {BaseURL: "http://b", Model: "m", Aliases: []string{"y"}},
				},
			},
		},
		{
			name:    "blank target name",
			config:  Config{LLMs: map[string]Target{" ": {BaseURL: "http://a"}}},
			wantErr: "blank name",
		},
		{
			name:    "whitespace in target name",
			config:  Config{LLMs: map[string]Target{"two words": {BaseURL: "http://a"}}},
			wantErr: "contains whitespace",
		},
		{
			name:    "blank alias",
			config:  Config{LLMs: map[string]Target{"a": {Aliases: []string{""}}}},
			wantErr: "blank alias",
		},
		{
			name:    "whitespace in alias",
			config:  Config{LLMs: map[string]Target{"a": {Aliases: []string{"two words"}}}},
			wantErr: "contains whitespace",
		},
		{
			name: "alias collides with another target name",
			config: Config{LLMs: map[string]Target{
				"a": {Aliases: []string{"b"}},
				"b": {},
			}},
			wantErr: `alias "b" on target "a" collides with "b"`,
		},
		{
			name: "alias collides with another target alias",
			config: Config{LLMs: map[string]Target{
				"a": {Aliases: []string{"shared"}},
				"b": {Aliases: []string{"shared"}},
			}},
			wantErr: `alias "shared" on target "b" collides with "a"`,
		},
		{
			name:    "alias repeats the target name",
			config:  Config{LLMs: map[string]Target{"a": {Aliases: []string{"a"}}}},
			wantErr: "own name",
		},
		{
			name:    "duplicate alias on one target",
			config:  Config{LLMs: map[string]Target{"a": {Aliases: []string{"x", "x"}}}},
			wantErr: `alias "x" is repeated on target "a"`,
		},
		{
			name:    "default is not a target",
			config:  Config{DefaultLLM: "ghost", LLMs: map[string]Target{"a": {}}},
			wantErr: `default_llm "ghost" is not a configured target`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.config.Validate()

			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() error = nil, want %q", test.wantErr)
			}
			if !strings.Contains(err.Error(), test.wantErr) {
				t.Errorf("Validate() error = %q, want it to contain %q", err, test.wantErr)
			}
		})
	}
}

func TestResolve(t *testing.T) {
	cfg := &Config{
		DefaultLLM: "deepseek",
		LLMs: map[string]Target{
			"deepseek": {BaseURL: "http://deepseek", Model: "deepseek-flash", Aliases: []string{"ds"}},
			"gpt6":     {BaseURL: "http://gpt6", Model: "gpt-6-luna", Aliases: []string{"luna"}},
		},
	}

	tests := []struct {
		name      string
		query     string
		wantName  string
		wantModel string
		wantErr   string
	}{
		{name: "by name", query: "gpt6", wantName: "gpt6", wantModel: "gpt-6-luna"},
		{name: "by alias", query: "luna", wantName: "gpt6", wantModel: "gpt-6-luna"},
		{name: "empty falls back to the default", query: "", wantName: "deepseek", wantModel: "deepseek-flash"},
		{name: "unknown", query: "ghost", wantErr: "known targets are deepseek, gpt6"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			name, target, err := cfg.Resolve(test.query)

			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("Resolve(%q) error = %v, want it to contain %q", test.query, err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Resolve(%q) error = %v", test.query, err)
			}
			if name != test.wantName || target.Model != test.wantModel {
				t.Errorf("Resolve(%q) = (%q, %q), want (%q, %q)",
					test.query, name, target.Model, test.wantName, test.wantModel)
			}
		})
	}
}

func TestResolveWithNoTargets(t *testing.T) {
	cfg := &Config{}

	_, _, err := cfg.Resolve("ds")
	if err == nil || !strings.Contains(err.Error(), "no targets configured") {
		t.Fatalf("Resolve() error = %v, want a no-targets message", err)
	}

	_, _, err = cfg.Resolve("")
	if err == nil || !strings.Contains(err.Error(), "no default set") {
		t.Fatalf("Resolve(\"\") error = %v, want a no-default message", err)
	}
}

func TestAPIKeyValue(t *testing.T) {
	tests := []struct {
		name   string
		target Target
		env    map[string]string
		want   string
	}{
		{name: "literal key", target: Target{APIKey: "sk-literal"}, want: "sk-literal"},
		{
			name:   "env var wins over a literal key",
			target: Target{APIKey: "sk-literal", APIKeyEnv: "TEST_LLM_KEY"},
			env:    map[string]string{"TEST_LLM_KEY": "sk-from-env"},
			want:   "sk-from-env",
		},
		{
			name:   "unset env var yields no key",
			target: Target{APIKey: "sk-literal", APIKeyEnv: "TEST_LLM_MISSING"},
			want:   "",
		},
		{name: "no credential at all", target: Target{}, want: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for key, value := range test.env {
				t.Setenv(key, value)
			}

			if got := test.target.APIKeyValue(); got != test.want {
				t.Errorf("APIKeyValue() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestHasCredential(t *testing.T) {
	tests := []struct {
		target Target
		want   bool
	}{
		{Target{APIKey: "k"}, true},
		{Target{APIKeyEnv: "VAR"}, true},
		{Target{}, false},
	}

	for _, test := range tests {
		if got := test.target.HasCredential(); got != test.want {
			t.Errorf("HasCredential() for %+v = %v, want %v", test.target, got, test.want)
		}
	}
}

func TestNamesAreSorted(t *testing.T) {
	cfg := &Config{LLMs: map[string]Target{"c": {}, "a": {}, "b": {}}}

	got := strings.Join(cfg.Names(), ",")
	if got != "a,b,c" {
		t.Errorf("Names() = %q, want a,b,c", got)
	}
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
