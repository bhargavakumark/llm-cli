# 2. Keep endpoints as named targets in a data file

Date: 2026-10-19
Status: accepted

## Why

The tool talks to endpoints that do not share behaviour. DeepSeek is reached
through a local bridge and takes its credential from an environment variable.
The local server on `127.0.0.1:6203` needs no credential at all. The
mesh-reachable luna gateway has to stay unbound while DeepSeek has to bind the
physical interface. Those differences decide whether a request works, so they
belong next to the destination rather than in the caller's shell. A single
endpoint compiled into the binary, or one selected by an environment variable,
would force every invocation to repeat a URL and a model and would leave
nowhere to record the per-destination settings. The same reasoning is what put
`bind_interface` and `include_usage` on the target rather than on a global flag
when those features arrived.

The configuration is a plain JSON file so that it can be read, edited, backed
up, and diffed with ordinary tools, and so that a new setting does not need a
new command to be useful. Validation runs on every load, which means a hand
edit is held to the same rules as a write from `auth setup`.

Three alternatives were rejected. Resolution does not pick a winner when a
name is ambiguous: aliases must be unique across every target name and alias,
and a collision is a hard error naming both targets. An early version reported
a repeated alias as the target's own name, and the message was corrected to
distinguish a repeat from a collision. The base URL is not normalised, so no
`/v1` is appended, because the library joins the stored URL with the endpoint
path verbatim and a rewrite would hide the URL that actually failed. And a
target's aliases are replaced wholesale by `auth setup --alias` rather than
merged, so the flag describes the full alias list and cannot accumulate stale
names.

## How

Targets live in `~/.config/llm-cli/config.json`, loaded and saved by
`pkg/config`. The file has a `default_llm` name and an `llms` map from target
name to `Target`. A `Target` holds `aliases`, `base_url`, `model`, `api_key`,
`api_key_env`, `include_usage`, and `bind_interface`. The last two are optional
and are omitted when false or empty, so a config written before they existed
still parses.

`Config.Load` reads the file, decodes it, and calls `Validate`. Validation
rejects a blank name, a name or alias containing whitespace, an alias equal to
its own target name, a repeated alias, an alias that collides with another
target's name or alias, and a `default_llm` that is not a configured target.
`Resolve` maps the `--llm` value to a canonical name and target. An empty value
falls back to `DefaultLLM`; an exact target name wins over an alias; otherwise
the sorted target names are scanned for a matching alias. An unknown value is
an error that lists the known targets.

Every command calls `config.Load` and `cfg.Resolve(llm)` before it builds a
request, so target selection happens at call time and a config edit is picked
up by the next invocation. `--llm` is a persistent flag on the root command,
which also gives the completion function in `cmd/completion.go` one place to
list names, aliases, models, endpoints, and the default marker.

`auth setup` creates or updates one target. With any flag other than `--llm`
and `--verify` set, it is non-interactive and copies only the flags that were
actually passed, so a re-run changes the fields it names and leaves the rest
alone. With no such flag it prompts for the base URL, model, credentials, and
usage setting, keeping the current value on a blank answer. The first target
created becomes the default automatically; `auth default` changes the default
later and accepts a name or an alias, storing the canonical name.

## Consequences

One binary serves every endpoint, and adding an endpoint is a config write
rather than a code change or a rebuild. The settings that decide whether a
request works, such as the credential source, the usage request, and the
interface binding, travel with the target, so two targets on different networks
can coexist without a global switch.

The costs follow from making the file the source of truth. Aliases share one
global namespace, so a short alias chosen for one target can block a name
someone later wants for another. The schema is now a public interface:
renaming a JSON field would break every existing config, so a change needs a
migration or a second field. A hand edit can still produce a config that is
valid but wrong, for example a base URL that points at the wrong port, and the
tool reports that only when a request is made. Target selection is fixed for
the life of a process, which is fine for a one-shot CLI but would need
rethinking if a long-running mode were added. And the per-invocation override
surface stays small: `--llm` selects a target, but changing the model or URL
for one call means editing the target or creating another one.
