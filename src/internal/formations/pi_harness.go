package formations

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// piHarnessID names the Pi coding agent harness. A Pi slot's model is
// provider/model, as in anthropic/claude-opus-5-5, so one harness reaches every
// provider Pi knows; Pi resolves that provider's credentials itself.
const piHarnessID = "pi"

// DefaultPiTranscriptRoot is where Pi saves sessions by default:
// $PI_CODING_AGENT_DIR/sessions, else ~/.pi/agent/sessions. "" when neither
// can be named.
func DefaultPiTranscriptRoot() string {
	dir := os.Getenv("PI_CODING_AGENT_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".pi", "agent")
	}
	return filepath.Join(dir, "sessions")
}

// piModelObserved says whether the provider/model Pi recorded is the one the
// slot named. Pi resolves a partial name itself, so a slot's name need only be
// part of the recorded one, within the provider when the slot names one.
func piModelObserved(want, observed string) bool {
	want, observed = strings.ToLower(want), strings.ToLower(observed)
	provider, model, scoped := strings.Cut(want, "/")
	if !scoped {
		return strings.Contains(observed, want)
	}
	seenProvider, seenModel, _ := strings.Cut(observed, "/")
	return provider == seenProvider && strings.Contains(seenModel, model)
}

// readPiTurn reads one dispatch from a Pi session file: a header naming the
// exact cwd, the exact pointer as a user message, then every entry after it.
// Only an assistant message that stopped natively (stopReason "stop") and
// carries this run's sentinel completes the dispatch. A later pointer in the
// same file starts the dispatch again, as a revised worker's does.
func readPiTurn(path, cwd, pointer, runID string) (codexTranscriptTurn, error) {
	f, err := os.Open(path)
	if err != nil {
		return codexTranscriptTurn{}, err
	}
	defer f.Close()
	return readPiTurnReader(f, cwd, pointer, runID)
}

type piEntry struct {
	Type          string `json:"type"`
	ID            string `json:"id"`
	Cwd           string `json:"cwd"`
	Provider      string `json:"provider"`
	ModelID       string `json:"modelId"`
	ThinkingLevel string `json:"thinkingLevel"`
	// Usage is what an entry other than a message spent: a compaction's or a
	// branch summary's request, or cache warming.
	Usage   *piUsage `json:"usage"`
	Message struct {
		Role       string          `json:"role"`
		Content    json.RawMessage `json:"content"`
		Provider   string          `json:"provider"`
		Model      string          `json:"model"`
		StopReason string          `json:"stopReason"`
		Usage      *piUsage        `json:"usage"`
	} `json:"message"`
}

type piUsage struct {
	Input      int `json:"input"`
	Output     int `json:"output"`
	CacheRead  int `json:"cacheRead"`
	CacheWrite int `json:"cacheWrite"`
}

func piModelName(provider, model string) string {
	if provider == "" || model == "" {
		return model
	}
	return provider + "/" + model
}

func piIsPointer(entry piEntry, pointer string) bool {
	return entry.Type == "message" && entry.Message.Role == "user" && strings.Join(assistantContentText(entry.Message.Content), "") == pointer
}

func piSameDirectory(a, b string) bool {
	if a == b {
		return true
	}
	resolvedA, errA := filepath.EvalSymlinks(a)
	resolvedB, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && resolvedA == resolvedB
}

func readPiTurnReader(reader io.Reader, cwd, pointer, runID string) (codexTranscriptTurn, error) {
	var turn codexTranscriptTurn
	sessionID, model, effort := "", "", ""
	first := true
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		var entry piEntry
		if json.Unmarshal(scanner.Bytes(), &entry) != nil {
			break // incomplete trailing record
		}
		if first {
			first = false
			if entry.Type != "session" || !piSameDirectory(entry.Cwd, cwd) {
				return codexTranscriptTurn{}, nil
			}
			if entry.ID == "" {
				return codexTranscriptTurn{}, errors.New("Pi session has no native session identity")
			}
			sessionID = entry.ID
			continue
		}
		switch {
		case entry.Type == "model_change":
			next := piModelName(entry.Provider, entry.ModelID)
			if turn.Consumed && turn.Model != "" && next != turn.Model {
				return turn, fmt.Errorf("the Pi model changed during the dispatch from %s to %s", turn.Model, next)
			}
			model = next
			if turn.Consumed {
				turn.Model = next
			}
		case entry.Type == "thinking_level_change":
			if turn.Consumed && turn.Effort != "" && entry.ThinkingLevel != turn.Effort {
				return turn, fmt.Errorf("the Pi effort changed during the dispatch from %s to %s", turn.Effort, entry.ThinkingLevel)
			}
			effort = entry.ThinkingLevel
			if turn.Consumed {
				turn.Effort = effort
			}
		case piIsPointer(entry, pointer):
			turn = codexTranscriptTurn{Consumed: true, SessionID: sessionID, TurnID: entry.ID, Model: model, Effort: effort}
		case !turn.Consumed || entry.Type != "message":
		case entry.Message.Role == "user":
			turn.OperatorTurns++
		case entry.Message.Role == "assistant":
			if turn.takePiAssistant(entry, runID); turn.Complete {
				return turn, nil
			}
		}
	}
	return turn, scanner.Err()
}

