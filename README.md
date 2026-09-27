# llm-cli - talk to OpenAI-compatible LLM endpoints from the terminal

`llm-cli` is a thin CLI over any OpenAI-compatible chat completions
endpoint. Endpoints are configured once as named targets, and every command
picks one with `--llm`.

The response text is the only thing written to stdout, so the tool composes:

```bash
llm-cli chat --llm ds "summarise this file" > answer.md
```

Progress messages, prompts, warnings and errors all go to stderr.

## Quick Start

```bash
go build -o llm-cli .

# DeepSeek, key read from an environment variable
./llm-cli auth setup --llm deepseek --alias ds \
  --base-url https://api.deepseek.com --model deepseek-flash \
  --key-env DEEPSEEK_API_KEY

# A local server that needs no credentials
./llm-cli auth setup --llm local --alias l \
  --base-url http://127.0.0.1:6203/v1 --model qwen3.6-35b-a3b-Q4_K_M.gguf

./llm-cli auth show
```

## Installation

```bash
git clone git@github.com:bhargavakumark/llm-cli.git
cd llm-cli
go build -o llm-cli .
cp llm-cli /usr/local/bin/    # or anywhere on PATH
```

Requires Go 1.27.0 or later.

## Authentication

Configuration lives in `~/.config/llm-cli/config.json`, written `0600`
inside a `0700` directory.

### `auth setup`

Creates or updates one target. With any flag set the command is
non-interactive; with no flags it prompts for the base URL, model and
credentials, keeping the current value when you press ENTER.

| Flag | Meaning |
|---|---|
| `--llm` | Target name (required) |
| `--alias` | Alias for the target; repeatable, and replaces the existing alias list |
| `--base-url` | OpenAI-compatible base URL, stored exactly as given |
| `--model` | Model id |
| `--key` | Literal API key; an empty string clears it |
| `--key-env` | Name of an environment variable holding the key; an empty string clears it |
| `--verify` | Call `GET /models` before saving; on failure nothing is written |

Credentials are a literal key or an env var name, never both. Passing both
is an error. An empty value clears the field it names:

```bash
llm-cli auth setup --llm deepseek --key ''        # drop the literal key
llm-cli auth setup --llm deepseek --key-env ''    # drop the env var reference
```

The base URL is never rewritten, so no `/v1` is appended. If an endpoint
rejects the path, the error shows the full URL that was used.

### `auth show`

Lists every target with its aliases, the default marker, base URL, model,
the literal key masked, and the env var name together with whether that
variable is currently set. Two conditions are called out as warnings,
because both result in a request with no `Authorization` header:

- a target with neither `api_key` nor `api_key_env`
- a target whose `api_key_env` variable is not set

`auth show --llm <name>` prints one target in detail.

### `auth default`

```bash
llm-cli auth default --llm ds    # accepts a name or an alias
```

Sets the target used when `--llm` is omitted. The first target created by
`auth setup` becomes the default automatically.

## Commands

```bash
llm-cli chat --llm ds "explain this error"
llm-cli auth show
llm-cli auth default --llm luna
```

Global flags:

| Flag | Meaning |
|---|---|
| `--llm` | Target name or alias; falls back to the configured default |
| `--quiet`, `-q` | Suppress progress messages on stderr |

## Shell Completion

`--llm` completes target names and aliases, with the model and endpoint shown
as the description and the default target marked. Pressing TAB on a bare
`--llm` also works for the commands added later, since the flag is persistent
to the root command.

```bash
# bash
source <(llm-cli completion bash)
# or, to load it in every shell
echo 'source <(llm-cli completion bash)' >> ~/.bashrc

# zsh
llm-cli completion zsh > "${fpath[1]}/_llm-cli"

# fish
llm-cli completion fish > ~/.config/fish/completions/llm-cli.fish
```

If the config file is missing or fails validation, completion returns the error
directive: suggestions stop rather than an error message being printed into
the shell, and the reason surfaces the next time a command actually runs.

## Configuration

```json
{
  "default_llm": "deepseek",
  "llms": {
    "deepseek": {
      "aliases": ["ds"],
      "base_url": "https://api.deepseek.com",
      "model": "deepseek-flash",
      "api_key": "",
      "api_key_env": "DEEPSEEK_API_KEY"
    },
    "local": {
      "aliases": ["l"],
      "base_url": "http://127.0.0.1:6203/v1",
      "model": "qwen3.6-35b-a3b-Q4_K_M.gguf",
      "api_key": "",
      "api_key_env": ""
    }
  }
}
```

Aliases must be unique across every target name and alias. A collision found
while loading the file is a hard error naming both targets, so resolution
never has to pick a winner. An exact target name wins over an alias.

There are no credential environment variables of the form `LLM_*`. The only
env indirection is a target's own `api_key_env`.

## Development

```bash
go build -o llm-cli .
go vet ./...
gofmt -l .
```

Layout follows the conventions in
[`go-cli-lib/CLI-GUIDELINES.md`](https://github.com/bhargavakumark/go-cli-lib):

```
main.go              thin entry point
cmd/                 cobra commands (root, auth, completion)
pkg/config/          config load, save, validation, target resolution
pkg/api/             low-level OpenAI-compatible client
```
