# 5. Keep transport in pkg/api and stdout rendering in pkg/formatter

Date: 2026-10-19
Status: accepted

## Why
Talking to the endpoint and writing the answer change for different reasons: 
wire format and timeouts belong with the client, flushing and fence removal 
with the output. Putting HTTP in `cmd` would tie the client to cobra and 
package-level flags; putting printing in the client would mix presentation with 
transport and make it untestable without capturing stdout.

## How
`pkg/api` owns the client and builds requests explicitly, sending only `model` 
and `messages`, plus `stream` when streaming and `stream_options` when the 
target sets `include_usage`, with 10 s dial/TLS and 300 s request limits. 
`pkg/formatter` owns stdout: `StreamPrinter`, `CodePrinter`, `Text`, 
`UsageSummary`. `cmd/chat.go` passes `printer.Write` to `Client.ChatStream`, 
and `models` and `auth setup --verify` reuse `ListModels`.

## Consequences
Client and formatters test against `httptest` and buffers without a terminal. 
The stream callback couples the packages, the minimal request leaves sampling 
and tools unavailable until the schema and client grow, and the fixed 300 s 
limit has no flag.
