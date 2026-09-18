package formations

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Perttulands/Archon-agentgraphs/internal/filewatch"
)

// The transcripts in testdata/seats were captured from real seats on
// 2026-09-17: Claude Code 2.1.274 and codex-cli 0.154.0 in tmux 3.6a, at low
// effort, with synthetic briefs and operator messages, and paths redacted to
// /work. Claude marks typed input origin.kind=human and promptSource=typed, and
// records a message queued while the agent works as a queue-operation plus a
// queued_command attachment with origin.kind=human; skill text is isMeta and
// task notifications carry their own origin. Codex marks typed input with the
// content kind user.text and injects context under other kinds, such as
// environments.environment_context. An interrupt is Claude's "[Request
// interrupted by user" record and Codex's turn_aborted event.
const fixtureRunID = "run_fixture"

func fixturePointer(brief string) string {
	return seatPointer("/work/briefs/" + brief + ".md")
}

// fixtureOutcome replays a captured transcript one record at a time, as the
// seat's transcript grows, and returns the reading at the first record where
// the dispatch completes or fails, and that record's line number. A dispatch
// that neither completes nor fails returns the reading of the whole file and 0.
func fixtureOutcome(t *testing.T, harness, name, brief string) (codexTranscriptTurn, int, error) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "seats", name+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.SplitAfter(string(raw), "\n")
	seat := &nativeSeat{variant: HarnessVariant{ID: harness}, runID: fixtureRunID}
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	var turn codexTranscriptTurn
	for index := range lines {
		if err := os.WriteFile(path, []byte(strings.Join(lines[:index+1], "")), 0600); err != nil {
			t.Fatal(err)
		}
		turn, err = readLatestSeatTurn(seat, path, "/work", fixturePointer(brief))
		if err != nil || turn.Complete {
			return turn, index + 1, err
		}
	}
	return turn, 0, nil
}

// lineOf returns the 1-based line of the last record containing text.
func lineOf(t *testing.T, name, text string) int {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "seats", name+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	line := 0
	for index, record := range strings.Split(string(raw), "\n") {
		if strings.Contains(record, text) {
			line = index + 1
		}
	}
	if line == 0 {
		t.Fatalf("%s has no record containing %q", name, text)
	}
	return line
}

var fixtureHarnesses = []struct{ id, name string }{{"claude-code", "claude"}, {"openai-codex", "codex"}}

func TestOperatorTurnsNeitherCompleteNorFailADispatch(t *testing.T) {
	for _, harness := range fixtureHarnesses {
		for _, fixture := range []struct {
			name, brief, operator string
		}{
			// The operator types a question while the agent runs a command.
			{map[string]string{"claude": "claude-operator-queued-mid-tool", "codex": "codex-operator-steers-mid-turn"}[harness.name], map[string]string{"claude": "midtool", "codex": "midturn"}[harness.name], "Operator question"},
			// The agent has written an answer, the operator says go, and only then does it print the sentinel.
			{harness.name + "-operator-after-answer", "afteranswer", `"go"`},
		} {
			t.Run(fixture.name, func(t *testing.T) {
				turn, line, err := fixtureOutcome(t, harness.id, fixture.name, fixture.brief)
				if err != nil || !turn.Complete {
					t.Fatalf("turn %+v, err %v; want the later sentinel turn to complete the dispatch", turn, err)
				}
				if _, ok := ParseCompletionSentinel(turn.Text, fixtureRunID); !ok || turn.OperatorTurns == 0 {
					t.Fatalf("completed turn %+v; want this run's sentinel after an operator turn", turn)
				}
				if operator := lineOf(t, fixture.name, fixture.operator); line <= operator {
					t.Fatalf("completed at line %d, not after the operator's message at line %d", line, operator)
				}
			})
		}
	}
	for _, harness := range fixtureHarnesses {
		name := harness.name + "-operator-chat-then-sentinel"
		t.Run(name, func(t *testing.T) {
			// The operator interrupts, asks a question and gets an answer that ends
			// its turn without the sentinel; the dispatch waits, and completes when
			// the agent later prints the sentinel.
			turn, line, err := fixtureOutcome(t, harness.id, name, "chat")
			if err != nil || !turn.Complete || turn.OperatorTurns < 2 {
				t.Fatalf("turn %+v, err %v; want completion after the operator's turns", turn, err)
			}
			if _, ok := ParseCompletionSentinel(turn.Text, fixtureRunID); !ok {
				t.Fatalf("completed turn %+v without this run's sentinel", turn)
			}
			if reply := lineOf(t, name, "I was about to"); line <= reply {
				t.Fatalf("completed at line %d; the answer to the operator is at line %d", line, reply)
			}
		})
	}
	t.Run("claude-background-then-operator", func(t *testing.T) {
		// The agent backgrounds a command and ends its turn without the sentinel;
		// Claude Code resumes when the command reports, so that turn is not final.
		turn, line, err := fixtureOutcome(t, "claude-code", "claude-background-then-operator", "midturn")
		if err != nil || !turn.Complete || line != lineOf(t, "claude-background-then-operator", "<<<CHROTE-DONE run-id=run_fixture") {
			t.Fatalf("turn %+v at line %d, err %v; want completion only at the sentinel", turn, line, err)
		}
	})
}

