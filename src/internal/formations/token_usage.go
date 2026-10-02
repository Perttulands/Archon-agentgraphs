package formations

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Token limits (archon-o7p.9). A Limit card's tokens knob caps what the work
// it covers spends, by one approximate definition for both harnesses: input
// the model did not read from its cache, writing to the cache included, plus
// every output token, reasoning included, subagents included, counted for
// each dispatch from its pointer.
//
//   - Claude Code: input_tokens + cache_creation_input_tokens + output_tokens
//     of each assistant message after the pointer, once per message id, in the
//     session file and in the subagent files beside it.
//   - Codex: how much the session's total_token_usage grew from its last count
//     before the pointer, input_tokens - cached_input_tokens + output_tokens.
//
// The executor records each dispatch's count as a token_usage event when the
// dispatch ends or is cut, so replay, later attempts and the mission's card
// read the ledger, never transcripts.

// RunEventTokenUsage records the tokens one dispatch spent.
const RunEventTokenUsage = "token_usage"

// ErrTokenBudgetSpent is how an executor reports that the step's seats spent
// the tokens its Limit cards allowed and were stopped.
var ErrTokenBudgetSpent = errors.New("the step spent the tokens its Limit card allows")

// TokenUsage is a dispatch's tokens by kind: Input is input neither read from
// nor written to the cache.
type TokenUsage struct {
	Input      int `json:"input"`
	CacheWrite int `json:"cacheWrite"`
	CacheRead  int `json:"cacheRead"`
	Output     int `json:"output"`
}

// Counted is what a tokens knob counts: uncached input, writing to the cache
// included, plus output.
func (u TokenUsage) Counted() int {
	return u.Input + u.CacheWrite + u.Output
}

func (u TokenUsage) plus(other TokenUsage) TokenUsage {
	return TokenUsage{Input: u.Input + other.Input, CacheWrite: u.CacheWrite + other.CacheWrite, CacheRead: u.CacheRead + other.CacheRead, Output: u.Output + other.Output}
}

// dispatchTokens counts a dispatch's tokens in its native transcript.
func dispatchTokens(harness, path, pointer string) (TokenUsage, error) {
	switch harness {
	case "claude-code":
		return claudeDispatchTokens(path, pointer)
	case "openai-codex":
		return codexDispatchTokens(path, pointer)
	}
	return TokenUsage{}, nil
}

// claudeDispatchTokens sums the assistant messages after the pointer in a
// Claude Code session, and in its subagents' files from the pointer's time on.
// Claude Code repeats a message's usage on each line it streams, so each
// message id counts once, as its last line says.
func claudeDispatchTokens(path, pointer string) (TokenUsage, error) {
	usage, since, err := claudeFileTokens(path, pointer, time.Time{})
	if err != nil || since.IsZero() {
		return usage, err
	}
	subagents, _ := filepath.Glob(filepath.Join(strings.TrimSuffix(path, ".jsonl"), "subagents", "*.jsonl"))
	for _, subagent := range subagents {
		counted, _, err := claudeFileTokens(subagent, "", since)
		if err != nil {
			return usage, err
		}
		usage = usage.plus(counted)
	}
	return usage, nil
}

