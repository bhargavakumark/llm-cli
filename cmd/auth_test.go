package cmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/bhargavakumark/llm-cli/pkg/config"
)

// loadConfig reads the config the command just wrote.
func loadConfig(t *testing.T) *config.Config {
	t.Helper()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return cfg
}

func TestAuthSetupWritesATarget(t *testing.T) {
	useTempHome(t)

	_, _, err := runRoot(t, "",
		"auth", "setup",
		"--llm", "deepseek",
		"--alias", "ds",
		"--base-url", "https://api.deepseek.com",
		"--model", "deepseek-flash",
		"--key-env", "DEEPSEEK_API_KEY",
		"--include-usage",
	)
	if err != nil {
		t.Fatalf("auth setup error = %v", err)
	}

	cfg := loadConfig(t)
	target, ok := cfg.LLMs["deepseek"]
	if !ok {
		t.Fatal("target deepseek was not written")
	}
	if target.BaseURL != "https://api.deepseek.com" {
		t.Errorf("BaseURL = %q, want it stored as given", target.BaseURL)
	}
	if target.Model != "deepseek-flash" {
		t.Errorf("Model = %q, want deepseek-flash", target.Model)
	}
	if target.APIKeyEnv != "DEEPSEEK_API_KEY" {
		t.Errorf("APIKeyEnv = %q, want DEEPSEEK_API_KEY", target.APIKeyEnv)
	}
	if target.APIKey != "" {
		t.Errorf("APIKey = %q, want it left empty when --key-env is used", target.APIKey)
	}
	if !target.IncludeUsage {
		t.Error("IncludeUsage = false, want true from --include-usage")
	}
	if len(target.Aliases) != 1 || target.Aliases[0] != "ds" {
		t.Errorf("Aliases = %v, want [ds]", target.Aliases)
	}
	if cfg.DefaultLLM != "deepseek" {
		t.Errorf("DefaultLLM = %q, want the first target to become the default", cfg.DefaultLLM)
	}
}

func TestAuthSetupWithALiteralKey(t *testing.T) {
	useTempHome(t)

	_, _, err := runRoot(t, "",
		"auth", "setup",
		"--llm", "luna",
		"--base-url", "https://gateway.example.com",
		"--model", "gpt-6-luna",
		"--key", "sk-test-abcdefghijklmnop",
	)
	if err != nil {
		t.Fatalf("auth setup error = %v", err)
	}

	target := loadConfig(t).LLMs["luna"]
	if target.APIKey != "sk-test-abcdefghijklmnop" {
		t.Errorf("APIKey = %q, want the literal key", target.APIKey)
	}
	if target.APIKeyEnv != "" {
		t.Errorf("APIKeyEnv = %q, want it empty when --key is used", target.APIKeyEnv)
	}
}

func TestAuthSetupClearsCredentialFields(t *testing.T) {
	useTempHome(t)

	if _, _, err := runRoot(t, "", "auth", "setup", "--llm", "t",
		"--base-url", "http://a/v1", "--model", "m", "--key", "sk-test-abcdefghijklmnop"); err != nil {
		t.Fatalf("first setup error = %v", err)
	}

	if _, _, err := runRoot(t, "", "auth", "setup", "--llm", "t", "--key", ""); err != nil {
		t.Fatalf("clearing setup error = %v", err)
	}
	if got := loadConfig(t).LLMs["t"].APIKey; got != "" {
		t.Errorf("APIKey = %q, want an empty string to clear it", got)
	}

	if _, _, err := runRoot(t, "", "auth", "setup", "--llm", "t", "--key-env", "SOME_VAR"); err != nil {
		t.Fatalf("env setup error = %v", err)
	}
	if got := loadConfig(t).LLMs["t"].APIKeyEnv; got != "SOME_VAR" {
		t.Errorf("APIKeyEnv = %q, want SOME_VAR", got)
	}

	if _, _, err := runRoot(t, "", "auth", "setup", "--llm", "t", "--key-env", ""); err != nil {
		t.Fatalf("clearing env error = %v", err)
	}
	if got := loadConfig(t).LLMs["t"].APIKeyEnv; got != "" {
		t.Errorf("APIKeyEnv = %q, want an empty string to clear it", got)
	}
}

func TestAuthSetupRejectsBothCredentialFlags(t *testing.T) {
	useTempHome(t)

	_, _, err := runRoot(t, "", "auth", "setup", "--llm", "t",
		"--base-url", "http://a/v1", "--model", "m", "--key", "sk-abc", "--key-env", "VAR")

	if err == nil || !strings.Contains(err.Error(), "not both") {
		t.Fatalf("auth setup error = %v, want a rejection of both flags", err)
	}
}

