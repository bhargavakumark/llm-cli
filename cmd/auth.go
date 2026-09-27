package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/bhargavakumark/go-cli-lib/mask"
	"github.com/bhargavakumark/llm-cli/pkg/api"
	"github.com/bhargavakumark/llm-cli/pkg/config"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage LLM targets and their credentials",
	}
	cmd.AddCommand(newAuthSetupCmd())
	cmd.AddCommand(newAuthShowCmd())
	cmd.AddCommand(newAuthDefaultCmd())
	return cmd
}

type setupFlags struct {
	llm          string
	aliases      []string
	baseURL      string
	model        string
	key          string
	keyEnv       string
	verify       bool
	includeUsage bool
}

func newAuthSetupCmd() *cobra.Command {
	var f setupFlags

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Create or update an LLM target",
		Long: `Create or update a named target in ~/.config/llm-cli/config.json.

With any flag set the command is non-interactive. With no flags it prompts
for the base URL, model, and credentials, keeping the current value when
you press ENTER.

Credentials are a literal key (--key) or the name of an environment
variable holding the key (--key-env), never both. An empty string clears
the field it names:

    llm-cli auth setup --llm deepseek --key ''
    llm-cli auth setup --llm deepseek --key-env ''

--include-usage asks the endpoint to report token accounting during a
streamed reply. It is off by default, so enable it only for endpoints that
accept the stream_options field:

    llm-cli auth setup --llm ds --include-usage
    llm-cli auth setup --llm ds --include-usage=false`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAuthSetup(cmd, f)
		},
	}

	fl := cmd.Flags()
	fl.StringVar(&f.llm, "llm", "", "Target name (required)")
	fl.StringArrayVar(&f.aliases, "alias", nil, "Alias for the target; replaces the existing alias list")
	fl.StringVar(&f.baseURL, "base-url", "", "OpenAI-compatible base URL")
	fl.StringVar(&f.model, "model", "", "Model id")
	fl.StringVar(&f.key, "key", "", "Literal API key; an empty string clears it")
	fl.StringVar(&f.keyEnv, "key-env", "", "Env var holding the API key; an empty string clears it")
	fl.BoolVar(&f.includeUsage, "include-usage", false, "Ask the endpoint for token usage during a streamed reply")
	fl.BoolVar(&f.verify, "verify", false, "Call GET /models before saving")

	return cmd
}

func runAuthSetup(cmd *cobra.Command, f setupFlags) error {
	if strings.TrimSpace(f.llm) == "" {
		return errors.New("--llm is required")
	}

	cfg, err := config.Load()
	switch {
	case err == nil:
	case errors.Is(err, config.ErrNoConfig):
		cfg = &config.Config{LLMs: map[string]config.Target{}}
	default:
		return err
	}

	target := cfg.LLMs[f.llm]
	fl := cmd.Flags()
	nonInteractive := fl.Changed("alias") || fl.Changed("base-url") || fl.Changed("model") ||
		fl.Changed("key") || fl.Changed("key-env") || fl.Changed("include-usage")

	if nonInteractive {
		if err := applySetupFlags(fl, f, &target); err != nil {
			return err
		}
	} else {
		if err := promptForTarget(cmd, &target); err != nil {
			return err
		}
	}

	if strings.TrimSpace(target.BaseURL) == "" {
		return errors.New("base_url is required: pass --base-url")
	}
	if strings.TrimSpace(target.Model) == "" {
		return errors.New("model is required: pass --model")
	}

	cfg.LLMs[f.llm] = target
	if err := cfg.Validate(); err != nil {
		return err
	}

	if f.verify {
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		if err := verifyTarget(ctx, target); err != nil {
			return fmt.Errorf("verify failed, nothing saved: %w", err)
		}
	}

	firstTarget := cfg.DefaultLLM == ""
	if firstTarget {
		cfg.DefaultLLM = f.llm
	}

	if err := config.Save(cfg); err != nil {
		return err
	}

	infof("Saved target %q to %s", f.llm, config.ConfigPath())
	if firstTarget {
		infof("Set %q as the default llm; change it with 'llm-cli auth default --llm <name>'", f.llm)
	}
	return nil
}

