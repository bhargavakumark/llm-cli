# 3. Store credentials in the config as a literal key or an env var name

Date: 2026-10-19
Status: accepted

## Why
Endpoints disagree on credentials: a hosted endpoint needs a key, a loopback 
server needs none. A literal key is convenient but sits in plain text; an 
environment variable keeps the secret out of the file but needs a recorded 
name. The forms are mutually exclusive so a request never has two possible 
credentials.

## How
`config.Target` has `api_key` and `api_key_env`; `auth setup --key` and 
`--key-env` write one and clear the other, and an empty value clears the field. 
`Target.APIKeyValue` resolves the variable at request time, and an empty result 
sends no `Authorization` header. `config.Save` uses modes `0700`/`0600`, and 
`auth show` masks literal keys with `mask.Token`.

## Consequences
A config naming variables carries no secret and can be copied between machines, 
but depends on the shell environment; a missing variable surfaces as an 
endpoint 401, not a local error. A literal key is protected only by file mode, 
and a hand-edited file naming both forms silently prefers the variable.
