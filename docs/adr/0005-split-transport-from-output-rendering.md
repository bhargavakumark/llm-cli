# 5. Keep transport in pkg/api and stdout rendering in pkg/formatter

Date: 2026-10-19
Status: accepted

## Why

The command layer has to stream deltas as they arrive while keeping stdout to
the answer and nothing else. Talking to the endpoint and writing the answer
are separate concerns that change for different reasons: the wire format and
timeout policy belong with the client, while flushing and fence removal belong
with the output. Putting HTTP inside `cmd` would tie the client to cobra and to
package-level flag variables. Putting printing inside the client would make it
hard to test without capturing stdout, and would mix presentation with
transport policy.

The third-party library is used as a transport and typed decoder only. It does
not add a system prompt, tools, or any other context to a request, and it does
not rewrite a response. Requests are built explicitly in `pkg/api` so that the
tool keeps that property even if a library default changes. The default request
carries `model` and `messages`, plus streaming when streaming was asked for.
`stream_options` is added only when the target sets `include_usage`, because an
endpoint that rejects the field would fail every request. Temperature, tools,
and every other optional field are left out for the same reason: a minimal
request works against the widest set of endpoints, and a caller cannot
accidentally inherit a library default.

Three other choices were rejected. Leaving the HTTP client at the library
default was rejected after a cold request to the Salesforce gateway took 84
seconds with no output, so dialing and the TLS handshake are now bounded at 10
seconds and the whole request at 300 seconds. Printing from the client was
rejected because the same client is used by `models` and by `auth setup
--verify`, neither of which writes an answer to stdout. Falling back to the
routing table when a requested bind interface cannot be resolved was rejected
because on this machine that would silently restore the tunnel path the binding
exists to avoid.

## How

`main.go` stays thin: it sets the time zone to UTC, injects the version
variables, calls `cmd.Execute`, and maps `context.Canceled` to exit code 130
without an error banner. `cmd/root.go` builds the cobra tree in `newRootCmd`,
registers the persistent `--llm`, `--quiet`, and `--log-requests` flags, and
provides the `info`, `infof`, and `infofGrey` helpers that write to stderr.
`newRootCmd` is called per test while the binary keeps one instance, which is
why flag state does not leak between runs. `readPrompt` takes an `io.Reader`,
the arguments, and the file path rather than reading package globals, and the
interactive prompts in `cmd/auth.go` write to a writer passed by the caller.

`pkg/config` owns load, validation, target resolution, and saving. `pkg/domain`
holds the shared `Message`, `Chunk`, `Result`, and `Usage` types and depends on
nothing else. `pkg/api` owns the client: `NewClient` checks the base URL,
builds an `http.Client` in `newHTTPClient`, and returns a `Client` that exposes
`ChatStream`, `Chat`, and `ListModels`. `dialerFor` builds the dialer, sets the
Go resolver, and binds the local address from `InterfaceIPv4` when a target
names an interface. `wrapAPIError` adds the base URL and unwraps the library's
error types so the HTTP status and the server's message are both visible.
`logRequest` marshals the same request value the library sends, so
`--log-requests` prints the body that actually goes on the wire.

Streaming lives in `Client.ChatStream`. It opens the stream, loops over
`stream.Recv`, converts each choice into a `domain.Chunk`, appends the content
to a `domain.Result`, and calls an `onChunk` callback for each delta. It
returns `ErrNoContent` when the stream produced no choices, which lets
`cmd/chat.go` add the hint about an endpoint ignoring `stream=true`.
`pkg/formatter` owns everything written to stdout: `StreamPrinter` writes and
flushes each delta, `CodePrinter` adds the fence handling, `Text` writes a
complete answer, and `UsageSummary` renders the token line that the command
sends to stderr.

`cmd/chat.go` wires the two together. It resolves the target, builds the
message list, constructs the client, copies `IncludeUsage` and the logging
settings onto it, creates a signal-aware context, and passes `printer.Write`
to `ChatStream`. On success it calls `printer.Finish`, then writes the target
and elapsed time to stderr. The `--no-stream` path uses `client.Chat` and
`formatter.Text` instead. `models` and `verifyTarget` use the same client's
`ListModels`, which is why the split pays for itself beyond chat.

## Consequences

The client can be tested against `httptest` servers without a terminal, and the
formatters can be tested against a buffer, because neither reaches for
`os.Stdout` directly. The command layer stays small enough to read, and the
timeout, resolver, proxy, and interface-binding policy has one home. Errors
carry the endpoint and the HTTP status, so a failed request names the URL it
actually used.

The costs are mostly coupling at the seams. The stream callback is the contract
between the client and the printer, so a new kind of streamed event means
changing both packages together. The request is deliberately minimal, which
means sampling settings and tools are unavailable until the client, the config
schema, and the command layer are extended together. The 300 second limit is
fixed and has no flag, so a generation that legitimately runs longer is cut
off. There is no retry and no fallback path, by design, so a transient network
failure surfaces as an error rather than a second attempt. And the package
direction is now load-bearing: `cmd` depends on `api`, `config`, `domain`, and
`formatter`; `api` and `formatter` depend on `domain`; nothing depends on
`cmd`. Changing that direction would make the client and printers harder to
test in isolation.
