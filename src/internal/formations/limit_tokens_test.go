package formations

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// Token limits (archon-o7p.9): a Limit card's tokens knob counts uncached
// input, cache writes included, plus output, subagents included, per dispatch
// from its pointer; at the budget the step stops and the run blocks.

// The counts match sums computed by hand from recorded sessions, trimmed to
// their message records with paths redacted: Claude Code 2.1.287 with one
// subagent, and a codex-cli 0.159.3 session whose dispatch is its second turn.
func TestTokensCountRecordedTranscripts(t *testing.T) {
	dir := filepath.Join("testdata", "transcripts", "tokens")
	claude, err := dispatchTokens("claude-code", filepath.Join(dir, "claude-subagent-session.jsonl"), seatPointer("/work/sa/brief.md"))
	if err != nil {
		t.Fatal(err)
	}
	// Main session 10,474 and its subagent 15,132.
	if want := (TokenUsage{Input: 44, CacheWrite: 24988, CacheRead: 74213, Output: 574}); claude != want || claude.Counted() != 25606 {
		t.Fatalf("claude = %+v (%d), want %+v (25606)", claude, claude.Counted(), want)
	}
	codex, err := dispatchTokens("openai-codex", filepath.Join(dir, "codex-two-turns.jsonl"), seatPointer("/work/cx2/brief.md"))
	if err != nil {
		t.Fatal(err)
	}
	// The session's total grew from 22,318 in and 5 out before the pointer to
	// 73,768 in, 47,616 of them cached, and 71 out.
	if want := (TokenUsage{Input: 3834, CacheRead: 47616, Output: 66}); codex != want || codex.Counted() != 3900 {
		t.Fatalf("codex = %+v (%d), want %+v (3900)", codex, codex.Counted(), want)
	}
	// A pointer the session never took counts nothing.
	if none, err := dispatchTokens("openai-codex", filepath.Join(dir, "codex-two-turns.jsonl"), seatPointer("/work/other.md")); err != nil || none != (TokenUsage{}) {
		t.Fatalf("untaken pointer = %+v, %v", none, err)
	}
}

func TestTokenWords(t *testing.T) {
	for count, want := range map[int]string{1: "1 token", 999: "999 tokens", 1000: "1,000 tokens", 51230: "51,230 tokens", 1234567: "1,234,567 tokens"} {
		if got := tokenWords(count); got != want {
			t.Errorf("tokenWords(%d) = %q, want %q", count, got, want)
		}
	}
}

func addTokensLimit(t *testing.T, store *Store, target string, tokens int) LimitNode {
	t.Helper()
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.CreateLimit("session-search", LimitCreateRequest{Title: "Budget", Target: target, Tokens: tokens}, WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev})
	if err != nil {
		t.Fatal(err)
	}
	return result.Limit
}

