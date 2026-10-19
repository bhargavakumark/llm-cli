# 3. Store credentials in the config as a literal key or an env var name

Date: 2026-10-19
Status: accepted

## Why

The endpoints this tool talks to do not agree on credentials. A hosted endpoint
needs a key, while a server on loopback often needs nothing. A key pasted into
the config file is convenient but leaves the secret in plain text. A key read
from an environment variable keeps the secret out of the file, but something
still has to record which variable belongs to which target. Supporting both
forms covers both cases, and the two forms are mutually exclusive so that a
request never has two possible credentials to choose from.

Four alternatives were rejected. Requiring a credential would break local
servers, so an empty credential is allowed and the request goes out without an
`Authorization` header. Treating a missing credential as an error was rejected
for the same reason; `auth show` warns instead, because a warning is useful
without inventing a failure for an endpoint that works anonymously. Adding a
global `LLM_*` environment variable layer was rejected because a target
already names its own variable, and a second namespace would make it unclear
which value a target uses. Resolution happens when a request is built rather
than at load time, because that keeps `config.Load` independent of the
environment and lets `auth show` report whether a variable is set without
changing the config.

## How

A `config.Target` has `api_key` and `api_key_env`. `auth setup --key` writes a
literal key and `--key-env` writes the name of an environment variable.
Passing a non-empty value for both is an error. Passing an empty string clears
the field it names, and setting either field to a non-empty value clears the
other, so the two forms cannot both be stored by the command.

`Target.APIKeyValue` returns the value to send. If `api_key_env` is set, it
calls `os.Getenv` with that name; otherwise it returns `api_key`. An unset
variable, like an empty literal, yields the empty string, which the client
passes to the library and which results in a request with no `Authorization`
header. A hand-edited file that sets both fields resolves to the environment
variable, although `auth setup` refuses to create that state.

`config.Save` creates the config directory with mode `0700` and writes the file
with mode `0600`, so a literal key is readable only by its owner. `auth show`
masks a literal key with `mask.Token` from `go-cli-lib`, prints an environment
variable's name with `(set)` or `(unset)`, and warns when a target has no
credential at all or when its named variable is not currently set.
`auth setup --verify` builds a client with the resolved credential and calls
`GET /models` before saving, so a key that the endpoint rejects leaves the
config untouched.

## Consequences

A config that names environment variables carries no secret and can be copied
between machines, while a literal key remains available for a machine where
exporting a variable is inconvenient. The rules are visible in `auth show`,
which reports both the source and the current state of the credential.

The costs are those of keeping secrets in a file at all. A literal key is
protected only by the file mode; there is no Keychain integration and no
encryption at rest. An environment variable has to be set in the process that
runs the tool, so a fresh shell or a scheduled job without that variable sends
no credential. The failure then comes from the endpoint as a 401 rather than
from the tool as a local error, and an endpoint that permits anonymous access
would answer instead of failing. The precedence rule for a hand-edited file
that names both forms is also a quiet one: the literal key is ignored, and
`auth show` reports both fields without saying which one wins. Changing the
field names or the clearing rules later would break existing configs, and the
masking rule has to keep pace with any new credential form.
