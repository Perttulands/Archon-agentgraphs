package formations

import (
	"bytes"
	"encoding/json"
	"strings"
)

func assistantContentText(content json.RawMessage) []string {
	trimmed := bytes.TrimSpace(content)
	if len(trimmed) == 0 {
		return nil
	}
	if trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return nil
		}
		if strings.TrimSpace(text) == "" {
			return nil
		}
		return []string{text}
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(trimmed, &blocks); err != nil {
		return nil
	}
	var parts []string
	for _, block := range blocks {
		if block.Type == "text" && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return parts
}

// splitJSONLines splits JSONL bytes into non-empty trimmed lines.
func splitJSONLines(data []byte) [][]byte {
	raw := bytes.Split(data, []byte("\n"))
	lines := make([][]byte, 0, len(raw))
	for _, line := range raw {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}