// A lab step with "archon-lab-tokens: 15000" in its brief spends that many
// tokens. Over its card's budget the step stops with a plain reason, a resume
// needs a grant, and the grant gives the card's tokens again.
func TestALabStepStopsAtItsTokensAndAGrantGivesThemAgain(t *testing.T) {
	store, personas := s4RunFixture(t)
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), strings.Replace(s4RunBoardFixture(), "title = \"Research\"\n", "title = \"Research\"\n\n[formation.brief]\ngoal = \"Research. archon-lab-tokens: 15000\"\n", 1))
	limit := addTokensLimit(t, store, "fmn_research", 10000)
	lab := NewLabFormationExecutor(store, personas, LabExecutorConfig{Cwd: t.TempDir(), Harnesses: []string{"openai-codex"}})
	engine := NewRunEngine(store, personas, lab)
	status, err := engine.RunMission("session-search", RunStartRequest{MissionID: "mis_showcase", Actor: "agent:test"})
	if err != nil || status.Status != RunStatusBlocked || status.ResumePolicy != ResumePolicyGrant {
		t.Fatalf("status = %+v, %v; want a block that takes a grant", status, err)
	}
	events := mustEvents(t, store, status.RunID)
	block := events[len(events)-1]
	if block.Data["reason"] != "Research used 15,000 of 10,000 tokens" || block.Data["code"] != RunBlockLimitReached {
		t.Fatalf("block = %+v", block.Data)
	}
	want := RunLimitReached{Kind: LimitKindTokens, LimitID: limit.ID, NodeID: "fmn_research", Used: 15000, Max: 10000}
	if got := runLimitReached(events, len(events)-1); got == nil || *got != want {
		t.Fatalf("limit = %+v, want %+v", got, want)
	}
	if GrantWords(want) != "10,000 tokens more" || SpentWords(want) != "all 10,000 tokens it may spend" {
		t.Fatalf("words = %q, %q", GrantWords(want), SpentWords(want))
	}
	if _, err := engine.ResumeRun(status.RunID, RunResumeRequest{Actor: "agent:test", Mode: "reattach"}); !errors.Is(err, ErrRunGrantRequired) {
		t.Fatalf("resume without a grant: %v", err)
	}
	status, err = engine.ResumeRun(status.RunID, RunResumeRequest{Actor: "human:perttu", Mode: "reattach", Grant: true})
	if err != nil || status.Status != RunStatusBlocked {
		t.Fatalf("after the grant = %+v, %v", status, err)
	}
	events = mustEvents(t, store, status.RunID)
	resumed := lastEventOfType(t, events, RunEventResumed)
	if !reflect.DeepEqual(resumed.Data["grant"], map[string]any{"limitId": limit.ID, "kind": "tokens", "amount": float64(10000)}) {
		t.Fatalf("grant = %+v", resumed.Data["grant"])
	}
	if got := nodeStartedAttempts(events, "fmn_research"); !reflect.DeepEqual(got, []int{1, 2}) {
		t.Fatalf("attempts = %v", got)
	}
	if reason := events[len(events)-1].Data["reason"]; reason != "Research used 30,000 of 20,000 tokens, 10,000 of them granted" {
		t.Fatalf("second block reason = %v", reason)
	}
}

// tokenSpendingExecutor spends tokens per dispatch as the executor contract
// says: it records token_usage, and stops the step at its budget.
type tokenSpendingExecutor struct {
	fakeRunExecutor
	store   *Store
	tokens  int
	budgets []int
}

func (f *tokenSpendingExecutor) ExecuteFormation(req FormationExecution) (FormationExecutionResult, error) {
	f.budgets = append(f.budgets, req.TokenBudget)
	if err := f.store.AppendRunEvent(req.RunID, RunEvent{Type: RunEventTokenUsage, NodeID: req.NodeID, Attempt: req.Attempt, Data: map[string]any{"tokens": f.tokens}}); err != nil {
		return FormationExecutionResult{}, err
	}
	if req.TokenBudget > 0 && f.tokens >= req.TokenBudget {
		f.calls = append(f.calls, req)
		return FormationExecutionResult{}, ErrTokenBudgetSpent
	}
	return f.fakeRunExecutor.ExecuteFormation(req)
}

// The mission's card counts every step's tokens, send-backs included: each
// step starts with what the mission has left and stops when it spends that.
func TestAMissionTokensCardCountsEveryStep(t *testing.T) {
	store, personas := s4RunFixture(t)
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), s4GateBoardFixture(true))
	limit := addTokensLimit(t, store, "mis_showcase", 10000)
	executor := &tokenSpendingExecutor{store: store, tokens: 4000}
	engine := NewRunEngine(store, personas, executor)
	engine.SetGateEvaluator(&fakeGateEvaluator{verdicts: []string{"fail", "fail", "pass"}})
	status, err := engine.RunMission("session-search", RunStartRequest{MissionID: "mis_showcase", Actor: "agent:test"})
	if err != nil || status.Status != RunStatusBlocked || status.ResumePolicy != ResumePolicyGrant {
		t.Fatalf("status = %+v, %v", status, err)
	}
	if !reflect.DeepEqual(executor.budgets, []int{10000, 6000, 2000}) {
		t.Fatalf("budgets = %v, want what the mission had left at each start", executor.budgets)
	}
	events := mustEvents(t, store, status.RunID)
	block := events[len(events)-1]
	if block.Data["reason"] != "The mission used 12,000 of 10,000 tokens" || block.NodeID != "fmn_work" {
		t.Fatalf("block = %+v", block)
	}
	if use := tokensUse(mustReadRunBoard(t, store, status.RunID), events, limit); use == nil || use.Used != 12000 || use.Max != 10000 {
		t.Fatalf("use = %+v", use)
	}
	// The grant gives the card's 10,000 again: Work runs on 8,000, the gate
	// passes, and Ship stops on the 4,000 left.
	status, err = engine.ResumeRun(status.RunID, RunResumeRequest{Actor: "agent:test", Mode: "reattach", Grant: true})
	if err != nil || status.Status != RunStatusBlocked {
		t.Fatalf("after the grant = %+v, %v", status, err)
	}
	if !reflect.DeepEqual(executor.budgets, []int{10000, 6000, 2000, 8000, 4000}) {
		t.Fatalf("budgets after a grant = %v", executor.budgets)
	}
	events = mustEvents(t, store, status.RunID)
	if block := events[len(events)-1]; block.Data["reason"] != "The mission used 20,000 of 20,000 tokens, 10,000 of them granted" || block.NodeID != "fmn_ship" {
		t.Fatalf("second block = %+v", block)
	}
}

