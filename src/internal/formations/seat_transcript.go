package formations

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
)

// A leader may revise a worker through the same reserved pointer. Start at
// its latest native user message so an earlier completion cannot hide new work.
// Preserve Codex's session identity and preceding turn context for versions
// that write context before the user message.
func readLatestSeatTurn(s *nativeSeat, path, cwd, pointer string) (codexTranscriptTurn, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return codexTranscriptTurn{}, err
	}
	lines := splitJSONLines(data)
	start := -1
	var meta, contextLine []byte
	var selectedContext []byte
	for i, line := range lines {
		var r struct {
			Type    string `json:"type"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
			Payload struct {
				Type    string `json:"type"`
				Role    string `json:"role"`
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"payload"`
		}
		if json.Unmarshal(line, &r) != nil {
			break
		}
		if r.Type == "session_meta" && i == 0 {
			meta = line
		}
		if r.Type == "turn_context" {
			contextLine = line
		}
		text := ""
		if s.variant.ID == "claude-code" && r.Type == "user" {
			text = strings.Join(assistantContentText(r.Message.Content), "")
		}
		if s.variant.ID == "openai-codex" && r.Type == "response_item" && r.Payload.Type == "message" && r.Payload.Role == "user" {
			for _, part := range r.Payload.Content {
				text += part.Text
			}
		}
		if text == pointer {
			start = i
			selectedContext = contextLine
		}
	}
	if start < 0 {
		return codexTranscriptTurn{}, nil
	}
	selected := lines[start:]
	if s.variant.ID == "claude-code" {
		return readClaudeTurnReader(bytes.NewReader(bytes.Join(selected, []byte("\n"))), cwd, pointer, s.runID)
	}
	if len(meta) == 0 {
		return codexTranscriptTurn{}, nil
	}
	prefix := [][]byte{meta}
	if len(selectedContext) > 0 {
		prefix = append(prefix, selectedContext)
	}
	return readCodexTurnReader(bytes.NewReader(bytes.Join(append(prefix, selected...), []byte("\n"))), cwd, pointer, s.runID)
}
