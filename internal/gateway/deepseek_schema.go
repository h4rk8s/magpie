package gateway

import (
	"bytes"
	"encoding/json"
	"strings"
)

// DeepSeek rejects the ECMAScript null escape in Claude Code's Artifact
// schema. Its Unicode spelling has the same meaning and is accepted by
// DeepSeek. Only tool schemas change; user messages and schema data do not.
func deepseekToolPatterns(body []byte) []byte {
	if !bytes.Contains(body, []byte(`\\0`)) {
		return body
	}
	var request map[string]any
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if dec.Decode(&request) != nil {
		return body
	}
	changed := false
	tools, _ := request["tools"].([]any)
	for _, value := range tools {
		tool, _ := value.(map[string]any)
		for _, key := range []string{"input_schema", "parameters"} {
			changed = normalizeNullPatterns(tool[key]) || changed
		}
		if fn, ok := tool["function"].(map[string]any); ok {
			changed = normalizeNullPatterns(fn["parameters"]) || changed
		}
	}
	if !changed {
		return body
	}
	out, err := json.Marshal(request)
	if err != nil {
		return body
	}
	return out
}

func normalizeNullPatterns(value any) bool {
	changed := false
	switch node := value.(type) {
	case map[string]any:
		if pattern, ok := node["pattern"].(string); ok {
			if normalized := unicodeNullEscape(pattern); normalized != pattern {
				node["pattern"], changed = normalized, true
			}
		}
		for key, child := range node {
			switch key {
			case "properties", "$defs", "definitions", "dependentSchemas", "patternProperties":
				if entries, ok := child.(map[string]any); ok {
					for _, schema := range entries {
						changed = normalizeNullPatterns(schema) || changed
					}
				}
			case "items", "additionalProperties", "contains", "propertyNames", "not", "if", "then", "else", "allOf", "anyOf", "oneOf", "prefixItems", "unevaluatedProperties", "unevaluatedItems", "contentSchema":
				changed = normalizeNullPatterns(child) || changed
			}
		}
	case []any:
		for _, child := range node {
			changed = normalizeNullPatterns(child) || changed
		}
	}
	return changed
}

func unicodeNullEscape(pattern string) string {
	var out strings.Builder
	for i := 0; i < len(pattern); i++ {
		if pattern[i] != '\\' || i+1 == len(pattern) {
			out.WriteByte(pattern[i])
			continue
		}
		next := pattern[i+1]
		// A pair of backslashes is literal; an octal escape has its own
		// meaning and must not be shortened to a null character.
		if next == '0' && (i+2 == len(pattern) || pattern[i+2] < '0' || pattern[i+2] > '9') {
			out.WriteString(`\u0000`)
		} else {
			out.WriteByte('\\')
			out.WriteByte(next)
		}
		i++
	}
	return out.String()
}
