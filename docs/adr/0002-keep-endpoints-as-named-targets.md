# 2. Keep endpoints as named targets in a data file

Date: 2026-10-19
Status: accepted

## Why
Endpoints differ in ways that decide whether a request works: the DeepSeek 
bridge takes its key from an environment variable, the loopback server on 
`127.0.0.1:6203` needs none, and one target binds an interface while another 
must not. Those settings belong next to the destination rather than in the 
caller's shell.

## How
`pkg/config` loads `~/.config/llm-cli/config.json`: `default_llm` plus an 
`llms` map of `Target` values holding `aliases`, `base_url`, `model`, 
`api_key`, `api_key_env`, `include_usage`, and `bind_interface`. 
`Config.Validate` rejects ambiguous names and aliases and an unknown default; 
`Config.Resolve` prefers an exact name over an alias and errors with the known 
targets. Every command loads and resolves before building a request.

## Consequences
Adding an endpoint is a config write, not a rebuild, and per-target settings 
travel with it. Aliases share one namespace, the JSON field names are a public 
interface a rename would break, and a valid but wrong base URL surfaces only on 
the first request.
