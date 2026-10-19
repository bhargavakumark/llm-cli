# 4. Make code a mode of chat rather than a second command

Date: 2026-10-19
Status: accepted

## Why

`code` asks for code and prints only the code. A model often answers a coding
question with an explanation wrapped around a fenced block, which is noisy
when the answer is going into a file or a compiler. Everything else
about the request is the same as `chat`: the prompt comes from the arguments,
a file, or stdin, the target is selected the same way, the reply streams by
default, an interrupt exits 130, and the summary goes to stderr. A second
command with its own copy of that plumbing would duplicate the parts that are
easy to get subtly different and leave two places to fix them.

The difference is therefore expressed in two places only: an instruction added
to the system message, and a printer that removes a wrapping markdown fence.
The instruction is the primary mechanism. Fence removal exists because models
sometimes ignore it anyway, and it is deliberately narrow: one fence at the
start of the answer and its matching fence at the end, with the body left
exactly as the model wrote it. Extracting a fenced block from an answer that
also contains prose was rejected, because dropping the prose changes what the
model meant to say, and a user who wants only the block can ask again.

`chat` later gained its own always-on style instruction, asking for a crisp
answer without filler or heavy emphasis, and it reuses the same append
mechanism. It has no off switch: the unadorned answer was noisy in a terminal
and expensive to skim. A `--system` value is kept in both commands and
the command's own instruction is appended after it, so extra context does not
override the style or raw-code request. Replacing the user's system prompt
instead of appending was rejected for that reason.

## How

`cmd/chat.go` defines `chatSystemPrompt` and a `chatMode` struct with three
fields: `systemPrompt`, `appendSystem`, and `rawCode`. `runChat` calls
`runChatMode` with the style prompt and `appendSystem` set; `runCode` in
`cmd/code.go` calls the same function with `codeSystemPrompt`, `appendSystem`,
and `rawCode` set. Both commands register the same `--system`, `--file`,
`--no-stream`, and `--show-reasoning` flags, and both use `readPrompt`, which
takes the instruction from `--file` or the arguments and appends piped stdin
to it.

`runChatMode` resolves the target, builds the messages, and constructs the
system message. If the user supplied `--system`, the mode's instruction is
appended after a blank line; if not, the mode's instruction is the system
message. The user's prompt follows as the user message. The request then goes
through `api.NewClient` and either `client.Chat` or `client.ChatStream`.

For a streamed answer, `newStreamWriter` returns `formatter.NewCodePrinter`
when `rawCode` is set and `formatter.NewStreamPrinter` otherwise, and both
satisfy the local `answerWriter` interface with `Write` and `Finish`. For a
non-streamed answer, `writeResult` writes plain chat through `formatter.Text`
and feeds a code answer through the same `CodePrinter`, so a fence is removed
whether or not the reply streamed.

The printer in `pkg/formatter/code.go` holds back only two regions. It waits
for enough leading text to decide whether the answer opens with a fence, drops
that fence line if it does, and keeps a trailing run of whitespace and
backticks of up to `codeFenceWindow` bytes in case it becomes a closing fence.
`Finish` removes one closing fence only when a leading fence was seen. An
answer with prose before the code never triggers the leading case, so it is
left untouched.

## Consequences

A new answer-shaped command is a mode and a printer rather than a new command
implementation, and the prompt, target selection, cancellation, usage, and
stderr reporting stay in one place. The narrow fence rule means a fenced answer
with commentary around it is printed as the model wrote it, which is the
conservative result.

The cost is that `code` is only as reliable as the instruction. A model that
answers with prose and no fence still produces prose, and the tool will not
extract code from it. The two commands also share package-level flag
variables, so they are coupled at the command layer: a new flag for one of them
has to be added carefully so that it does not inherit state from the other.
`chatMode` has no field for request parameters, so a future command that needs
a different temperature or tool set cannot be expressed as a mode without
extending both the mode and the client. Finally, both commands always send a
system message, which means a user who wants the endpoint's own default
behaviour with no system instruction cannot ask for it.