func TestAuthSetupRequiredFlags(t *testing.T) {
	useTempHome(t)

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "no llm",
			args:    []string{"auth", "setup", "--base-url", "http://a/v1", "--model", "m"},
			wantErr: "--llm is required",
		},
		{
			name:    "no base url",
			args:    []string{"auth", "setup", "--llm", "t", "--model", "m"},
			wantErr: "base_url is required",
		},
		{
			name:    "no model",
			args:    []string{"auth", "setup", "--llm", "t", "--base-url", "http://a/v1"},
			wantErr: "model is required",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := runRoot(t, "", test.args...)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("auth setup error = %v, want it to contain %q", err, test.wantErr)
			}
		})
	}
}

func TestAuthSetupRejectsAnAliasCollision(t *testing.T) {
	useTempHome(t)

	if _, _, err := runRoot(t, "", "auth", "setup", "--llm", "deepseek",
		"--alias", "ds", "--base-url", "http://a/v1", "--model", "m"); err != nil {
		t.Fatalf("first setup error = %v", err)
	}
	before, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	_, _, err = runRoot(t, "", "auth", "setup", "--llm", "other",
		"--alias", "ds", "--base-url", "http://b/v1", "--model", "m")
	if err == nil || !strings.Contains(err.Error(), "collides") {
		t.Fatalf("auth setup error = %v, want an alias collision", err)
	}

	after := loadConfig(t)
	if _, ok := after.LLMs["other"]; ok {
		t.Error("the colliding target was written despite the error")
	}
	if len(after.LLMs) != len(before.LLMs) {
		t.Errorf("target count changed from %d to %d", len(before.LLMs), len(after.LLMs))
	}
}

func TestAuthSetupReplacesAliases(t *testing.T) {
	useTempHome(t)

	if _, _, err := runRoot(t, "", "auth", "setup", "--llm", "t",
		"--alias", "one", "--alias", "two", "--base-url", "http://a/v1", "--model", "m"); err != nil {
		t.Fatalf("first setup error = %v", err)
	}
	if _, _, err := runRoot(t, "", "auth", "setup", "--llm", "t", "--alias", "three"); err != nil {
		t.Fatalf("second setup error = %v", err)
	}

	got := loadConfig(t).LLMs["t"].Aliases
	if len(got) != 1 || got[0] != "three" {
		t.Errorf("Aliases = %v, want [three] because --alias replaces the list", got)
	}
}

func TestAuthSetupKeepsFieldsItWasNotGiven(t *testing.T) {
	useTempHome(t)

	if _, _, err := runRoot(t, "", "auth", "setup", "--llm", "t",
		"--base-url", "http://a/v1", "--model", "m1", "--key", "sk-test-abcdefghijklmnop"); err != nil {
		t.Fatalf("first setup error = %v", err)
	}
	if _, _, err := runRoot(t, "", "auth", "setup", "--llm", "t", "--model", "m2"); err != nil {
		t.Fatalf("second setup error = %v", err)
	}

	target := loadConfig(t).LLMs["t"]
	if target.Model != "m2" {
		t.Errorf("Model = %q, want m2", target.Model)
	}
	if target.BaseURL != "http://a/v1" {
		t.Errorf("BaseURL = %q, want it preserved", target.BaseURL)
	}
	if target.APIKey != "sk-test-abcdefghijklmnop" {
		t.Errorf("APIKey = %q, want it preserved", target.APIKey)
	}
}

func TestAuthSetupTurnsUsageOff(t *testing.T) {
	useTempHome(t)

	if _, _, err := runRoot(t, "", "auth", "setup", "--llm", "t",
		"--base-url", "http://a/v1", "--model", "m", "--include-usage"); err != nil {
		t.Fatalf("first setup error = %v", err)
	}
	if _, _, err := runRoot(t, "", "auth", "setup", "--llm", "t", "--include-usage=false"); err != nil {
		t.Fatalf("second setup error = %v", err)
	}

	if loadConfig(t).LLMs["t"].IncludeUsage {
		t.Error("IncludeUsage = true, want false after --include-usage=false")
	}
}

func TestAuthSetupBindsAnInterface(t *testing.T) {
	useTempHome(t)

	_, _, err := runRoot(t, "", "auth", "setup", "--llm", "deepseek",
		"--base-url", "https://api.deepseek.com", "--model", "deepseek-flash",
		"--bind-interface", "en0")
	if err != nil {
		t.Fatalf("auth setup error = %v", err)
	}
	if got := loadConfig(t).LLMs["deepseek"].BindInterface; got != "en0" {
		t.Errorf("BindInterface = %q, want en0", got)
	}

	if _, _, err := runRoot(t, "", "auth", "setup", "--llm", "deepseek", "--bind-interface", ""); err != nil {
		t.Fatalf("clearing setup error = %v", err)
	}
	if got := loadConfig(t).LLMs["deepseek"].BindInterface; got != "" {
		t.Errorf("BindInterface = %q, want an empty string to clear it", got)
	}
}

