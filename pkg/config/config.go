// Package config loads and saves named LLM targets from
// ~/.config/llm-cli/config.json.
//
// A target is an OpenAI-compatible endpoint: a base URL, a model id, and
// credentials. Credentials are either a literal API key or the name of an
// environment variable holding that key, never both. Both fields may be
// empty, which means the endpoint is called without an Authorization
// header; that is legitimate for local servers and is reported by
// `llm-cli auth show` rather than enforced here.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ErrNoConfig is returned by Load when no config file exists yet.
var ErrNoConfig = errors.New("no config")

// Target is one named OpenAI-compatible endpoint.
type Target struct {
	Aliases   []string `json:"aliases,omitempty"`
	BaseURL   string   `json:"base_url"`
	Model     string   `json:"model"`
	APIKey    string   `json:"api_key"`
	APIKeyEnv string   `json:"api_key_env"`

	// IncludeUsage asks the endpoint to report token accounting during a
	// streamed reply. It is off by default, because an endpoint that rejects
	// the stream_options field would fail every request.
	IncludeUsage bool `json:"include_usage,omitempty"`
}

// Config is the on-disk config file.
type Config struct {
	DefaultLLM string            `json:"default_llm"`
	LLMs       map[string]Target `json:"llms"`
}

// ConfigPath returns the config file path, or "" if the home directory
// cannot be determined. Callers turn "" into an error.
func ConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "llm-cli", "config.json")
}

// Load reads and validates the config file. A missing file returns an
// error wrapping ErrNoConfig.
func Load() (*Config, error) {
	path := ConfigPath()
	if path == "" {
		return nil, errors.New("cannot determine home directory for the config path")
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w at %s: run 'llm-cli auth setup'", ErrNoConfig, path)
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg.LLMs == nil {
		cfg.LLMs = map[string]Target{}
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &cfg, nil
}

// Save writes the config file with owner-only permissions.
func Save(cfg *Config) error {
	path := ConfigPath()
	if path == "" {
		return errors.New("cannot determine home directory for the config path")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// Names returns the target names in sorted order.
func (c *Config) Names() []string {
	names := make([]string, 0, len(c.LLMs))
	for name := range c.LLMs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Validate rejects a config whose target names or aliases are ambiguous.
// Aliases must be unique across every name and alias, so that resolution
// never has to pick a winner.
func (c *Config) Validate() error {
	owner := make(map[string]string, len(c.LLMs))

	for _, name := range c.Names() {
		switch {
		case strings.TrimSpace(name) == "":
			return errors.New("config has a target with a blank name")
		case strings.ContainsAny(name, " \t"):
			return fmt.Errorf("target name %q contains whitespace", name)
		}
		owner[name] = name
	}

	for _, name := range c.Names() {
		for _, alias := range c.LLMs[name].Aliases {
			if strings.TrimSpace(alias) == "" {
				return fmt.Errorf("target %q has a blank alias", name)
			}
			if strings.ContainsAny(alias, " \t") {
				return fmt.Errorf("alias %q on target %q contains whitespace", alias, name)
			}
			if existing, ok := owner[alias]; ok {
				switch {
				case alias == name:
					return fmt.Errorf("alias %q on target %q is the target's own name", alias, name)
				case existing == name:
					return fmt.Errorf("alias %q is repeated on target %q", alias, name)
				default:
					return fmt.Errorf("alias %q on target %q collides with %q", alias, name, existing)
				}
			}
			owner[alias] = name
		}
	}

	if c.DefaultLLM != "" {
		if _, ok := c.LLMs[c.DefaultLLM]; !ok {
			return fmt.Errorf("default_llm %q is not a configured target", c.DefaultLLM)
		}
	}
	return nil
}

// Resolve maps a target name or alias to its canonical name and Target.
// An empty name falls back to the configured default. An exact target name
// wins over an alias, which Validate already guarantees cannot collide.
func (c *Config) Resolve(nameOrAlias string) (string, Target, error) {
	if nameOrAlias == "" {
		nameOrAlias = c.DefaultLLM
	}
	if nameOrAlias == "" {
		return "", Target{}, errors.New(
			"no --llm given and no default set: run 'llm-cli auth default --llm <name>'")
	}

	if target, ok := c.LLMs[nameOrAlias]; ok {
		return nameOrAlias, target, nil
	}
	for _, name := range c.Names() {
		for _, alias := range c.LLMs[name].Aliases {
			if alias == nameOrAlias {
				return name, c.LLMs[name], nil
			}
		}
	}

	if len(c.LLMs) == 0 {
		return "", Target{}, fmt.Errorf("unknown llm %q: no targets configured, run 'llm-cli auth setup'", nameOrAlias)
	}
	return "", Target{}, fmt.Errorf("unknown llm %q: known targets are %s",
		nameOrAlias, strings.Join(c.Names(), ", "))
}

// APIKeyValue returns the credential to send, resolving api_key_env at call
// time. An unset variable, like an empty api_key, yields "", and the request
// goes out without an Authorization header.
func (t Target) APIKeyValue() string {
	if t.APIKeyEnv != "" {
		return os.Getenv(t.APIKeyEnv)
	}
	return t.APIKey
}

// HasCredential reports whether the target names a literal key or an env var.
func (t Target) HasCredential() bool {
	return t.APIKey != "" || t.APIKeyEnv != ""
}
