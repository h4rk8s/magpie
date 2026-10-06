package sessions

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/appdir"
)

// ReasonixDir is the native session state home, which may be separate from
// REASONIX_HOME (the configuration home). Reading it never starts Reasonix.
func ReasonixDir() string {
	for _, name := range []string{"REASONIX_STATE_HOME", "REASONIX_HOME"} {
		if d := appdir.Getenv(name); d != "" {
			return d
		}
	}
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "windows" {
		if d := appdir.Getenv("APPDATA"); d != "" {
			return filepath.Join(d, "reasonix")
		}
		return filepath.Join(home, "AppData", "Roaming", "reasonix")
	}
	return filepath.Join(home, ".reasonix")
}

type reasonixMeta struct {
	ID      string    `json:"id"`
	Title   string    `json:"topic_title"`
	Custom  string    `json:"custom_title"`
	Cwd     string    `json:"workspace_root"`
	Created time.Time `json:"created_at"`
	Updated time.Time `json:"updated_at"`
}

// Legacy transcripts are JSONL messages; their .jsonl.meta sidecar supplies
// identity, title and workspace. Usage lives in a separate .turns.jsonl ledger.
// Neither daily global stats (no session id) nor event/DAG logs are transcripts.
// v4/v5 framed stores require their own codec and are not read as legacy files.
func reasonixFiles() []file {
	root := ReasonixDir()
	dirs := []string{filepath.Join(root, "sessions")}
	projects, _ := SessionGlob(filepath.Join(root, "projects", "*", "sessions"))
	dirs = append(dirs, projects...)
	out := []file{}
	chosen := map[string][]file{}
	for _, dir := range dirs {
		for _, e := range readDirectory(dir) {
			n := e.Name()
			if e.IsDir() || !strings.HasSuffix(n, ".jsonl") || strings.HasPrefix(n, ".") {
				continue
			}
			stem := strings.TrimSuffix(n, ".jsonl")
			if strings.Contains(stem, ".") && reasonixSidecar(stem) {
				continue
			}
			p := filepath.Join(dir, n)
			b, err := os.ReadFile(p + ".meta")
			var m reasonixMeta
			manifest := p + ".meta"
			if os.IsNotExist(err) {
				// Older saved conversations have no metadata sidecar. Validate
				// the message prefix, keep their title, and leave usage unknown.
				head := []byte(headOf(p))
				role := strAt(head, []byte(`"role":"`))
				if role == "" {
					role = strAt(bytes.ReplaceAll(head, []byte(`": "`), []byte(`":"`)), []byte(`"role":"`))
				}
				if role != "system" && role != "user" && role != "assistant" && role != "tool" {
					continue
				}
				legacyID := sha256.Sum256([]byte(filepath.Clean(p)))
				m.ID, manifest = "legacy-"+hex.EncodeToString(legacyID[:16]), ""
			} else if err != nil || json.Unmarshal(b, &m) != nil || m.ID == "" {
				continue // unreadable or malformed metadata is not an empty one
			}
			key := "reasonix:" + m.ID
			rev := sha256.Sum256(b)
			f := file{agent: "reasonix", path: p, key: key, main: true, sid: m.ID, manifest: manifest, rev: hex.EncodeToString(rev[:])}
			if !stat(&f) {
				continue
			}
			ledger := file{agent: "reasonix", path: filepath.Join(dir, stem+".turns.jsonl"), key: key, sid: m.ID}
			pair := []file{}
			if stat(&ledger) {
				f.rev += ":ledger"
				pair = []file{f, ledger}
			} else {
				f.rev += ":no-ledger"
				pair = []file{f}
			}
			// A copied session retains its id. Read one copy, not two totals.
			old := chosen[m.ID]
			if len(old) == 0 || f.mod.After(old[0].mod) || f.mod.Equal(old[0].mod) && f.path < old[0].path {
				chosen[m.ID] = pair
			}
		}
	}
	ids := make([]string, 0, len(chosen))
	for id := range chosen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		out = append(out, chosen[id]...)
	}
	return out
}

func reasonixSidecar(stem string) bool {
	for _, suffix := range []string{".events", ".turns", ".conflicts", ".guardian", ".wire", ".adjudication", ".execution"} {
		if strings.HasSuffix(stem, suffix) {
			return true
		}
	}
	return false
}

