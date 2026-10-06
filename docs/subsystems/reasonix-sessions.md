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

Release lines differ (npm tags checked 2026-10-06): `latest` is 2.29.0;
`next`/`canary` are 1.39.7. The 1.x line writes the associated `.turns.jsonl`
ledger and supports retained per-session usage. The 2.x line does not write
that ledger: its daily `stats/*.jsonl` have no session id. A 2.x conversation
therefore shows models from assistant `modelRef` fields, unknown token totals
and **Partial usage history** by design. Model presence is not billable usage.
The release contract was checked against
[Reasonix source 9d4a2bd](https://github.com/esengine/DeepSeek-Reasonix/blob/9d4a2bd/internal/contract/provider/provider.go)
and its [session store](https://github.com/esengine/DeepSeek-Reasonix/blob/9d4a2bd/internal/state/store/session.go).

The 1.x ledger supplies retained, non-estimated `usage`
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
Directory reporting stats the native session directories without opening any
metadata or transcript. Unchanged files reuse their aggregate. The adapter's
revision invalidates older transcript aggregates so a previously cached 2.x
conversation also gains its model names. Appends read from the last complete line;
truncation, sampled prefix changes or identity changes rebuild it. Metadata-only
changes invalidate the transcript summary. Ledger text/tool-output lines are
filtered before gathering long lines. Prefix validation uses the shared bounded
sampling contract; an unsampled same-size rewrite is not guaranteed detectable.

`local_only` and `host_authored` messages do not count as user prompts or
supply fallback titles. The transcript viewer preserves the native content.

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
The 2.29.0-shaped fixture has transcript/metadata and assistant `modelRef`,
with no ledger; tests assert both model names, unknown tokens, partial history,
host-authored exclusion and old-cache upgrade. It is source-contract validation,
not a claim of running the published 2.29.0 executable. The real native JSONL
read-only observation was from the local 1.x installation
`v1.39.4-74-g4a0505420` (commit `4a050542060a`). Historical files have no
writer-version stamp, so individual producer versions are unknown; this did not
validate the published 2.x executable.
`BenchmarkReasonixLedgerAppend` measures append updates after a text-heavy ledger.
The GUI session conversation and agent-strip tests cover Reasonix filtering,
transcript opening and incomplete-history labels in both engines.
