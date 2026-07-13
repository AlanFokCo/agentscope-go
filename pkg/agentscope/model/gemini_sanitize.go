package model

import "encoding/json"

// SanitizeSchemaForGemini recursively sanitizes a JSON schema for Gemini API
// compatibility. Gemini does not support: const, $ref, $schema,
// additionalProperties, and {"type":"null"}. These are converted to
// Gemini-compatible equivalents or removed.
func SanitizeSchemaForGemini(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return raw
	}
	sanitizeGeminiSchemaMap(schema)
	out, err := json.Marshal(schema)
	if err != nil {
		return raw
	}
	return out
}

func sanitizeGeminiSchemaMap(s map[string]any) {
	// Convert const to enum (Gemini does not support const)
	if c, ok := s["const"]; ok {
		delete(s, "const")
		if _, hasEnum := s["enum"]; !hasEnum {
			s["enum"] = []any{c}
		}
	}

	// Remove unsupported keys
	delete(s, "$ref")
	delete(s, "$schema")
	delete(s, "additionalProperties")

	// Rewrite {"type":"null"} to {"type":"object"}
	if t, ok := s["type"].(string); ok && t == "null" {
		s["type"] = "object"
	}

	// Simplify anyOf with null entries
	if anyOf, ok := s["anyOf"].([]any); ok {
		var filtered []any
		for _, item := range anyOf {
			if m, ok := item.(map[string]any); ok {
				if t, ok := m["type"].(string); ok && t == "null" {
					continue
				}
				sanitizeGeminiSchemaMap(m)
				filtered = append(filtered, m)
			}
		}
		if len(filtered) == 1 {
			if m, ok := filtered[0].(map[string]any); ok {
				for k, v := range m {
					s[k] = v
				}
				delete(s, "anyOf")
			}
		} else if len(filtered) > 0 {
			s["anyOf"] = filtered
		} else {
			delete(s, "anyOf")
		}
	}

	// Recurse into properties
	if props, ok := s["properties"].(map[string]any); ok {
		for _, v := range props {
			if m, ok := v.(map[string]any); ok {
				sanitizeGeminiSchemaMap(m)
			}
		}
	}

	// Recurse into items
	if items, ok := s["items"].(map[string]any); ok {
		sanitizeGeminiSchemaMap(items)
	}

	// Recurse into allOf/oneOf arrays
	for _, key := range []string{"allOf", "oneOf"} {
		if arr, ok := s[key].([]any); ok {
			for _, item := range arr {
				if m, ok := item.(map[string]any); ok {
					sanitizeGeminiSchemaMap(m)
				}
			}
		}
	}
}