type reasonixMessage struct {
	Role     string `json:"role"`
	Content  string `json:"content"`
	Thinking string `json:"reasoning_content"`
	Name     string `json:"name"`
	At       int64  `json:"createdAt"`
	Local    bool   `json:"local_only"`
	Tools    []struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"tool_calls"`
}

var reasonixUsageKind = regexp.MustCompile(`"(?:kind|recordType)"\s*:\s*"(?:usage|checkpoint)"`)

// Most ledger bytes are streamed text and tool output. Reject them from the
// first scan buffer, before gathering a potentially multi-megabyte line.
func reasonixUsageHead(b []byte) bool { return reasonixUsageKind.Match(b) }

func reasonixLine(s *state, b []byte, main bool) {
	if !main {
		var e struct {
			Record    string `json:"recordType"`
			Session   string `json:"sessionId"`
			Seq       uint64 `json:"seq"`
			Kind      string `json:"kind"`
			At        int64  `json:"createdAt"`
			Compacted uint64 `json:"compactedThroughSeq"`
			Event     struct {
				Usage *struct {
					Prompt    int    `json:"promptTokens"`
					Output    int    `json:"completionTokens"`
					Hit       int    `json:"cacheHitTokens"`
					Model     string `json:"model"`
					Estimated bool   `json:"estimated"`
				} `json:"usage"`
			} `json:"event"`
		}
		if json.Unmarshal(b, &e) != nil {
			s.UsageIncomplete = true
			return
		}
		if e.Session != s.ID {
			s.UsageIncomplete = true
			return
		}
		if e.Record == "checkpoint" {
			if e.Compacted > 0 {
				s.UsageIncomplete = true
			}
			return
		}
		if e.Record != "event" || e.Kind != "usage" || e.Event.Usage == nil || e.Seq == 0 || e.Seq <= s.ReasonixSeq {
			return
		}
		// Sequence numbers increase across turns in one native ledger. Persist
		// the usage watermark with the aggregate, including after a cold start.
		s.ReasonixSeq = e.Seq
		u := e.Event.Usage
		if u.Estimated || u.Model == "" || u.Prompt < 0 || u.Output < 0 || u.Hit < 0 || u.Hit > u.Prompt || e.At <= 0 {
			s.UsageIncomplete = true
			return
		}
		at := time.UnixMilli(e.At)
		s.saw(at, false)
		// Reasoning is part of completion; sessionCache* and context* fields
		// are gauges, not additional billable usage.
		s.use(dateOf(at), strings.TrimPrefix(u.Model, "magpie/"), Tokens{Input: u.Prompt - u.Hit, Output: u.Output, CacheRead: u.Hit})
		return
	}
	var m reasonixMessage
	if json.Unmarshal(b, &m) != nil || m.Local {
		return
	}
	if m.Role != "user" && m.Role != "assistant" && m.Role != "tool" {
		return
	}
	if m.Role == "user" && s.Title == "" {
		s.Title = title(m.Content)
	}
	if m.At <= 0 {
		return
	}
	at := time.UnixMilli(m.At)
	s.saw(at, true)
	d := s.day(dateOf(at))
	if m.Role == "user" {
		d.Prompts++
	}
	if m.Role == "assistant" {
		d.Replies++
		for _, tool := range m.Tools {
			s.tool(at, tool.Name, "")
		}
	}
}

func reasonixMetadata(s *state, f file) {
	s.ID = f.sid
	if !f.main {
		return
	}
	s.DBRevision = f.rev
	s.UsageIncomplete = f.manifest == "" || strings.HasSuffix(f.rev, ":no-ledger")
	if f.manifest == "" {
		s.Cwd, s.Named, s.Custom = "", "", ""
		return
	}
	var m reasonixMeta
	b, err := os.ReadFile(f.manifest)
	if err != nil || json.Unmarshal(b, &m) != nil {
		return
	}
	s.Cwd, s.Named, s.Custom, s.DBRevision = m.Cwd, m.Title, m.Custom, f.rev
	if !m.Created.IsZero() && (s.Start.IsZero() || m.Created.Before(s.Start)) {
		s.Start = m.Created
	}
	if s.Last.IsZero() {
		s.Last = m.Updated
	}
}

func reasonixTranscript(path string, add func(bool, Part) bool) error {
	_, err := scanAt(path, 0, nil, func(b []byte, _, _ int64) bool {
		var m reasonixMessage
		if json.Unmarshal(b, &m) != nil {
			return true
		}
		if m.Role != "user" && m.Role != "assistant" && m.Role != "tool" {
			return true
		}
		if m.Thinking != "" && !add(false, Part{Role: m.Role, Kind: "thinking", Text: m.Thinking}) {
			return false
		}
		kind := "text"
		if m.Role == "tool" {
			kind = "tool_result"
		}
		if !add(m.Role == "user", Part{Role: m.Role, Kind: kind, Name: m.Name, Text: m.Content}) {
			return false
		}
		for _, tool := range m.Tools {
			if !add(false, Part{Role: m.Role, Kind: "tool_use", Name: tool.Name, Text: tool.Arguments}) {
				return false
			}
		}
		return true
	})
	return err
}
