package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/bhargavakumark/llm-cli/pkg/api"
	"github.com/bhargavakumark/llm-cli/pkg/config"
	"github.com/spf13/cobra"
)

func newModelsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "models",
		Short: "List the model ids the selected target advertises",
		Long: `Ask the selected target for its model list and print the ids.

This asks the target what it advertises, which is the quickest way to check a
model id without reading vendor docs. The list is not a complete contract:
endpoints are known to accept ids they do not advertise, so an id that is
missing here may still work. One id per line goes to stdout, in the order the
endpoint returned them, so the output pipes:

    llm-cli models --llm ds | grep flash

The target's credential is used when it has one. Nothing is written to the
config file.`,
		Args: cobra.NoArgs,
		RunE: runModels,
	}

	return cmd
}

func runModels(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	name, target, err := cfg.Resolve(llm)
	if err != nil {
		return err
	}

	client, err := api.NewClient(target.BaseURL, target.APIKeyValue())
	if err != nil {
		return err
	}
	client.LogRequests = logRequests
	client.Logger = func(dump string) { infofGrey("%s", dump) }

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ids, err := client.ListModels(ctx)
	if err != nil {
		return cancelled(ctx, err)
	}

	out := cmd.OutOrStdout()
	for _, id := range ids {
		if _, err := fmt.Fprintln(out, id); err != nil {
			return err
		}
	}

	infofGrey("%s -> %s (%d models)", name, target.BaseURL, len(ids))
	return nil
}