func TestAuthSetupRejectsAnUnknownInterface(t *testing.T) {
	useTempHome(t)

	_, _, err := runRoot(t, "", "auth", "setup", "--llm", "deepseek",
		"--base-url", "https://api.deepseek.com", "--model", "m",
		"--bind-interface", "no-such-interface-xyz")

	if err == nil || !strings.Contains(err.Error(), "no-such-interface-xyz") {
		t.Fatalf("auth setup error = %v, want it to name the interface", err)
	}
	// Nothing is written at all, so the config file must not exist yet.
	if _, err := config.Load(); !errors.Is(err, config.ErrNoConfig) {
		t.Fatalf("Load() error = %v, want no config file written", err)
	}
}

func TestAuthShowShowsTheBindInterface(t *testing.T) {
	useTempHome(t)

	if _, _, err := runRoot(t, "", "auth", "setup", "--llm", "deepseek", "--alias", "ds",
		"--base-url", "https://api.deepseek.com", "--model", "m", "--bind-interface", "en0"); err != nil {
		t.Fatalf("setup error = %v", err)
	}
	if _, _, err := runRoot(t, "", "auth", "setup", "--llm", "luna",
		"--base-url", "https://gateway.example.com", "--model", "gpt-6-luna"); err != nil {
		t.Fatalf("setup error = %v", err)
	}

	stdout, _, err := runRoot(t, "", "auth", "show")
	if err != nil {
		t.Fatalf("auth show error = %v", err)
	}
	if !strings.Contains(stdout, "BIND") || !strings.Contains(stdout, "en0") {
		t.Errorf("the table should have a BIND column showing en0:\n%s", stdout)
	}

	stdout, _, err = runRoot(t, "", "auth", "show", "--llm", "ds")
	if err != nil {
		t.Fatalf("auth show error = %v", err)
	}
	if !strings.Contains(stdout, "BIND_INTERFACE") || !strings.Contains(stdout, "en0") {
		t.Errorf("the single-target view should show the bind interface:\n%s", stdout)
	}

	// A target without a bind shows a dash rather than a default.
	stdout, _, err = runRoot(t, "", "auth", "show", "--llm", "luna")
	if err != nil {
		t.Fatalf("auth show error = %v", err)
	}
	if !strings.Contains(stdout, "BIND_INTERFACE  -") {
		t.Errorf("an unbound target should show a dash:\n%s", stdout)
	}
}

func TestAuthSetupInteractively(t *testing.T) {
	useTempHome(t)

	// Base URL, model, literal key, env var name, include usage.
	stdin := "http://a/v1\nm\n\n\ntrue\n"
	_, stderr, err := runRoot(t, stdin, "auth", "setup", "--llm", "t")
	if err != nil {
		t.Fatalf("auth setup error = %v", err)
	}
	if !strings.Contains(stderr, "Base URL") {
		t.Errorf("stderr %q should contain the prompts", stderr)
	}

	target := loadConfig(t).LLMs["t"]
	if target.BaseURL != "http://a/v1" || target.Model != "m" {
		t.Errorf("target = %+v, want the prompted values", target)
	}
	if !target.IncludeUsage {
		t.Error("IncludeUsage = false, want true from the interactive answer")
	}
}

func TestAuthSetupInteractiveRejectsANonBoolean(t *testing.T) {
	useTempHome(t)

	stdin := "http://a/v1\nm\n\n\nmaybe\n"
	_, _, err := runRoot(t, stdin, "auth", "setup", "--llm", "t")

	if err == nil || !strings.Contains(err.Error(), "is not true or false") {
		t.Fatalf("auth setup error = %v, want a rejection of the bad answer", err)
	}
}

func TestAuthDefault(t *testing.T) {
	useTempHome(t)

	for _, name := range []string{"deepseek", "gpt6"} {
		if _, _, err := runRoot(t, "", "auth", "setup", "--llm", name,
			"--base-url", "http://a/v1", "--model", "m"); err != nil {
			t.Fatalf("setup %s error = %v", name, err)
		}
	}
	if _, _, err := runRoot(t, "", "auth", "setup", "--llm", "deepseek", "--alias", "ds"); err != nil {
		t.Fatalf("alias setup error = %v", err)
	}

	if _, _, err := runRoot(t, "", "auth", "default", "--llm", "ds"); err != nil {
		t.Fatalf("auth default error = %v", err)
	}
	if got := loadConfig(t).DefaultLLM; got != "deepseek" {
		t.Errorf("DefaultLLM = %q, want deepseek resolved from the alias", got)
	}
}