func TestAFinishedTurnWithoutTheSentinelFailsFastWhenNobodyElseTookATurn(t *testing.T) {
	for _, harness := range fixtureHarnesses {
		t.Run(harness.name, func(t *testing.T) {
			name := harness.name + "-no-sentinel"
			turn, line, err := fixtureOutcome(t, harness.id, name, "nosentinel")
			if err != nil || !turn.Complete || turn.OperatorTurns != 0 || strings.TrimSpace(turn.Text) != "hello" {
				t.Fatalf("turn %+v, err %v; want the finished answer handed back at once", turn, err)
			}
			// Claude returns on its end_turn record; Codex on task_complete.
			end := map[string]string{"claude": `"stop_reason":"end_turn"`, "codex": `"task_complete"`}[harness.name]
			if want := lineOf(t, name, end); line != want {
				t.Fatalf("returned at line %d, want the native turn end at line %d", line, want)
			}
			if _, ok := ParseCompletionSentinel(turn.Text, fixtureRunID); ok {
				t.Fatal("an answer without the sentinel carried one")
			}
		})
	}
}

func TestAnotherRunsSentinelDoesNotCompleteADispatch(t *testing.T) {
	for _, harness := range fixtureHarnesses {
		t.Run(harness.name, func(t *testing.T) {
			turn, _, err := fixtureOutcome(t, harness.id, harness.name+"-other-run-sentinel", "otherrun")
			if err != nil || !turn.Complete || !strings.Contains(turn.Text, "run-id=run_other") {
				t.Fatalf("turn %+v, err %v", turn, err)
			}
			// The finished turn goes back to the executor, whose completion check
			// rejects an answer without this run's sentinel (see
			// TestS4CompletionSentinelRequiresMatchingRunID).
			if _, ok := ParseCompletionSentinel(turn.Text, fixtureRunID); ok {
				t.Fatal("another run's sentinel completed this run's dispatch")
			}
		})
	}
}

func TestAModelSwitchFailsTheDispatchLoudly(t *testing.T) {
	for _, harness := range fixtureHarnesses {
		t.Run(harness.name, func(t *testing.T) {
			name := harness.name + "-model-switch"
			turn, line, err := fixtureOutcome(t, harness.id, name, "model")
			if err == nil || !strings.Contains(err.Error(), "model") {
				t.Fatalf("turn %+v, err %v; want a model change error", turn, err)
			}
			marker := map[string]string{"claude": "<command-name>/model</command-name>", "codex": "model_switch.instructions"}[harness.name]
			if want := lineOf(t, name, marker); line != want {
				t.Fatalf("failed at line %d, want the switch at line %d", line, want)
			}
			if turn.OperatorTurns == 0 {
				t.Fatalf("turn %+v; the operator's interrupt before the switch was not counted", turn)
			}
		})
	}
}

func TestAReplacedConversationFailsTheDispatchLoudly(t *testing.T) {
	for _, harness := range fixtureHarnesses {
		t.Run(harness.name, func(t *testing.T) {
			old := map[string]string{"claude": "claude-clear-old-session", "codex": "codex-new-conversation-old-thread"}[harness.name]
			turn, line, err := fixtureOutcome(t, harness.id, old, "clear")
			// The old transcript only shows the operator's interrupt; the new
			// conversation is found through the seat's process.
			if err != nil || turn.Complete || line != 0 || turn.OperatorTurns == 0 || !turn.Consumed {
				t.Fatalf("old conversation: turn %+v at line %d, err %v", turn, line, err)
			}
			root := filepath.Join(t.TempDir(), "transcripts")
			if err := os.MkdirAll(root, 0700); err != nil {
				t.Fatal(err)
			}
			proc := t.TempDir()
			seat := &nativeSeat{variant: HarnessVariant{ID: harness.id}, root: root, pid: 4242}
			transport := realSeatTransport{proc: proc}
			switch harness.name {
			case "claude":
				// Claude Code records each process's current session; after /clear it names the new one.
				raw, err := os.ReadFile(filepath.Join("testdata", "seats", "claude-sessions-pid.json"))
				if err != nil {
					t.Fatal(err)
				}
				sessions := filepath.Join(filepath.Dir(root), "sessions")
				if err := os.MkdirAll(sessions, 0700); err != nil {
					t.Fatal(err)
				}
				unchanged := strings.Replace(string(raw), "bf6ec72c-eba8-4907-8d40-4166b881868b", turn.SessionID, 1)
				if err := os.WriteFile(filepath.Join(sessions, "4242.json"), []byte(unchanged), 0600); err != nil {
					t.Fatal(err)
				}
				if err := transport.conversationReplaced(context.Background(), seat, turn); err != nil {
					t.Fatalf("the dispatched conversation counted as replaced: %v", err)
				}
				if err := os.WriteFile(filepath.Join(sessions, "4242.json"), raw, 0600); err != nil {
					t.Fatal(err)
				}
			case "codex":
				// Codex keeps the old rollout open and opens the new thread's rollout.
				fds := filepath.Join(proc, "4242", "fd")
				fdinfo := filepath.Join(proc, "4242", "fdinfo")
				if err := os.MkdirAll(fdinfo, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(fds, 0700); err != nil {
					t.Fatal(err)
				}
				link := func(fd, fixture string) string {
					path := filepath.Join(root, fixture+".jsonl")
					raw, err := os.ReadFile(filepath.Join("testdata", "seats", fixture+".jsonl"))
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, raw, 0600); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(path, filepath.Join(fds, fd)); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(fdinfo, fd), []byte("flags:\t02102002\n"), 0600); err != nil {
						t.Fatal(err)
					}
					return path
				}
				oldPath := link("46", old)
				transport.recordConversation(context.Background(), seat, oldPath)
				if err := transport.conversationReplaced(context.Background(), seat, turn); err != nil {
					t.Fatalf("the dispatched conversation counted as replaced: %v", err)
				}
				link("67", "codex-new-conversation-new-thread")
			}
			err = transport.conversationReplaced(context.Background(), seat, turn)
			var executionErr *RunExecutionError
			if !errors.As(err, &executionErr) || executionErr.Code != "conversation_replaced" {
				t.Fatalf("replaced conversation: %v", err)
			}
		})
	}
}

