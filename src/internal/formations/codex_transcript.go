package formations

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
)

type codexTranscriptTurn struct {
	SessionID string
	Consumed  bool
	Complete  bool
	Text      string
	Model     string
	Effort    string
	TurnID    string
}

// Read only the exact workspace and dispatched user message. A final assistant
// message counts only after the native task_complete event for that turn.
func readCodexTurn(path, cwd, pointer string) (codexTranscriptTurn, error) {
	var turn codexTranscriptTurn
	f, err := os.Open(path)
	if err != nil {
		return turn, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	first := true
	for scanner.Scan() {
		var record struct {
			Type    string `json:"type"`
			Payload struct {
				ID      string `json:"id"`
				Cwd     string `json:"cwd"`
				Type    string `json:"type"`
				Role    string `json:"role"`
				Channel string `json:"channel"`
				Phase   string `json:"phase"`
				TurnID  string `json:"turn_id"`
				Model   string `json:"model"`
				Effort  string `json:"effort"`
				Message string `json:"message"`
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			break
		} // incomplete trailing record
		p := record.Payload
		if first {
			first = false
			if record.Type != "session_meta" || p.Cwd != cwd {
				return codexTranscriptTurn{}, nil
			}
			turn.SessionID = p.ID
		}
		if record.Type == "turn_context" {
			if turn.Consumed && turn.TurnID != "" && p.TurnID != turn.TurnID {
				return turn, errors.New("another native turn interrupted the dispatched Codex turn")
			}
			turn.Model, turn.Effort = p.Model, p.Effort
			turn.TurnID = p.TurnID
		}
		if record.Type == "response_item" && p.Type == "message" {
			text := ""
			for _, part := range p.Content {
				text += part.Text
			}
			if p.Role == "user" && text == pointer {
				turn.Consumed = true
				turn.Complete = false
				turn.Text = ""
			}
			if p.Role == "user" && text != pointer && turn.Consumed {
				if !turn.Complete {
					return turn, errors.New("another user message interrupted the dispatched Codex turn")
				}
				return turn, nil
			}
			if p.Role == "assistant" && (p.Channel == "final" || p.Phase == "final_answer") && turn.Consumed {
				turn.Text = text
			}
		}
		if record.Type == "event_msg" && p.Type == "task_complete" && turn.Consumed && turn.Text != "" && p.TurnID == turn.TurnID {
			turn.Complete = true
			return turn, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return turn, err
	}
	if first {
		return turn, nil
	}
	if turn.SessionID == "" {
		return turn, errors.New("Codex transcript has no native session identity")
	}
	return turn, nil
}