func TestAuthDefaultErrors(t *testing.T) {
	useTempHome(t)

	if _, _, err := runRoot(t, "", "auth", "default"); err == nil ||
		!strings.Contains(err.Error(), "--llm is required") {
		t.Fatalf("auth default error = %v, want a required-flag failure", err)
	}

	if _, _, err := runRoot(t, "", "auth", "setup", "--llm", "t",
		"--base-url", "http://a/v1", "--model", "m"); err != nil {
		t.Fatalf("setup error = %v", err)
	}
	if _, _, err := runRoot(t, "", "auth", "default", "--llm", "ghost"); err == nil ||
		!strings.Contains(err.Error(), "unknown llm") {
		t.Fatalf("auth default error = %v, want an unknown-target failure", err)
	}
}

func TestAuthShowListsTargets(t *testing.T) {
	useTempHome(t)

	if _, _, err := runRoot(t, "", "auth", "setup", "--llm", "deepseek",
		"--alias", "ds", "--base-url", "https://api.deepseek.com", "--model", "deepseek-flash",
		"--key", "sk-secret-abcdefghijklmnop"); err != nil {
		t.Fatalf("setup error = %v", err)
	}
	if _, _, err := runRoot(t, "", "auth", "setup", "--llm", "local",
		"--base-url", "http://127.0.0.1:6203/v1", "--model", "qwen3", "--include-usage"); err != nil {
		t.Fatalf("setup error = %v", err)
	}

	stdout, _, err := runRoot(t, "", "auth", "show")
	if err != nil {
		t.Fatalf("auth show error = %v", err)
	}

	for _, want := range []string{"NAME", "DEFAULT", "ALIASES", "USAGE", "deepseek", "ds", "deepseek-flash", "local"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output should contain %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "sk-secret-abcdefghijklmnop") {
		t.Errorf("the literal key must be masked:\n%s", stdout)
	}
	if !strings.Contains(stdout, "sk-s") && !strings.Contains(stdout, "...") {
		t.Errorf("the masked key should still be recognisable:\n%s", stdout)
	}
}

func TestAuthShowWarnsAboutMissingCredentials(t *testing.T) {
	useTempHome(t)

	if _, _, err := runRoot(t, "", "auth", "setup", "--llm", "local",
		"--base-url", "http://127.0.0.1:6203/v1", "--model", "qwen3"); err != nil {
		t.Fatalf("setup error = %v", err)
	}

	// Warnings go to os.Stderr, so capture that. quiet must be false, which
	// means setting it after the command has been built.
	var runErr error
	_, stderr := captureStdio(t, func() {
		cmd := newRootCmd()
		cmd.SetOut(&strings.Builder{})
		cmd.SetErr(&strings.Builder{})
		cmd.SetArgs([]string{"auth", "show"})

		quiet = false
		runErr = cmd.Execute()
	})
	if runErr != nil {
		t.Fatalf("auth show error = %v", runErr)
	}
	if !strings.Contains(stderr, "no api_key or api_key_env") {
		t.Errorf("stderr %q should warn about a target with no credentials", stderr)
	}
}

func TestAuthShowOneTarget(t *testing.T) {
	useTempHome(t)

	if _, _, err := runRoot(t, "", "auth", "setup", "--llm", "deepseek",
		"--alias", "ds", "--base-url", "https://api.deepseek.com", "--model", "deepseek-flash",
		"--key-env", "DEEPSEEK_API_KEY", "--include-usage"); err != nil {
		t.Fatalf("setup error = %v", err)
	}

	stdout, _, err := runRoot(t, "", "auth", "show", "--llm", "ds")
	if err != nil {
		t.Fatalf("auth show error = %v", err)
	}
	for _, want := range []string{"deepseek", "DEEPSEEK_API_KEY", "INCLUDE_USAGE", "true"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output should contain %q:\n%s", want, stdout)
		}
	}
}

func TestAuthShowErrors(t *testing.T) {
	useTempHome(t)

	t.Run("no config file", func(t *testing.T) {
		if _, _, err := runRoot(t, "", "auth", "show"); err == nil ||
			!strings.Contains(err.Error(), "no config") {
			t.Fatalf("auth show error = %v, want a missing-config failure", err)
		}
	})

	t.Run("config with no targets", func(t *testing.T) {
		saveConfig(t, &config.Config{LLMs: map[string]config.Target{}})

		if _, _, err := runRoot(t, "", "auth", "show"); err == nil ||
			!strings.Contains(err.Error(), "no targets configured") {
			t.Fatalf("auth show error = %v, want a no-targets failure", err)
		}
	})

	if _, _, err := runRoot(t, "", "auth", "setup", "--llm", "t",
		"--base-url", "http://a/v1", "--model", "m"); err != nil {
		t.Fatalf("setup error = %v", err)
	}
	if _, _, err := runRoot(t, "", "auth", "show", "--llm", "ghost"); err == nil ||
		!strings.Contains(err.Error(), "unknown llm") {
		t.Fatalf("auth show error = %v, want an unknown-target failure", err)
	}
}