func (turn *codexTranscriptTurn) takePiAssistant(entry piEntry, runID string) {
	// The selected model is the latest model_change; a virtual model's messages
	// name whichever physical model answered.
	if turn.Model == "" {
		turn.Model = piModelName(entry.Message.Provider, entry.Message.Model)
	}
	text := strings.Join(assistantContentText(entry.Message.Content), "")
	if text != "" {
		turn.Text = text
	}
	turn.Waiting = ""
	switch entry.Message.StopReason {
	case "stop":
		if _, ok := ParseCompletionSentinel(text, runID); ok {
			turn.Complete = true
			return
		}
		turn.Waiting = SeatStateTurnEndedWithoutSentinel
	case "aborted":
		// Esc in the seat: the operator's, and neither completes nor fails.
		turn.OperatorTurns++
		turn.Text = ""
		turn.Waiting = SeatStateTurnEndedWithoutSentinel
	case "length", "error":
		turn.Text = ""
		turn.Waiting = SeatStateTurnEndedWithoutSentinel
	}
}

// piDispatchTokens sums each assistant message after the latest pointer, and
// the usage of Pi's own requests there, such as compaction. Pi writes a message
// once, and counts cached input apart from input.
func piDispatchTokens(path, pointer string) (TokenUsage, error) {
	file, err := os.Open(path)
	if err != nil {
		return TokenUsage{}, err
	}
	defer file.Close()
	var usage TokenUsage
	counting := false
	err = scanJSONLines(file, func(line []byte) {
		var entry piEntry
		if json.Unmarshal(line, &entry) != nil {
			return
		}
		if piIsPointer(entry, pointer) {
			usage, counting = TokenUsage{}, true
			return
		}
		u := entry.Usage
		if entry.Type == "message" && entry.Message.Role == "assistant" {
			u = entry.Message.Usage
		}
		if counting && u != nil {
			usage = usage.plus(TokenUsage{Input: u.Input, CacheWrite: u.CacheWrite, CacheRead: u.CacheRead, Output: u.Output})
		}
	})
	return usage, err
}

// Pi's editor has no prompt glyph: it is the text between the screen's last two
// rules. A plain rule may name how far a tall editor has scrolled. While Pi
// works it writes its status into the top rule, as in "── ⠋ Working ───".
var piPlainRule = regexp.MustCompile(`^─+( [↑↓] \d+ more ─*)?$`)

// piWorkingLine is the hint on a status Pi draws as its own line.
var piWorkingLine = regexp.MustCompile(`esc(ape)? to (cancel|interrupt)`)

// piEditor reads Pi's editor from a pane capture, styled or plain: what it holds
// as renderedText keeps it, and whether Pi's status says it is working.
func piEditor(screen string) (typed string, working, found bool) {
	lines := strings.Split(ansiSGR.ReplaceAllString(screen, ""), "\n")
	for index := range lines {
		lines[index] = strings.TrimSpace(lines[index])
	}
	bottom := len(lines) - 1
	for bottom >= 0 && !piPlainRule.MatchString(lines[bottom]) {
		bottom--
	}
	for top := bottom - 1; top >= 0; top-- {
		if !strings.HasPrefix(lines[top], "─") || !strings.HasSuffix(lines[top], "─") {
			continue
		}
		working = !piPlainRule.MatchString(lines[top])
		for row, seen := top-1, 0; row >= 0 && seen < 2; row-- {
			if lines[row] != "" {
				seen++
				working = working || tmuxPaneShowsAgentWorking(piHarnessID, lines[row])
			}
		}
		return renderedText(strings.Join(lines[top+1:bottom], "")), working, true
	}
	return "", false, false
}

// piInputClear reports a Pi seat idle with an empty editor.
func piInputClear(screen string) bool {
	typed, working, found := piEditor(screen)
	return found && typed == "" && !working
}

// piReady reports a Pi seat showing its editor with no work under way.
func piReady(captured string) bool {
	_, working, found := piEditor(captured)
	return found && !working
}

// piSessionDir is the directory Pi itself saves a cwd's sessions in under root:
// its default layout, so `pi --resume` there finds a seat's session.
func piSessionDir(root, cwd string) string {
	path := strings.TrimPrefix(filepath.ToSlash(cwd), "/")
	return filepath.Join(root, "--"+strings.NewReplacer("/", "-", "\\", "-", ":", "-").Replace(path)+"--")
}
