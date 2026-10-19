# Architecture decision records

These records were reverse-engineered from the code and the git history in
October 2026, and describe decisions the code already embodies rather than
proposals. ADR 1 was written when its decision was made and is the reference
for the house style.

| Number | Title                                                               | Status                |
| ------ | ------------------------------------------------------------------- | --------------------- |
| 0001   | Route DeepSeek through a network-aware local bridge                 | accepted, implemented |
| 0002   | Keep endpoints as named targets in a data file                      | accepted              |
| 0003   | Store credentials in the config as a literal key or an env var name | accepted              |
| 0004   | Make code a mode of chat rather than a second command               | accepted              |
| 0005   | Keep transport in pkg/api and stdout rendering in pkg/formatter     | accepted              |
