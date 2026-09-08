package formations

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
)

// Claude tool-result user records belong to the same turn. Only a human text
// message starts a new turn; a tool-use assistant message cannot complete it.
func readClaudeTurn(path, cwd, pointer string) (codexTranscriptTurn, error) {
	var turn codexTranscriptTurn
	f, err := os.Open(path)
	if err != nil {
		return turn, err
	}
	defer f.Close()

	return readClaudeTurnReader(f, cwd, pointer)
}

func readClaudeTurnReader(reader io.Reader, cwd, pointer string) (codexTranscriptTurn, error) {
	var turn codexTranscriptTurn
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 65536), 16<<20)
	for scanner.Scan() {
		var r struct {
			Type      string `json:"type"`
			Cwd       string `json:"cwd"`
			SessionID string `json:"sessionId"`
			UUID      string `json:"uuid"`
			Effort    string `json:"effort"`
			Message   struct {
				Content    json.RawMessage `json:"content"`
				Model      string          `json:"model"`
				StopReason *string         `json:"stop_reason"`
				Effort     string          `json:"effort"`
			} `json:"message"`
		}
		if json.Unmarshal(scanner.Bytes(), &r) != nil {
			break
		}
		if r.Cwd != "" && r.Cwd != cwd {
			return codexTranscriptTurn{}, nil
		}
		text := strings.Join(assistantContentText(r.Message.Content), "")
		if r.Type == "user" && text != "" {
			if turn.Consumed {
				return turn, errors.New("another user message interrupted the dispatched Claude turn")
			}
			if text == pointer && r.Cwd == cwd && r.SessionID != "" {
				turn.Consumed = true
				turn.SessionID = r.SessionID
				turn.TurnID = r.UUID
			}
		}
		if !turn.Consumed {
			continue
		}
		if r.SessionID != "" && r.SessionID != turn.SessionID {
			return turn, errors.New("Claude native session identity changed")
		}
		if r.Effort != "" {
			turn.Effort = r.Effort
		}
		if r.Type != "assistant" {
			continue
		}
		turn.Model = r.Message.Model
		if r.Message.Effort != "" {
			turn.Effort = r.Message.Effort
		}
		turn.Text = text
		// Some Claude versions omit stop_reason in persisted messages. In that
		// format the last assistant text plus the exact run sentinel is the end
		// signal. An explicit tool_use or max_tokens is never completion.
		hasSentinel := strings.Contains(text, "<<<CHROTE-DONE run-id=")
		turn.Complete = text != "" && hasSentinel && (r.Message.StopReason == nil || *r.Message.StopReason == "end_turn")
		if turn.Complete {
			return turn, nil
		}
	}
	return turn, scanner.Err()
}