// applySetupFlags copies only the flags the user actually passed, so that a
// re-run updates the fields it names and leaves the rest alone.
func applySetupFlags(fl *pflag.FlagSet, f setupFlags, target *config.Target) error {
	if fl.Changed("base-url") {
		target.BaseURL = f.baseURL
	}
	if fl.Changed("model") {
		target.Model = f.model
	}
	if fl.Changed("alias") {
		target.Aliases = f.aliases
	}
	if fl.Changed("include-usage") {
		target.IncludeUsage = f.includeUsage
	}

	keyGiven := fl.Changed("key") && strings.TrimSpace(f.key) != ""
	keyEnvGiven := fl.Changed("key-env") && strings.TrimSpace(f.keyEnv) != ""
	if keyGiven && keyEnvGiven {
		return errors.New("pass either --key or --key-env, not both")
	}

	if fl.Changed("key") {
		target.APIKey = f.key
		if f.key != "" {
			target.APIKeyEnv = ""
		}
	}
	if fl.Changed("key-env") {
		target.APIKeyEnv = f.keyEnv
		if f.keyEnv != "" {
			target.APIKey = ""
		}
	}
	return nil
}

func promptForTarget(cmd *cobra.Command, target *config.Target) error {
	scanner := bufio.NewScanner(cmd.InOrStdin())
	out := cmd.ErrOrStderr()

	target.BaseURL = ask(out, scanner, "Base URL", target.BaseURL)
	target.Model = ask(out, scanner, "Model", target.Model)
	target.APIKey = ask(out, scanner, "API key (literal, blank to skip)", mask.Token(target.APIKey))
	target.APIKeyEnv = ask(out, scanner, "API key env var name (blank to skip)", target.APIKeyEnv)
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read input: %w", err)
	}

	includeUsage, err := askBool(out, scanner, "Include token usage in streamed replies (true/false)", target.IncludeUsage)
	if err != nil {
		return err
	}
	target.IncludeUsage = includeUsage

	if target.APIKey != "" && target.APIKeyEnv != "" {
		return errors.New("set either a literal API key or an env var name, not both")
	}
	return nil
}

// ask prompts on out and returns the current value when the answer is blank,
// so that ENTER keeps what is already there.
func ask(out io.Writer, scanner *bufio.Scanner, label, current string) string {
	if current != "" {
		fmt.Fprintf(out, "%s [%s, press ENTER to keep]: ", label, current)
	} else {
		fmt.Fprintf(out, "%s: ", label)
	}

	if !scanner.Scan() {
		fmt.Fprintln(out)
		return current
	}
	if v := strings.TrimSpace(scanner.Text()); v != "" {
		return v
	}
	return current
}

// askBool prompts on stderr and keeps the current value on a blank answer. An
// answer that is not a boolean is an error, not a silent default.
func askBool(out io.Writer, scanner *bufio.Scanner, label string, current bool) (bool, error) {
	answer := ask(out, scanner, label, strconv.FormatBool(current))

	parsed, err := strconv.ParseBool(strings.TrimSpace(answer))
	if err != nil {
		return current, fmt.Errorf("%s: %q is not true or false", label, answer)
	}
	return parsed, nil
}

func verifyTarget(parent context.Context, target config.Target) error {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()

	client, err := api.NewClient(target.BaseURL, target.APIKeyValue())
	if err != nil {
		return err
	}
	client.LogRequests = logRequests
	client.Logger = func(dump string) { infofGrey("%s", dump) }

	ids, err := client.ListModels(ctx)
	if err != nil {
		return err
	}

	infofGrey("verified %s: %d models, including %s", target.BaseURL, len(ids), sampleModels(ids))
	return nil
}