func TestWaitTurnFailsWhenTheSeatEnds(t *testing.T) {
	root := t.TempDir()
	raw, err := os.ReadFile(filepath.Join("testdata", "seats", "claude-operator-chat-then-sentinel.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	// The pointer and the start of the agent's work, then the harness exits.
	lines := strings.SplitAfter(string(raw), "\n")
	if err := os.WriteFile(filepath.Join(root, "seat.jsonl"), []byte(strings.Join(lines[:4], "")), 0600); err != nil {
		t.Fatal(err)
	}
	watch, err := filewatch.New(root)
	if err != nil {
		t.Fatal(err)
	}
	events := make(chan struct{})
	close(events)
	seat := &nativeSeat{variant: HarnessVariant{ID: "claude-code"}, runID: fixtureRunID, root: root, watch: watch, control: &seatControl{events: events}}
	defer seat.close()
	transport := realSeatTransport{command: func(context.Context, string, *strings.Reader, ...string) (string, error) {
		return "", errors.New("no tmux")
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	consumed := 0
	_, err = transport.WaitTurn(ctx, seat, "/work", fixturePointer("chat"), func(codexTranscriptTurn) error { consumed++; return nil })
	var executionErr *RunExecutionError
	if !errors.As(err, &executionErr) || executionErr.Code != "dead_pane" || consumed != 1 {
		t.Fatalf("WaitTurn after the seat ended: %v (consumed %d)", err, consumed)
	}
}

func TestCodexHistoryReadDoesNotReplaceTheDispatchedConversation(t *testing.T) {
	root := t.TempDir()
	writeRollout := func(name, id, cwd string) string {
		path := filepath.Join(root, name+".jsonl")
		meta := fmt.Sprintf("{\"type\":\"session_meta\",\"payload\":{\"id\":%q,\"cwd\":%q,\"source\":\"cli\",\"thread_source\":\"user\"}}\n", id, cwd)
		if err := os.WriteFile(path, []byte(meta), 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	current := writeRollout("current", "current-session", "/work/mission")
	history := writeRollout("history", "previous-session", "/work/unrelated")
	writer, err := os.OpenFile(current, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	seat := &nativeSeat{variant: HarnessVariant{ID: "openai-codex"}, root: root, pid: os.Getpid()}
	transport := realSeatTransport{}
	transport.recordConversation(context.Background(), seat, current)
	// The live smoke showed Codex opening an unrelated historical user rollout
	// during startup. A read does not move the seat to that conversation.
	reader, err := os.Open(history)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	turn := codexTranscriptTurn{SessionID: "current-session", Consumed: true}
	if err := transport.conversationReplaced(context.Background(), seat, turn); err != nil {
		t.Fatalf("read-only history access replaced the dispatched conversation: %v", err)
	}
	// Resuming that same history for writing must still fail the dispatch.
	for _, mode := range []int{os.O_WRONLY, os.O_RDWR} {
		resumed, err := os.OpenFile(history, mode|os.O_APPEND, 0)
		if err != nil {
			t.Fatal(err)
		}
		var executionErr *RunExecutionError
		err = transport.conversationReplaced(context.Background(), seat, turn)
		resumed.Close()
		if !errors.As(err, &executionErr) || executionErr.Code != "conversation_replaced" {
			t.Fatalf("resumed conversation with mode %d was not detected: %v", mode, err)
		}
	}
}
