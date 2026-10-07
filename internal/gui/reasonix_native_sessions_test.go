package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/sessions"
)

// Exercise the actual HTTP handlers with files emitted by published Reasonix,
// rather than returning a precomputed session from a browser route mock.
func TestReasonix229NativeSessionRoutes(t *testing.T) {
	home := sandboxHome(t)
	root := filepath.Join(home, "reasonix-state")
	dir := filepath.Join(root, "projects", "fixture", "sessions")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REASONIX_STATE_HOME", root)
	for _, name := range []string{"resumed.jsonl", "resumed.jsonl.meta", "resumed.jsonl.telemetry.json", "resumed.wire.jsonl"} {
		b, err := os.ReadFile(filepath.Join("..", "sessions", "testdata", "reasonix-2.29.0", name))
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, name), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	sessions.Reset()
	t.Cleanup(sessions.Reset)
	mux := http.NewServeMux()
	sessionRoutes(mux, folderOnly{})
	sessionManageRoutes(mux, folderOnly{})
	var id string
	for _, path := range []string{"/api/sessions", "/api/sessions/manage?agent=reasonix"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		var out struct {
			Sessions []sessions.Session `json:"sessions"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
		found := false
		for _, s := range out.Sessions {
			if s.Agent == "reasonix" {
				found = true
				id = s.ID
				if s.Title != "hello there" || s.Cwd != "/work/reasonix-229" || s.Input != 702 || s.Output != 168 || s.CacheRead != 3000 || s.UsageIncomplete {
					t.Fatalf("%s: %+v", path, s)
				}
			}
		}
		if !found {
			t.Fatalf("%s hides native Reasonix session", path)
		}
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/sessions/transcript?agent=reasonix&id="+id, nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "resume follow up") || strings.Contains(w.Body.String(), "Current workspace:") {
		t.Fatalf("native transcript: %d %s", w.Code, w.Body)
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/sessions/stats", nil))
	var stats struct {
		sessions.Stats
		Agents map[string]string `json:"agents"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &stats) != nil {
		t.Fatalf("stats: %d %s", w.Code, w.Body)
	}
	found := false
	for _, d := range stats.Days {
		for _, u := range d.Usage {
			if u.Agent == "reasonix" {
				found = true
				if u.Input != 702 || u.Output != 168 || u.CacheRead != 3000 || u.Model != "fake/fake-model" {
					t.Fatalf("native stats usage: %+v", u)
				}
			}
		}
	}
	if !found || stats.Agents["reasonix"] == "" {
		t.Fatalf("native stats not discoverable: %+v", stats)
	}
}