func sampleModels(ids []string) string {
	if len(ids) == 0 {
		return "none"
	}
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	if len(sorted) > 3 {
		sorted = append(sorted[:3], "...")
	}
	return strings.Join(sorted, ", ")
}

func newAuthShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show configured targets and where their keys come from",
		Long: `Show targets from the config file.

Literal keys are masked. Env var names are printed in full, together with
whether the variable is currently set, because an env var that is missing
at call time sends the request without an Authorization header.`,
		Args: cobra.NoArgs,
		RunE: runAuthShow,
	}
	return cmd
}

func runAuthShow(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if llm != "" {
		name, target, err := cfg.Resolve(llm)
		if err != nil {
			return err
		}
		return showOneTarget(cmd.OutOrStdout(), name, target, name == cfg.DefaultLLM)
	}

	if len(cfg.LLMs) == 0 {
		return errors.New("no targets configured: run 'llm-cli auth setup'")
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tDEFAULT\tALIASES\tBASE_URL\tMODEL\tKEY\tKEY_ENV\tUSAGE")

	for _, name := range cfg.Names() {
		target := cfg.LLMs[name]
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			name,
			defaultMark(name == cfg.DefaultLLM),
			dash(strings.Join(target.Aliases, ",")),
			target.BaseURL,
			target.Model,
			dash(mask.Token(target.APIKey)),
			keyEnvColumn(target.APIKeyEnv),
			yesNo(target.IncludeUsage),
		)
	}
	if err := w.Flush(); err != nil {
		return err
	}

	for _, name := range cfg.Names() {
		if !cfg.LLMs[name].HasCredential() {
			infofGrey("warn: %s has no api_key or api_key_env, requests go out unauthenticated", name)
		}
		if env := cfg.LLMs[name].APIKeyEnv; env != "" {
			if _, ok := os.LookupEnv(env); !ok {
				infofGrey("warn: %s is not set, so %s sends no Authorization header", env, name)
			}
		}
	}
	return nil
}

func showOneTarget(w io.Writer, name string, target config.Target, isDefault bool) error {
	fw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	rows := [][2]string{
		{"NAME", name},
		{"DEFAULT", defaultMark(isDefault)},
		{"ALIASES", dash(strings.Join(target.Aliases, ","))},
		{"BASE_URL", target.BaseURL},
		{"MODEL", target.Model},
		{"API_KEY", dash(mask.Token(target.APIKey))},
		{"API_KEY_ENV", keyEnvColumn(target.APIKeyEnv)},
		{"INCLUDE_USAGE", strconv.FormatBool(target.IncludeUsage)},
	}
	for _, row := range rows {
		fmt.Fprintf(fw, "%s\t%s\n", row[0], row[1])
	}
	return fw.Flush()
}

func defaultMark(isDefault bool) string {
	if isDefault {
		return "*"
	}
	return "-"
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func keyEnvColumn(env string) string {
	if env == "" {
		return "-"
	}
	if _, ok := os.LookupEnv(env); ok {
		return env + " (set)"
	}
	return env + " (unset)"
}

func newAuthDefaultCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "default",
		Short: "Set the default LLM target",
		Long:  `Set the target used when --llm is not given. Accepts a name or an alias.`,
		Args:  cobra.NoArgs,
		RunE:  runAuthDefault,
	}
	return cmd
}

func runAuthDefault(cmd *cobra.Command, _ []string) error {
	if strings.TrimSpace(llm) == "" {
		return errors.New("--llm is required")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	name, _, err := cfg.Resolve(llm)
	if err != nil {
		return err
	}

	previous := cfg.DefaultLLM
	cfg.DefaultLLM = name

	if err := config.Save(cfg); err != nil {
		return err
	}

	from := previous
	if from == "" {
		from = "none"
	}
	infof("Default llm: %s -> %s", from, name)
	return nil
}
