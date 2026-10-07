# Reasonix local conversations and usage

Reasonix is an agent, not a subscription provider. Configuration and independent
Executor/Plan model choices belong to [agent wiring](agent-wiring.md). Provider
plugins do not register local conversation readers.

## Sources and boundaries

[`reasonixFiles`](../../internal/sessions/reasonix.go) discovers JSONL transcripts
under `sessions/` and `projects/*/sessions/`. The root is `REASONIX_STATE_HOME`,
then `REASONIX_HOME`, otherwise `~/.reasonix` on Unix or `%APPDATA%/reasonix` on
Windows. Discovery never starts Reasonix or migrates its data. Reasonix's
`[storage]` configuration can move its state root; this reader does not parse
that table. Set the state-home override to the actual configured root.

A `.jsonl.meta` supplies identity and optional title/workspace fields. Native
2.29.0 metadata need not contain `workspace_root`; the reader uses the quoted
path in the host's `<workspace>` block when available. It does not decode a
lossy project-directory name into a path. `preview` is a fallback title, not a
conversation. `raw_content` on a user message, including an explicitly empty
value, wins over the host-wrapped `content` for titles and displayed text.

Metadata-free transcripts are admitted from their native message prefix.
Malformed/unreadable metadata is not an empty one. Sidecars, subagent directories
and archive/import trees are not additional conversations. Copies with one id
count once: newest transcript wins, lexical path breaks ties.

## Usage sources and release lines

Reasonix **1.x** writes `.turns.jsonl` usage events. That ledger takes precedence
when present. The reader keeps retained, non-estimated usage with each event's
actual model and timestamp. Its usage sequence watermark persists across cache
reloads. Checkpoints do not reconstruct discarded usage.

Reasonix **2.29.0** also records per-session usage, in two different sidecars:

- `.wire.jsonl` retains individual request `usage` frames. `turn_started` provides
  the executor model and the native user-message index. A usage cost quote's
  `modelRef`, when present, supplies the actual request model, including auxiliary
  calls. A missing auxiliary model is not assigned to the executor.
- `.jsonl.telemetry.json` records host totals without a model/date breakdown.
  It checks wire coverage; it is **not added** to the wire totals.

The published 2.29.0 `serve --resume` restarts wire `seq` at 1 while retaining
previous frames. Its telemetry accumulator resets to the new process's requests.
The reader counts retained frames across processes and accepts telemetry matching
either the whole log or its latest sequence epoch. It never treats a restarted
wire seq as a duplicate request. The 1.x ledger watermark is not used for wire.

Reasonix caps wire logs at 8 MB. `.wire.meta.json` with `truncated:true` marks the
retained prefix incomplete. Missing usage, unreadable/malformed data, invalid or
estimated usage, unavailable model/date attribution, or a telemetry mismatch
also make `usage_incomplete` true. The GUI displays **Partial usage history**.
Retained wire tokens are still visible; telemetry totals are not guessed into
unknown models or dates. Daily `stats/*.jsonl` have no session id and are not
assigned to conversations by temporal proximity.

Prompt includes cache hit: uncached input is prompt minus cache hit. Completion
already includes reasoning. Session/context cumulative gauges and cold-start
subtotals are not added again. An estimated cost quote is not estimated token
usage. Assistant `modelRef` also preserves model presence when no usage survives;
presence adds no tokens and no unpriced spend.

## Messages, dates and cache

Native 2.29.0 assistant messages have no `createdAt`. Their reply count and model
presence belong to the preceding native user turn's date; this is not an invented
assistant wall-clock timestamp. Wire requests use their turn's indexed user
timestamp. Missing anchors stay unknown. Metadata `updated_at` can supply the
last activity time. `local_only` and `host_authored` messages do not count as
human prompts or fallback titles. The transcript viewer preserves native content
while showing a human user's `raw_content` instead of injected context.

The adapter uses shared discovery, scanning and summary caching. `Dirs()` stats
session directories without opening metadata/transcripts. Unchanged summaries
are reused. Message/1.x ledger appends read from their saved offset. The bounded
2.x wire log is rebuilt when its bytes, transcript revision, telemetry or
truncation marker changes; its transcript supplies message-index timestamps only
through the last referenced user index, avoiding unrelated large transcript tails.
Cached pre-adapter-revision message summaries rebuild once, correcting old titles,
reply counts and model dates. Truncation, sampled prefix or identity changes
rebuild incremental state. An unsampled same-size rewrite is not guaranteed
detectable by the shared bounded prefix contract.

`TranscriptOf` reads text/thinking/tool calls/results with shared limits. Unix
resume quotes the native path. Deletion and cross-agent conversion are unsupported.
Framed `sessions-v4`/`desktop-sessions-v5` stores need separate codecs. Native
request rows, conversation tracing and library targets are outside this reader.
Gateway request accounting remains independent, avoiding a second native copy
of routed requests in that ledger.

## Verification and provenance

The fixtures in [`testdata/reasonix-2.29.0`](../../internal/sessions/testdata/reasonix-2.29.0/README.md)
were captured from the published `@reasonix/cli-darwin-arm64@2.29.0` binary,
with its npm archive integrity verified, in an empty HOME/state/workspace against
a loopback fake OpenAI upstream. Two real POST /submit prompts produced native
transcript, metadata, telemetry and wire files. A fresh serve --resume process
produced the resumed capture. Sandbox path and writer id are the only normalizations.
These are real native-host/file-format checks, not real-vendor inference checks.

`go test -tags nogui ./internal/sessions ./internal/agentenv` covers both sources,
native raw input, missing assistant timestamps/workspace metadata, resume epochs,
coverage markers, duplicates/copies, cold reload, tails/replacements, metadata
edits and old-cache upgrade. Both GUI suites assert native 2.x model, user text
and visible usage in Chromium/WebKit and en/zh/ja/de. `BenchmarkReasonixLedgerAppend`
checks the existing text-heavy 1.x append path; `BenchmarkReasonix229WireLargeTranscriptTail`
checks a changed 2.x wire summary beside a 16 MB irrelevant transcript tail.
`TestReasonix229NativeSessionRoutes` runs the actual sessions/manage/stats/transcript
HTTP handlers over the captured files.

The earlier read-only native-history observation was against the local 1.x
installation `v1.39.4-74-g4a0505420`; historical files have no writer-version stamp.
That observation did not establish the published 2.x file format. The real 2.29.0
captures above replace the earlier source-inferred 2.x fixtures and conclusions.
