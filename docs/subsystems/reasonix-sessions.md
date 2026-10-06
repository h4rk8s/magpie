# Reasonix local conversations and usage

Reasonix is an agent, not a subscription provider. Its configuration and separate
Executor/Plan model choices belong to [agent wiring](agent-wiring.md).
Provider plugins authenticate and forward requests; they do not register local
conversation readers.

## Sources and boundaries

[`reasonixFiles`](../../internal/sessions/reasonix.go) discovers native JSONL
conversations under `sessions/` and `projects/*/sessions/`. The root is
`REASONIX_STATE_HOME`, then `REASONIX_HOME`, otherwise `~/.reasonix` on Unix or
`%APPDATA%/reasonix` on Windows. Reads neither start Reasonix nor migrate its data.

Each conversation's `.jsonl.meta` supplies its identity, title and workspace.
Older message files without metadata are admitted from their native message
prefix; unknown workspace and usage attribution are left unknown. Sidecars,
subagent directories and archive/import trees are not separate conversations.
Copied conversations with the same metadata id count once, using the newest
transcript (lexical path breaks a tie).

The associated `.turns.jsonl` ledger supplies retained, non-estimated `usage`
events. A sequence watermark persists with the aggregate across cache reloads.
Planner, Executor and auxiliary usage retain their actual model references.
Prompt includes cached input: uncached input is prompt minus cache hit. Completion
already includes reasoning; cumulative session and context gauges add no tokens.

No ledger, missing identity, a compacted ledger or invalid/estimated usage makes
`usage_incomplete` true. The GUI labels that history incomplete. A checkpoint
contains terminal summaries, not the discarded usage; those totals cannot be
reconstructed. Daily `stats/*.jsonl` have no session id and are not assigned to
conversations by proximity in time. Missing message timestamps do not become
invented daily activity.

## Shared readers and cache behavior

The adapter uses the shared discovery, incremental scanner and summary cache.
Unchanged files reuse their aggregate. Appends read from the last complete line;
truncation, sampled prefix changes or identity changes rebuild it. Metadata-only
changes invalidate the transcript summary. Ledger text/tool-output lines are
filtered before gathering long lines. Prefix validation uses the shared bounded
sampling contract; an unsampled same-size rewrite is not guaranteed detectable.

`TranscriptOf` reads text, thinking, tool calls and results on demand with shared
size limits. Unix resume commands quote the native file path. Deletion and
cross-agent conversion are unsupported: this adapter does not delete sidecars,
content blobs, or rewrite another agent's history.

## Coverage not provided

Framed `sessions-v4`/`desktop-sessions-v5` stores need their own codecs and are not
interpreted as legacy JSONL. This reader does not add native request rows to the
usage ledger, conversation tracing, or library instructions/skills/MCP targets.
Gateway Reasonix request accounting continues independently. Keeping native
summary usage separate avoids counting a routed request twice in that ledger.

## Verification

`go test -tags nogui ./internal/sessions ./internal/agentenv` exercises native
shapes, independent role models, duplicate events/copies, missing/compacted
history, metadata edits, completed tails, replacement and cold cache reloads.
`BenchmarkReasonixLedgerAppend` measures append updates after a text-heavy ledger.
The GUI session conversation and agent-strip tests cover Reasonix filtering,
transcript opening and incomplete-history labels in both engines.
