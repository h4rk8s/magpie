package sessions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func reasonixFixture(t *testing.T) (string, string) {
	t.Helper()
	setup(t)
	root := t.TempDir()
	t.Setenv("REASONIX_STATE_HOME", root)
	dir := filepath.Join(root, "projects", "project", "sessions")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "2026-10-06.session.jsonl")
	write := func(p, s string) {
		t.Helper()
		if err := os.WriteFile(p, []byte(s), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(p, `{"role":"user","content":"hello","createdAt":1791244800000}`+"\n"+`{"role":"assistant","content":"hello back","reasoning_content":"thinking","createdAt":1791244801000}`+"\n")
	write(p+".meta", `{"id":"session-a","custom_title":"Named session","workspace_root":"/work/project","created_at":"2026-10-06T00:00:00Z"}`)
	l := strings.TrimSuffix(p, ".jsonl") + ".turns.jsonl"
	write(l, reasonixUsageFixture(1, "executor", "magpie/deepseek/deepseek-flash"))
	// Global usage cannot be assigned to this session. Even a valid metadata
	// sidecar does not make a wire/events ledger into a second conversation.
	write(strings.TrimSuffix(p, ".jsonl")+".wire.jsonl", `{"role":"user","content":"not a transcript"}`+"\n")
	write(strings.TrimSuffix(p, ".jsonl")+".wire.jsonl.meta", `{"id":"wire"}`)
	return p, l
}

func reasonixUsageFixture(seq int, source, model string) string {
	b, _ := json.Marshal(map[string]any{"schemaVersion": 1, "recordType": "event", "sessionId": "session-a", "turnId": "turn-a", "seq": seq, "kind": "usage", "createdAt": 1791244801000, "event": map[string]any{"usage": map[string]any{"promptTokens": 100, "completionTokens": 20, "cacheHitTokens": 60, "reasoningTokens": 10, "sessionCacheHitTokens": 99999, "contextPromptTokens": 900, "source": source, "model": model}}})
	return string(b) + "\n"
}

func reasonixOnly(t *testing.T) Session {
	t.Helper()
	var out []Session
	for _, s := range List(0) {
		if s.Agent == "reasonix" {
			out = append(out, s)
		}
	}
	if len(out) != 1 {
		t.Fatalf("want one Reasonix conversation, got %d", len(out))
	}
	return out[0]
}

func TestReasonixSessionUsageAndTranscript(t *testing.T) {
	p, l := reasonixFixture(t)
	s := reasonixOnly(t)
	if s.ID != "session-a" {
		t.Fatalf("native session identity lost: %q", s.ID)
	}
	if got, ok := Get("reasonix:session-a"); !ok || got.Path != p {
		t.Fatalf("session drill-down missing: %+v %v", got, ok)
	}
	if s.Title != "Named session" || s.Cwd != "/work/project" || s.Path != p || !s.Transcript {
		t.Fatalf("session metadata: %+v", s)
	}
	if s.Input != 40 || s.Output != 20 || s.CacheRead != 60 {
		t.Fatalf("billable buckets: %+v", s.Tokens)
	}
	tr, err := TranscriptOf(s)
	if err != nil || len(tr.Parts) != 3 || tr.Parts[0].Text != "hello" || tr.Parts[1].Kind != "thinking" {
		t.Fatalf("transcript: %+v, %v", tr, err)
	}
	f, err := os.OpenFile(l, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	// A repeated event is not a new call; Planner and Executor count under
	// their actual models, not under the session's default model.
	_, err = f.WriteString(reasonixUsageFixture(1, "executor", "magpie/deepseek/deepseek-flash") + reasonixUsageFixture(2, "planner", "magpie/deepseek/deepseek-pro"))
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	s = reasonixOnly(t)
	if s.Input != 80 || s.Output != 40 || s.CacheRead != 120 || len(s.Models) != 2 {
		t.Fatalf("append or duplicate accounting: %+v", s)
	}
	Reset() // cold aggregate cache must retain the ledger watermark
	s = reasonixOnly(t)
	if s.Input != 80 {
		t.Fatalf("cold cache duplicated usage: %+v", s)
	}
	if err := os.WriteFile(p+".meta", []byte(`{"id":"session-a","custom_title":"Renamed","workspace_root":"/work/other"}`), 0600); err != nil {
		t.Fatal(err)
	}
	s = reasonixOnly(t)
	if s.Title != "Renamed" || s.Cwd != "/work/other" {
		t.Fatalf("metadata-only change was stale: %+v", s)
	}
	var found bool
	for _, sum := range StatsFor(0).Sessions {
		if sum.Agent == "reasonix" {
			found = true
			if sum.Input != 80 {
				t.Fatalf("statistics: %+v", sum)
			}
		}
	}
	if !found {
		t.Fatal("Reasonix missing from session statistics")
	}
}

func TestReasonixLedgerRewriteAndPartialTail(t *testing.T) {
	_, l := reasonixFixture(t)
	reasonixOnly(t)
	line := reasonixUsageFixture(3, "executor", "deepseek/deepseek-flash")
	if err := os.WriteFile(l, []byte(line[:len(line)-1]), 0600); err != nil {
		t.Fatal(err)
	}
	if s := reasonixOnly(t); s.Input != 0 {
		t.Fatalf("partial event counted: %+v", s)
	}
	f, _ := os.OpenFile(l, os.O_APPEND|os.O_WRONLY, 0600)
	f.WriteString("\n")
	f.Close()
	if s := reasonixOnly(t); s.Input != 40 {
		t.Fatalf("completed tail not read: %+v", s)
	}
	if err := os.WriteFile(l, []byte(reasonixUsageFixture(1, "planner", "deepseek/deepseek-pro")), 0600); err != nil {
		t.Fatal(err)
	}
	if s := reasonixOnly(t); len(s.Models) != 1 || s.Models[0].Model != "deepseek/deepseek-pro" {
		t.Fatalf("replacement retained old usage: %+v", s)
	}
}

func TestReasonixMissingAndCompactedHistory(t *testing.T) {
	p, l := reasonixFixture(t)
	if err := os.Remove(l); err != nil {
		t.Fatal(err)
	}
	if s := reasonixOnly(t); !s.UsageIncomplete || s.Input != 0 {
		t.Fatalf("missing history: %+v", s)
	}
	checkpoint := `{"recordType":"checkpoint","sessionId":"session-a","compactedThroughSeq":200}` + "\n"
	if err := os.WriteFile(l, []byte(checkpoint+reasonixUsageFixture(201, "executor", "deepseek/deepseek-flash")), 0600); err != nil {
		t.Fatal(err)
	}
	if s := reasonixOnly(t); !s.UsageIncomplete || s.Input != 40 {
		t.Fatalf("compacted history: %+v", s)
	}
	if err := os.Remove(p + ".meta"); err != nil {
		t.Fatal(err)
	}
	if s := reasonixOnly(t); !s.UsageIncomplete || s.Title != "hello" {
		t.Fatalf("older metadata-free conversation: %+v", s)
	}
}

func TestReasonixCopiedSessionIsNotCountedTwice(t *testing.T) {
	p, l := reasonixFixture(t)
	dir := filepath.Join(ReasonixDir(), "sessions")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, src := range []string{p, p + ".meta", l} {
		b, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, filepath.Base(src)), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if s := reasonixOnly(t); s.Input != 40 {
		t.Fatalf("copied session double-counted: %+v", s)
	}
}

func BenchmarkReasonixLedgerAppend(b *testing.B) {
	dir := b.TempDir()
	p := filepath.Join(dir, "session.turns.jsonl")
	// Real ledger shape: text dominates the bytes; only a few usage events
	// are needed by statistics. The scanner must skip long streamed text.
	text := `{"recordType":"event","sessionId":"session-a","kind":"text","event":{"text":"` + strings.Repeat("x", 8192) + `"}}` + "\n"
	data := strings.Repeat(text, 2048) + reasonixUsageFixture(1, "executor", "deepseek/deepseek-flash")
	if err := os.WriteFile(p, []byte(data), 0600); err != nil {
		b.Fatal(err)
	}
	f := file{agent: "reasonix", path: p, sid: "session-a"}
	stat(&f)
	s := parse(f, nil)
	writer, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		b.Fatal(err)
	}
	defer writer.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err = writer.WriteString(reasonixUsageFixture(i+2, "executor", "deepseek/deepseek-flash")); err != nil {
			b.Fatal(err)
		}
		stat(&f)
		s = parse(f, s)
	}
	if s.Models["deepseek/deepseek-flash"].Input != 40*(b.N+1) {
		b.Fatal("append usage lost or duplicated")
	}
}
