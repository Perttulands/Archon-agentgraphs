package formations

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// readClaudeTurn reads one dispatch from a Claude Code transcript: the exact
// pointer, then every record after it. Tool results, skill text and task
// notifications belong to the agent's work. The operator may type or queue
// messages and interrupt; those turns neither complete nor fail the dispatch.
func readClaudeTurn(path, cwd, pointer, runID string) (codexTranscriptTurn, error) {
	var turn codexTranscriptTurn
	f, err := os.Open(path)
	if err != nil {
		return turn, err
	}
	defer f.Close()

	return readClaudeTurnReader(f, cwd, pointer, runID)
}

func readClaudeTurnReader(reader io.Reader, cwd, pointer, runID string) (codexTranscriptTurn, error) {
	var turn codexTranscriptTurn
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 65536), 16<<20)
	// Work the agent started in the background finishes after its turn ends, and
	// Claude Code starts a new turn to report it, so such a turn is not final.
	background := false
	for scanner.Scan() {
		var r struct {
			Type   string `json:"type"`
			IsMeta bool   `json:"isMeta"`
			Origin struct {
				Kind string `json:"kind"`
			} `json:"origin"`
			PromptSource string `json:"promptSource"`
			Operation    string `json:"operation"`
			Content      string `json:"content"`
			Cwd          string `json:"cwd"`
			SessionID    string `json:"sessionId"`
			UUID         string `json:"uuid"`
			Effort       string `json:"effort"`
			Attachment   struct {
				Type   string `json:"type"`
				Origin struct {
					Kind string `json:"kind"`
				} `json:"origin"`
			} `json:"attachment"`
			Message struct {
				Content    json.RawMessage `json:"content"`
				Model      string          `json:"model"`
				StopReason *string         `json:"stop_reason"`
				Effort     string          `json:"effort"`
			} `json:"message"`
		}
		if json.Unmarshal(scanner.Bytes(), &r) != nil {
			break
		}
		// The dispatched session is identified by its pointer record, which must
		// carry the run cwd. A seat may change directory while it works, and
		// Claude then records the new cwd on every later line; those records
		// still belong to the same native session.
		if !turn.Consumed && r.Cwd != "" && r.Cwd != cwd {
			continue
		}
		text := strings.Join(assistantContentText(r.Message.Content), "")
		// Claude Code also writes skill content (isMeta), task notifications and
		// other system prompts as user records. Only a human message starts a
		// new turn; when the record names its origin, trust that over heuristics.
		human := r.Type == "user" && text != "" && !r.IsMeta
		if human && (r.Origin.Kind != "" || r.PromptSource != "") {
			human = r.Origin.Kind == "human" || (r.Origin.Kind == "" && r.PromptSource == "typed")
		}
		if human && claudePointerMatches(text, pointer) && r.Cwd == cwd && r.SessionID != "" {
			turn = codexTranscriptTurn{Consumed: true, SessionID: r.SessionID, TurnID: r.UUID}
			background = false
			continue
		}
		if !turn.Consumed {
			continue
		}
		if r.SessionID != "" && r.SessionID != turn.SessionID {
			return turn, errors.New("Claude native session identity changed")
		}
		if command := claudeLocalCommand(text); r.Type == "user" && (command == "/model" || command == "/effort") {
			return turn, fmt.Errorf("the Claude %s changed during the dispatch (%s)", strings.TrimPrefix(command, "/"), command)
		}
		switch {
		case human, claudeInterrupted(r.Type, text):
			turn.OperatorTurns++
		case r.Type == "queue-operation" && r.Operation == "enqueue" && !strings.HasPrefix(strings.TrimSpace(r.Content), "<task-notification>"):
			// A message the operator queued while the agent worked.
			turn.OperatorTurns++
		case r.Type == "attachment" && r.Attachment.Type == "queued_command" && r.Attachment.Origin.Kind == "human":
			turn.OperatorTurns++
		}
		if r.Effort != "" {
			turn.Effort = r.Effort
		}
		if r.Type != "assistant" {
			continue
		}
		background = background || claudeStartsBackgroundWork(r.Message.Content)
		// Claude Code writes its own notices as "<synthetic>" assistant records.
		if model := r.Message.Model; model != "" && model != "<synthetic>" {
			if turn.Model != "" && model != turn.Model {
				return turn, fmt.Errorf("the Claude model changed during the dispatch from %s to %s", turn.Model, model)
			}
			turn.Model = model
		}
		if r.Message.Effort != "" {
			turn.Effort = r.Message.Effort
		}
		if text == "" {
			continue
		}
		turn.Text = text
		// Some Claude versions omit stop_reason in persisted messages. In that
		// format the last assistant text plus the exact run sentinel is the end
		// signal. An explicit tool_use or max_tokens is never completion.
		stop := ""
		if r.Message.StopReason != nil {
			stop = *r.Message.StopReason
		}
		if _, ok := ParseCompletionSentinel(text, runID); ok && (stop == "" || stop == "end_turn") {
			turn.Complete = true
			return turn, nil
		}
		// A turn that ends without the sentinel, while nobody else took a turn and
		// no background work is still due, is the agent's final word: the caller
		// rejects it at once rather than waiting for the seat timeout.
		if stop == "end_turn" && turn.OperatorTurns == 0 && !background {
			turn.Complete = true
			return turn, nil
		}
	}
	return turn, scanner.Err()
}

// claudeInterrupted reports the record Claude Code writes when the operator
// presses Esc during a turn.
func claudeInterrupted(recordType, text string) bool {
	return recordType == "user" && strings.HasPrefix(text, "[Request interrupted by user")
}

// claudeLocalCommand returns the slash command a local command record names,
// such as /model, or "".
func claudeLocalCommand(text string) string {
	_, rest, ok := strings.Cut(text, "<command-name>")
	if !ok {
		return ""
	}
	name, _, _ := strings.Cut(rest, "</command-name>")
	return strings.TrimSpace(name)
}

// claudeStartsBackgroundWork reports a tool call whose result arrives after the
// turn: a background shell command or a Monitor.
func claudeStartsBackgroundWork(content json.RawMessage) bool {
	var blocks []struct {
		Type  string `json:"type"`
		Name  string `json:"name"`
		Input struct {
			RunInBackground bool `json:"run_in_background"`
		} `json:"input"`
	}
	if json.Unmarshal(content, &blocks) != nil {
		return false
	}
	for _, block := range blocks {
		if block.Type == "tool_use" && (block.Input.RunInBackground || block.Name == "Monitor") {
			return true
		}
	}
	return false
}

// claudePointerMatches accepts the exact pointer, plain or inside Claude's
// native bracketed-paste record. It never strips arbitrary markup or prose.
func claudePointerMatches(text, pointer string) bool {
	if text == pointer {
		return true
	}
	rest, ok := strings.CutPrefix(text, "\n\n<pasted_content id=\"")
	if !ok {
		return false
	}
	id, body, ok := strings.Cut(rest, "\">\n")
	if !ok || id == "" {
		return false
	}
	for _, c := range id {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return body == pointer+"\n</pasted_content id=\""+id+"\">\n"
}
