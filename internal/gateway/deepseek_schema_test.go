package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yetone/magpie/internal/provider"
)

func TestUnicodeNullEscape(t *testing.T) {
	for _, tc := range [][2]string{
		{`^[^\0]*$`, `^[^\u0000]*$`},
		{`\\0`, `\\0`},
		{`\01`, `\01`},
		{`\u0000`, `\u0000`},
		{`^https?://`, `^https?://`},
		{`\\\0`, `\\\u0000`},
	} {
		if got := unicodeNullEscape(tc[0]); got != tc[1] {
			t.Errorf("%q: got %q, want %q", tc[0], got, tc[1])
		}
	}
}

func TestDeepSeekToolPatternsOnlySchemas(t *testing.T) {
	input := []byte(`{"messages":[{"role":"user","content":"\\0"}],"tools":[{"name":"Artifact","input_schema":{"type":"object","properties":{"content":{"type":"string","pattern":"^[^\\0]*$","default":{"pattern":"\\0"}}},"const":{"pattern":"\\0"}}},{"type":"function","function":{"name":"other","parameters":{"anyOf":[{"pattern":"\\0"}]}}}]}`)
	out := deepseekToolPatterns(input)
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	tools := got["tools"].([]any)
	schema := tools[0].(map[string]any)["input_schema"].(map[string]any)
	field := schema["properties"].(map[string]any)["content"].(map[string]any)
	if field["pattern"] != `^[^\u0000]*$` {
		t.Fatal("Artifact pattern not normalized")
	}
	if field["default"].(map[string]any)["pattern"] != `\0` || schema["const"].(map[string]any)["pattern"] != `\0` {
		t.Fatal("schema data changed")
	}
	if got["messages"].([]any)[0].(map[string]any)["content"] != `\0` {
		t.Fatal("user message changed")
	}
	other := tools[1].(map[string]any)["function"].(map[string]any)["parameters"].(map[string]any)
	if other["anyOf"].([]any)[0].(map[string]any)["pattern"] != `\u0000` {
		t.Fatal("Chat function schema not normalized")
	}
	unchanged := []byte(` {"tools":[{"input_schema":{"pattern":"^https?://"}}]} `)
	if !bytes.Equal(deepseekToolPatterns(unchanged), unchanged) {
		t.Fatal("unchanged request was re-encoded")
	}
}

func TestDeepSeekToolPatternsResponsesAndInvalidBodies(t *testing.T) {
	input := []byte(`{"id":9007199254740993,"tools":[{"type":"function","name":"Artifact","parameters":{"$defs":{"content":{"pattern":"^[^\\0]*$"}},"properties":{"value":{"items":{"pattern":"\\0"}}}}}]}`)
	out := deepseekToolPatterns(input)
	if bytes.Contains(out, []byte(`\\0`)) || !bytes.Contains(out, []byte(`9007199254740993`)) || !bytes.Contains(out, []byte(`\\u0000`)) {
		t.Fatalf("flattened schema or number not preserved: %s", out)
	}
	for _, input := range [][]byte{[]byte(`{"tools":`), []byte(`null`), []byte(`{"tools":[null]}`)} {
		if !bytes.Equal(deepseekToolPatterns(input), input) {
			t.Fatal("invalid or unchanged request modified")
		}
	}
}

func TestDeepSeekForwardNormalizesArtifact(t *testing.T) {
	var got []byte
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{}`)
	}))
	defer up.Close()
	body := []byte(`{"tools":[{"name":"Artifact","input_schema":{"pattern":"^[^\\0]*$"}}]}`)
	for _, preset := range []string{"deepseek", "anthropic"} {
		p := provider.Provider{Preset: preset, Anthropic: up.URL, Key: "test"}
		res, err := New().forward(context.Background(), p, provider.Anthropic, "/v1/messages", body, http.Header{})
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if preset == "deepseek" && !bytes.Equal(got, deepseekToolPatterns(body)) {
			t.Fatal("DeepSeek forwarded the rejected pattern")
		}
		if preset != "deepseek" && !bytes.Equal(got, body) {
			t.Fatal("other provider changed")
		}
	}
}
