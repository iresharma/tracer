package parser

import (
	"encoding/json"
	"strconv"
	"strings"
)

// ExtractTraceID reports whether content is a JSON object and, if so,
// attempts to pull the configured field out as a string (numeric values
// are coerced to string). Any failure (not JSON, missing field, wrong
// type) yields isJSON=false or traceID="" — callers must still store the
// line for plain browsing even when correlation isn't possible.
func ExtractTraceID(content, fieldName string) (isJSON bool, traceID string) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" || trimmed[0] != '{' {
		return false, ""
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &fields); err != nil {
		return false, ""
	}

	raw, ok := fields[fieldName]
	if !ok {
		return true, ""
	}

	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return true, s
	}

	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		return true, n.String()
	}

	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return true, strconv.FormatFloat(f, 'f', -1, 64)
	}

	return true, ""
}