// spendingSeats writes each seat's native Codex rollout as its turn runs, the
// session's running total past the budget, and holds the turn until the
// attempt stops.
type spendingSeats struct {
	*fakeTmuxHarnessClient
	dir    string
	tokens int
	mu     sync.Mutex
	paths  map[string]string
}

func (s *spendingSeats) TranscriptPath(seat *nativeSeat, _, _ string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.paths[seat.name], nil
}

func (s *spendingSeats) WaitTurn(ctx context.Context, seat *nativeSeat, cwd, pointer string, consumed func(codexTranscriptTurn) error) (codexTranscriptTurn, error) {
	turn := codexTranscriptTurn{Consumed: true, SessionID: "native-" + seat.name}
	if err := consumed(turn); err != nil {
		return turn, err
	}
	path := filepath.Join(s.dir, seat.name+".jsonl")
	user := fmt.Sprintf(`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":%q}]}}`, pointer)
	count := fmt.Sprintf(`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":%d,"cached_input_tokens":9000,"cache_write_input_tokens":0,"output_tokens":0}}}}`, s.tokens+9000)
	if err := os.WriteFile(path, []byte(user+"\n"+count+"\n"), 0o600); err != nil {
		return turn, err
	}
	s.mu.Lock()
	s.paths[seat.name] = path
	s.mu.Unlock()
	select {
	case <-ctx.Done():
		return turn, ctx.Err()
	case <-time.After(testPatience):
		return turn, errors.New("the seat was never stopped")
	}
}

// A real seat whose transcript passes the step's budget is stopped while it
// works; the ledger records what it spent and the run blocks on the card.
func TestATmuxSeatStopsWhenItsTokensReachTheBudget(t *testing.T) {
	poll := tokenPoll
	tokenPoll = 10 * time.Millisecond
	t.Cleanup(func() { tokenPoll = poll })
	board := s4RunBoardFixture() + "\n[[limit]]\nid = \"lim_research\"\ntitle = \"Research budget\"\ntarget = \"fmn_research\"\ntokens = 5000\n"
	store, personas := s4RunFixture(t)
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), board)
	cfg := tmuxTestConfig(t)
	seats := &spendingSeats{fakeTmuxHarnessClient: &fakeTmuxHarnessClient{pane: tmuxPaneState{CurrentPath: cfg.Cwd}}, dir: t.TempDir(), tokens: 7000, paths: map[string]string{}}
	executor := newTmuxFormationExecutorWithClient(store, personas, cfg, seats)
	status, err := NewRunEngine(store, personas, executor).RunFormation("session-search", "fmn_research", FormationRunRequest{Actor: "agent:test"})
	if err != nil || status.Status != RunStatusBlocked || status.ResumePolicy != ResumePolicyGrant {
		t.Fatalf("status = %+v, %v", status, err)
	}
	events := mustEvents(t, store, status.RunID)
	usage := lastEventOfType(t, events, RunEventTokenUsage)
	if intFromRunEventData(usage.Data["tokens"]) != 7000 || usage.NodeID != "fmn_research" || stringFromEventData(usage, "dispatchId") == "" {
		t.Fatalf("token_usage = %+v", usage)
	}
	if reason := events[len(events)-1].Data["reason"]; reason != "Research used 7,000 of 5,000 tokens" {
		t.Fatalf("block reason = %v: %s", reason, eventTypeTrail(events))
	}
}
