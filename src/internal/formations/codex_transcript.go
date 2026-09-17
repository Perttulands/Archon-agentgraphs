package formations

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

type codexTranscriptTurn struct {
	SessionID string
	Consumed  bool
	// Complete means the dispatch has an answer to read: an agent turn after the
	// pointer finished natively with this run's completion sentinel, or finished
	// without it while nobody else took a turn, which the caller rejects.
	Complete bool
	Text     string
	Model    string
	Effort   string
	TurnID   string
	// OperatorTurns counts what someone other than the dispatch did in the seat
	// after the pointer: typed or queued messages and interrupts.
	OperatorTurns int
}

// readCodexTurn reads one dispatch from a Codex rollout: the exact workspace and
// dispatched user message, then every record after it. A final answer counts
// only with the native task_complete event for its turn. Messages the operator
// types, and interrupts, neither complete nor fail the dispatch; the completing
// answer may come on a later turn.
func readCodexTurn(path, cwd, pointer, runID string) (codexTranscriptTurn, error) {
	var turn codexTranscriptTurn
	f, err := os.Open(path)
	if err != nil {
		return turn, err
	}
	defer f.Close()

	return readCodexTurnReader(f, cwd, pointer, runID)
}

func readCodexTurnReader(reader io.Reader, cwd, pointer, runID string) (codexTranscriptTurn, error) {
	var turn codexTranscriptTurn
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	first := true
	pendingContextID := ""
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
				Metadata struct {
					Kinds []string `json:"content_item_kinds"`
				} `json:"internal_chat_message_metadata_passthrough"`
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
			pendingContextID = p.TurnID
			if turn.Consumed && turn.Model != "" && (p.Model != turn.Model || p.Effort != turn.Effort) {
				return turn, fmt.Errorf("the Codex model or effort changed during the dispatch from %s/%s to %s/%s", turn.Model, turn.Effort, p.Model, p.Effort)
			}
			turn.Model, turn.Effort = p.Model, p.Effort
			turn.TurnID = p.TurnID
		}
		if record.Type == "response_item" && p.Type == "message" {
			text := ""
			for _, part := range p.Content {
				text += part.Text
			}
			switch {
			case p.Role == "user" && text == pointer:
				turn.Consumed = true
				turn.TurnID = ""
				turn.Complete = false
				turn.Text = ""
				turn.OperatorTurns = 0
			case !turn.Consumed:
			case p.Role == "user" && codexTypedByOperator(p.Metadata.Kinds):
				turn.OperatorTurns++
			case p.Role == "developer" && hasString(p.Metadata.Kinds, "model_switch.instructions"):
				return turn, errors.New("the Codex model changed during the dispatch (/model)")
			case p.Role == "assistant" && (p.Channel == "final" || p.Phase == "final_answer"):
				turn.Text = text
			}
		}
		if record.Type == "event_msg" && p.Type == "turn_aborted" && turn.Consumed {
			turn.OperatorTurns++
			turn.Text = ""
		}
		if record.Type == "event_msg" && p.Type == "task_complete" && turn.Consumed && turn.Text != "" {
			if turn.TurnID == "" {
				turn.TurnID = pendingContextID
			}
			if p.TurnID != turn.TurnID {
				continue
			}
			if _, ok := ParseCompletionSentinel(turn.Text, runID); ok || turn.OperatorTurns == 0 {
				turn.Complete = true
				return turn, nil
			}
			// The operator took a turn, so an answer without the sentinel may be a
			// reply to them; wait for the dispatch's own completion.
			turn.Text = ""
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

// codexTypedByOperator reports a user message someone typed. Codex marks typed
// text as user.text and injects its own context under other kinds, such as
// environments.environment_context. A message without kinds is typed text.
func codexTypedByOperator(kinds []string) bool {
	return len(kinds) == 0 || hasString(kinds, "user.text")
}

func hasString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
