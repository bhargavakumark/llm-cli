# 4. Make code a mode of chat rather than a second command

Date: 2026-10-19
Status: accepted

## Why
`code` differs from `chat` only in asking for code and printing only the code; 
prompt sources, target selection, streaming, interrupt handling, and stderr 
reporting are identical, so a second command would duplicate plumbing that is 
easy to get subtly wrong.

## How
`cmd/chat.go` defines `chatSystemPrompt` and a `chatMode` of `systemPrompt`, 
`appendSystem`, and `rawCode`; `runChat` and `runCode` both call `runChatMode`, 
which appends the mode's instruction after a user `--system`. Streaming selects 
`formatter.NewCodePrinter` when `rawCode` is set, and `writeResult` uses the 
same printer for non-streamed replies, so one wrapping fence is stripped either 
way.

## Consequences
A new answer-shaped command is a mode and a printer, not another command 
implementation. Reliability rests on the instruction: a model answering with 
prose and no fence still produces prose. Both commands share package-level flag 
variables and always send a system message, so the endpoint's own default 
cannot be requested.