// claudeFileTokens counts one Claude Code file: after the record that carries
// pointer, or from since on when pointer is empty. It returns the pointer's
// time.
func claudeFileTokens(path, pointer string, since time.Time) (TokenUsage, time.Time, error) {
	file, err := os.Open(path)
	if err != nil {
		return TokenUsage{}, time.Time{}, err
	}
	defer file.Close()
	messages := map[string]TokenUsage{}
	order := []string{}
	counting := pointer == ""
	var started time.Time
	err = scanJSONLines(file, func(line []byte) {
		var record struct {
			Type      string `json:"type"`
			IsMeta    bool   `json:"isMeta"`
			Timestamp string `json:"timestamp"`
			Message   struct {
				ID      string          `json:"id"`
				Content json.RawMessage `json:"content"`
				Usage   *struct {
					Input      int `json:"input_tokens"`
					CacheWrite int `json:"cache_creation_input_tokens"`
					CacheRead  int `json:"cache_read_input_tokens"`
					Output     int `json:"output_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &record) != nil {
			return
		}
		at, _ := time.Parse(time.RFC3339Nano, record.Timestamp)
		if pointer != "" && record.Type == "user" && !record.IsMeta && claudePointerMatches(strings.Join(assistantContentText(record.Message.Content), ""), pointer) {
			// A seat reused for a later attempt takes a new pointer; count from
			// the latest.
			clear(messages)
			order, counting, started = order[:0], true, at
			return
		}
		if !counting || record.Type != "assistant" || record.Message.Usage == nil || record.Message.ID == "" {
			return
		}
		if !since.IsZero() && at.Before(since) {
			return
		}
		if _, seen := messages[record.Message.ID]; !seen {
			order = append(order, record.Message.ID)
		}
		u := record.Message.Usage
		messages[record.Message.ID] = TokenUsage{Input: u.Input, CacheWrite: u.CacheWrite, CacheRead: u.CacheRead, Output: u.Output}
	})
	var usage TokenUsage
	for _, id := range order {
		usage = usage.plus(messages[id])
	}
	return usage, started, err
}

// codexDispatchTokens is how much a Codex session's running total grew from
// its last count before the pointer to its latest count. Codex counts cached
// and cache-written input inside input_tokens and reasoning inside
// output_tokens.
func codexDispatchTokens(path, pointer string) (TokenUsage, error) {
	file, err := os.Open(path)
	if err != nil {
		return TokenUsage{}, err
	}
	defer file.Close()
	var before, latest TokenUsage
	consumed := false
	err = scanJSONLines(file, func(line []byte) {
		var record struct {
			Type    string `json:"type"`
			Payload struct {
				Type    string `json:"type"`
				Role    string `json:"role"`
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
				Info *struct {
					Total struct {
						Input      int `json:"input_tokens"`
						CacheRead  int `json:"cached_input_tokens"`
						CacheWrite int `json:"cache_write_input_tokens"`
						Output     int `json:"output_tokens"`
					} `json:"total_token_usage"`
				} `json:"info"`
			} `json:"payload"`
		}
		if json.Unmarshal(line, &record) != nil {
			return
		}
		p := record.Payload
		switch {
		case record.Type == "response_item" && p.Type == "message" && p.Role == "user":
			text := ""
			for _, part := range p.Content {
				text += part.Text
			}
			if text == pointer {
				consumed, before = true, latest
			}
		case record.Type == "event_msg" && p.Type == "token_count" && p.Info != nil:
			total := p.Info.Total
			latest = TokenUsage{Input: total.Input - total.CacheRead - total.CacheWrite, CacheWrite: total.CacheWrite, CacheRead: total.CacheRead, Output: total.Output}
		}
	})
	if !consumed {
		return TokenUsage{}, err
	}
	return TokenUsage{Input: latest.Input - before.Input, CacheWrite: latest.CacheWrite - before.CacheWrite, CacheRead: latest.CacheRead - before.CacheRead, Output: latest.Output - before.Output}, err
}

// scanJSONLines calls each for every complete line; a line still being
// written at the end is left for the next read.
func scanJSONLines(reader io.Reader, each func([]byte)) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		each(scanner.Bytes())
	}
	return scanner.Err()
}

// tokensUsed is what a card's tokens have counted: every recorded dispatch of
// the step it covers, or of the whole run for the mission.
func tokensUsed(board *BoardDocument, events []RunEvent, limit LimitNode) int {
	mission := isMissionLimit(board, limit)
	used := 0
	for _, event := range events {
		if event.Type == RunEventTokenUsage && (mission || event.NodeID == limit.Target) {
			used += intFromRunEventData(event.Data["tokens"])
		}
	}
	return used
}

// tokensUse is a tokens card's use, or nil when the card sets no tokens.
func tokensUse(board *BoardDocument, events []RunEvent, limit LimitNode) *RunLimitReached {
	if limit.Tokens == nil || *limit.Tokens <= 0 {
		return nil
	}
	granted := limitGranted(events, limit.ID, LimitKindTokens)
	return &RunLimitReached{Kind: LimitKindTokens, LimitID: limit.ID, NodeID: limit.Target, Used: tokensUsed(board, events, limit), Max: *limit.Tokens + granted, Granted: granted}
}

// tokenBudget is the tokens card covering a step that has the fewest left:
// the step's own or the mission's. Nil when neither sets tokens.
func tokenBudget(board *BoardDocument, events []RunEvent, nodeID string) *RunLimitReached {
	var first *RunLimitReached
	for _, limit := range coveringLimits(board, nodeID) {
		if use := tokensUse(board, events, limit); use != nil && (first == nil || use.Max-use.Used < first.Max-first.Used) {
			first = use
		}
	}
	return first
}

// coveringLimits are the cards that cover a step: its own, then the
// mission's.
func coveringLimits(board *BoardDocument, nodeID string) []LimitNode {
	cards := []LimitNode{}
	if limit, ok := limitCovering(board, nodeID); ok {
		cards = append(cards, limit)
	}
	for _, mission := range board.Missions {
		if limit, ok := limitCovering(board, mission.ID); ok {
			cards = append(cards, limit)
		}
	}
	return cards
}

// tokenMeter sums the live counts of one attempt's dispatches and stops the
// attempt once they reach its budget. It is safe for a peer step's seats to
// share.
type tokenMeter struct {
	mu      sync.Mutex
	budget  int
	counts  map[string]int
	spent   bool
	onSpent func()
}

func newTokenMeter(budget int, onSpent func()) *tokenMeter {
	return &tokenMeter{budget: budget, counts: map[string]int{}, onSpent: onSpent}
}

// set records a dispatch's count so far and reports whether the attempt has
// now spent its budget.
func (m *tokenMeter) set(dispatch string, counted int) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	m.counts[dispatch] = counted
	total := 0
	for _, count := range m.counts {
		total += count
	}
	stop := total >= m.budget && !m.spent
	m.spent = m.spent || stop
	m.mu.Unlock()
	if stop {
		m.onSpent()
	}
	return stop
}
