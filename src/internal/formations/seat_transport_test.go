package formations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func nativeFixture(harness, cwd, pointer, text string) string {
	q := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	if harness == "claude-code" {
		return fmt.Sprintf("{\"type\":\"user\",\"cwd\":%s,\"sessionId\":\"native\",\"uuid\":\"turn\",\"message\":{\"content\":%s}}\n{\"type\":\"assistant\",\"cwd\":%s,\"sessionId\":\"native\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":%s}],\"model\":\"test-model\",\"stop_reason\":\"end_turn\"}}\n", q(cwd), q(pointer), q(cwd), q(text))
	}
	return fmt.Sprintf("{\"type\":\"session_meta\",\"payload\":{\"id\":\"native\",\"cwd\":%s}}\n{\"type\":\"turn_context\",\"payload\":{\"turn_id\":\"turn\",\"model\":\"test-model\",\"effort\":\"medium\"}}\n{\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"text\":%s}]}}\n{\"type\":\"response_item\",\"payload\":{\"type\":\"message\",\"role\":\"assistant\",\"phase\":\"final_answer\",\"content\":[{\"text\":%s}]}}\n{\"type\":\"event_msg\",\"payload\":{\"type\":\"task_complete\",\"turn_id\":\"turn\"}}\n", q(cwd), q(pointer), q(text))
}

func TestSeatAdaptersReadinessStagingCompletionAndImmutableCleanup(t *testing.T) {
	for _, h := range []string{"claude-code", "openai-codex"} {
		t.Run(h, func(t *testing.T) {
			root := t.TempDir()
			for _, bin := range []string{"codex", "claude"} {
				if err := os.WriteFile(filepath.Join(root, bin), []byte("#!/bin/sh\n"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("PATH", root)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			events := make(chan struct{}, 10)
			for i := 0; i < 5; i++ {
				events <- struct{}{}
			}
			phase, captures, submits := "startup", 0, 0
			var createdLaunch, loaded, killed string
			transport := realSeatTransport{
				control: func(context.Context, string, string) (*seatControl, error) { return &seatControl{events: events}, nil },
				command: func(_ context.Context, _ string, input *strings.Reader, args ...string) (string, error) {
					switch args[0] {
					case "new-session":
						createdLaunch = args[len(args)-1]
						return "$42 %23", nil
					case "display-message":
						if strings.Contains(args[len(args)-1], "cursor_x") {
							return "2 1 0", nil
						}
						return "0", nil
					case "capture-pane":
						if phase == "staged" {
							return loaded, nil
						}
						captures++
						if captures == 1 {
							return "starting", nil
						}
						if h == "claude-code" {
							return "Claude Code\n❯", nil
						}
						return "OpenAI Codex\n›", nil
					case "load-buffer":
						b, _ := io.ReadAll(input)
						loaded = string(b)
						return "", nil
					case "paste-buffer":
						phase = "staged"
						return "", nil
					case "send-keys":
						submits++
						final := "answer\n<<<CHROTE-DONE run-id=run_test status=ok artifact=report>>>"
						return "", os.WriteFile(filepath.Join(root, "native.jsonl"), []byte(nativeFixture(h, root, loaded, final)), 0600)
					case "kill-session":
						killed = args[len(args)-1]
						return "", nil
					}
					return "", fmt.Errorf("unexpected command %v", args)
				}}
			seat, err := transport.Create(ctx, "socket", "form-proof-worker", root, root, HarnessVariant{ID: h, Model: "test-model"})
			if err != nil {
				t.Fatal(err)
			}
			defer seat.close()
			if err := transport.Ready(ctx, "socket", seat, h); err != nil {
				t.Fatal(err)
			}
			seat.brief = filepath.Join(root, "brief.md")
			pointer := seatPointer(seat.brief)
			if err := transport.Stage(ctx, "socket", seat, "dispatch", pointer); err != nil {
				t.Fatal(err)
			}
			consumed := 0
			turn, err := transport.WaitTurn(ctx, seat, root, pointer, func(codexTranscriptTurn) error { consumed++; return nil })
			if err != nil {
				t.Fatal(err)
			}
			if !turn.Complete || consumed != 1 || submits != 1 || loaded != pointer || captures < 2 {
				t.Fatalf("turn=%+v consumed=%d submits=%d loaded=%q captures=%d", turn, consumed, submits, loaded, captures)
			}
			if err := seat.variant.verifyTurnSettings(turn); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(createdLaunch, "medium") {
				t.Fatalf("launch=%s", createdLaunch)
			}
			if err := transport.End(ctx, "socket", seat); err != nil {
				t.Fatal(err)
			}
			seat.watch = nil
			seat.control = nil
			if killed != "$42" {
				t.Fatalf("cleanup target=%q", killed)
			}
		})
	}
}

func TestClaudeNativeTurnIdentityAndEndTurn(t *testing.T) {
	for _, kind := range []string{"complete", "wrong cwd", "cwd moves after pointer", "meta record after pointer", "task notification after pointer", "wrong pointer", "tool use", "partial", "no stop reason", "wrong session"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			pointer := seatPointer(filepath.Join(root, "brief.md"))
			final := "answer\n<<<CHROTE-DONE run-id=run_test status=ok artifact=result>>>"
			raw := nativeFixture("claude-code", root, pointer, final)
			switch kind {
			case "wrong cwd":
				raw = strings.ReplaceAll(raw, root, root+"-other")
			case "meta record after pointer":
				// Claude Code injected skill text as a meta user record mid-turn.
				meta := "{\"type\":\"user\",\"isMeta\":true,\"cwd\":\"" + root + "\",\"sessionId\":\"native\",\"message\":{\"content\":\"Base directory for this skill: skills/brainstorm\"}}\n"
				i := strings.Index(raw, "{\"type\":\"assistant\"")
				raw = raw[:i] + meta + raw[i:]
			case "task notification after pointer":
				// A Monitor set by the seat delivers events as system-origin user records.
				note := "{\"type\":\"user\",\"origin\":{\"kind\":\"task-notification\"},\"promptSource\":\"system\",\"cwd\":\"" + root + "\",\"sessionId\":\"native\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"<task-notification>worker done</task-notification>\"}]}}\n"
				i := strings.Index(raw, "{\"type\":\"assistant\"")
				raw = raw[:i] + note + raw[i:]
			case "cwd moves after pointer":
				// The seat changed directory while working; later records carry the new cwd.
				i := strings.LastIndex(raw, "\"cwd\":\""+root+"\"")
				raw = raw[:i] + strings.Replace(raw[i:], "\""+root+"\"", "\""+root+"/src\"", 1)
			case "wrong pointer":
				raw = strings.Replace(raw, "whole brief", "other brief", 1)
			case "tool use":
				raw = strings.Replace(raw, "end_turn", "tool_use", 1)
			case "partial":
				raw = raw[:len(raw)-20]
			case "no stop reason":
				raw = strings.Replace(raw, ",\"stop_reason\":\"end_turn\"", "", 1)
			case "wrong session":
				i := strings.LastIndex(raw, "\"sessionId\":\"native\"")
				raw = raw[:i] + strings.Replace(raw[i:], "native", "other", 1)
			}
			path := filepath.Join(root, "turn.jsonl")
			if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			turn, err := readClaudeTurn(path, root, pointer)
			want := kind == "complete" || kind == "no stop reason" || kind == "cwd moves after pointer" || kind == "meta record after pointer" || kind == "task notification after pointer"
			if turn.Complete != want {
				t.Fatalf("turn=%+v err=%v", turn, err)
			}
			if kind == "wrong session" && err == nil {
				t.Fatal("changed identity accepted")
			}
		})
	}
}

type observationTransport struct {
	realSeatTransport
	turns map[string]codexTranscriptTurn
	errs  map[string]error
}

func (t observationTransport) Snapshot(_ context.Context, s *nativeSeat, _, _ string) (codexTranscriptTurn, error) {
	return t.turns[s.name], t.errs[s.name]
}

func TestWorkerObservationsUseNativeTurnsAndFailSettingsMismatch(t *testing.T) {
	for _, kind := range []string{"complete", "never prompted", "incomplete", "missing", "mismatch"} {
		t.Run(kind, func(t *testing.T) {
			store, started := startS4DispatchRun(t)
			final := "worker result\n<<<CHROTE-DONE run-id=" + started.RunID + " status=ok artifact=report>>>"
			turn := codexTranscriptTurn{Consumed: true, Complete: true, SessionID: "native-worker", TurnID: "turn", Text: final, Model: "test-model", Effort: "medium"}
			want := workerOutcomeOutputCaptured
			errs := map[string]error{}
			switch kind {
			case "never prompted":
				turn = codexTranscriptTurn{}
				want = workerOutcomeNeverTouched
			case "incomplete":
				turn.Complete = false
				want = workerOutcomeHangTimeout
			case "missing":
				errs["worker-a"] = errTmuxTargetMissing
				want = workerOutcomeMissingSession
			case "mismatch":
				turn.Effort = "high"
				want = "settings_mismatch"
			}
			e := NewTmuxFormationExecutor(store, nil, TmuxExecutorConfig{})
			e.seatClient = observationTransport{turns: map[string]codexTranscriptTurn{"worker-a": turn, "worker-b": turn}, errs: errs}
			var baselines []workerBaseline
			for _, name := range []string{"worker-a", "worker-b"} {
				v := HarnessVariant{ID: "openai-codex", Model: "test-model"}
				baselines = append(baselines, workerBaseline{binding: tmuxSlotBinding{Slot: FormationSlot{ID: name}, Variant: v, SessionName: name}, seat: &nativeSeat{name: name, pointer: seatPointer(name)}})
			}
			err := e.observeWorkerOutcomes(context.Background(), FormationExecution{RunID: started.RunID, NodeID: "fmn_research"}, tmuxSlotBinding{}, baselines)
			if (err != nil) != (kind == "mismatch") {
				t.Fatalf("err=%v", err)
			}
			events, err := store.ReadRunEvents(started.RunID)
			if err != nil {
				t.Fatal(err)
			}
			observations := 0
			for _, event := range events {
				if event.Type == RunEventWorkerObservation {
					observations++
					if event.SlotID == "worker-a" && event.Data["outcome"] != want {
						t.Fatalf("event=%+v", event)
					}
					if event.Data["observer"] != "archon-native-transcript" {
						t.Fatalf("wrong provenance %+v", event)
					}
				}
			}
			if observations != 2 {
				t.Fatalf("observations=%d", observations)
			}
		})
	}
}

func TestLatestWorkerTurnDoesNotReuseEarlierCompletion(t *testing.T) {
	for _, h := range []string{"claude-code", "openai-codex"} {
		for _, complete := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/complete=%v", h, complete), func(t *testing.T) {
				root := t.TempDir()
				pointer := seatPointer(filepath.Join(root, "worker.md"))
				first := nativeFixture(h, root, pointer, "first\n<<<CHROTE-DONE run-id=run_test status=ok artifact=first>>>")
				second := strings.ReplaceAll(nativeFixture(h, root, pointer, "second\n<<<CHROTE-DONE run-id=run_test status=ok artifact=second>>>"), "\"turn\"", "\"turn-two\"")
				lines := splitJSONLines([]byte(second))
				var tail [][]byte
				if h == "openai-codex" {
					// A reused TUI can persist its user record before the new turn context.
					tail = [][]byte{lines[2], lines[1]}
					if complete {
						tail = append(tail, lines[3:]...)
					}
				} else {
					tail = lines[:1]
					if complete {
						tail = lines
					}
				}
				path := filepath.Join(root, "native.jsonl")
				if err := os.WriteFile(path, append([]byte(first), bytes.Join(tail, []byte("\n"))...), 0600); err != nil {
					t.Fatal(err)
				}
				turn, err := readLatestSeatTurn(&nativeSeat{variant: HarnessVariant{ID: h}}, path, root, pointer)
				if err != nil {
					t.Fatal(err)
				}
				if !turn.Consumed || turn.Complete != complete {
					t.Fatalf("turn=%+v", turn)
				}
				if complete && (!strings.HasPrefix(turn.Text, "second") || turn.TurnID != "turn-two") {
					t.Fatalf("stale turn=%+v", turn)
				}
			})
		}
	}
}

func TestSeatTrustBootstrapFinishesBeforeReadiness(t *testing.T) {
	for _, h := range []string{"claude-code", "openai-codex"} {
		t.Run(h, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			events := make(chan struct{}, 8)
			for i := 0; i < 8; i++ {
				events <- struct{}{}
			}
			phase := 0
			var keys []string
			transport := realSeatTransport{control: func(context.Context, string, string) (*seatControl, error) { return &seatControl{events: events}, nil }, command: func(_ context.Context, _ string, _ *strings.Reader, args ...string) (string, error) {
				switch args[0] {
				case "display-message":
					return "0", nil
				case "capture-pane":
					if phase == 2 {
						if h == "claude-code" {
							return "Claude Code\n❯", nil
						}
						return "OpenAI Codex\n›", nil
					}
					if h == "openai-codex" {
						return "Do you trust the contents of this directory?\n1. Yes, continue", nil
					}
					if phase == 1 {
						return "Quick safety check\n❯ Yes, I trust this folder", nil
					}
					return "Quick safety check\n❯ No, exit\nYes, I trust this folder", nil
				case "send-keys":
					key := args[len(args)-1]
					keys = append(keys, key)
					if key == "Down" {
						phase = 1
					} else {
						phase = 2
					}
					return "", nil
				}
				return "", fmt.Errorf("unexpected command %v", args)
			}}
			seat := &nativeSeat{sessionID: "$42", paneID: "%23"}
			if err := transport.Ready(ctx, "socket", seat, h); err != nil {
				t.Fatal(err)
			}
			want := "[Enter]"
			if h == "claude-code" {
				want = "[Down Enter]"
			}
			if fmt.Sprint(keys) != want {
				t.Fatalf("bootstrap keys=%v", keys)
			}
		})
	}
}

func TestCodexReadinessWaitsForCurrentModelPanel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	events := make(chan struct{}, 2)
	events <- struct{}{}
	captures := 0
	loading := "OpenAI Codex\nmodel: loading /model to change\n" + strings.Repeat("startup line\n", 9) + "›"
	ready := "OpenAI Codex\nmodel: test-model medium /model to change\n›"
	transport := realSeatTransport{command: func(_ context.Context, _ string, _ *strings.Reader, args ...string) (string, error) {
		if args[0] == "display-message" {
			return "0", nil
		}
		captures++
		if captures == 1 {
			return loading, nil
		}
		return loading + "\n" + ready, nil
	}}
	seat := &nativeSeat{control: &seatControl{events: events}}
	if err := transport.Ready(ctx, "socket", seat, "openai-codex"); err != nil {
		t.Fatal(err)
	}
	if captures != 2 {
		t.Fatalf("readiness accepted loading panel after %d captures", captures)
	}
}

// paneFixture reads a pane captured with capture-pane -p -e from a real seat
// (Claude Code 2.1.274 and codex-cli 0.154.0 in tmux 3.6a, synthetic prompts,
// paths redacted). Its first line holds the cursor position.
func paneFixture(t *testing.T, name string) (string, int, int) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "panes", name+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	header, screen, _ := strings.Cut(string(raw), "\n")
	var x, y int
	if _, err := fmt.Sscanf(header, "cursor %d %d", &x, &y); err != nil {
		t.Fatalf("%s header %q: %v", name, header, err)
	}
	return screen, x, y
}

func TestSeatInputClearReadsCapturedPanes(t *testing.T) {
	for _, harness := range []struct{ id, name string }{{"claude-code", "claude"}, {"openai-codex", "codex"}} {
		for state, want := range map[string]bool{
			"idle-empty":      true,
			"idle-after-turn": true,
			"idle-typed":      false,
			"busy-empty":      false,
			"busy-typed":      false,
		} {
			screen, x, y := paneFixture(t, harness.name+"-"+state)
			if got := seatInputClear(harness.id, screen, x, y); got != want {
				t.Errorf("%s %s: clear = %t, want %t", harness.name, state, got, want)
			}
		}
	}
	screen, x, y := paneFixture(t, "codex-idle-empty")
	if seatInputClear("openai-codex", screen, x+1, y) || seatInputClear("openai-codex", screen, x, y-1) || seatInputClear("claude-code", screen, x, y) {
		t.Error("a cursor off the input start, or the other harness's prompt, counted as a clear input line")
	}
	// Codex draws its placeholder dim; the same words typed by the operator are not.
	if !seatInputClear("openai-codex", "\x1b[1m›\x1b[0m \x1b[2mAsk Codex to do anything", 2, 0) ||
		seatInputClear("openai-codex", "\x1b[1m›\x1b[0m Ask Codex to do anything", 2, 0) ||
		!seatInputClear("openai-codex", "\x1b[38;2;242;242;242m›\x1b[0m \x1b[2mplaceholder", 2, 0) {
		t.Error("dim placeholder and typed text were not told apart")
	}
}

func TestStageWaitsForAnIdleAgentWithAnEmptyInputLine(t *testing.T) {
	for _, harness := range []struct{ id, name string }{{"claude-code", "claude"}, {"openai-codex", "codex"}} {
		t.Run(harness.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			events := make(chan struct{}, 64)
			var states []string
			var shown, pasted []string
			loaded, afterPaste := "", false
			transport := realSeatTransport{
				control: func(context.Context, string, string) (*seatControl, error) { return &seatControl{events: events}, nil },
				command: func(_ context.Context, _ string, input *strings.Reader, args ...string) (string, error) {
					state := states[0]
					screen, x, y := paneFixture(t, harness.name+"-"+state)
					switch args[0] {
					case "display-message":
						return fmt.Sprintf("%d %d 0", x, y), nil
					case "capture-pane":
						if afterPaste {
							return screen + "\n" + loaded, nil
						}
						shown = append(shown, state)
						if len(states) > 1 {
							// The pane changes after each look, and tmux reports it.
							states = states[1:]
							events <- struct{}{}
						}
						return screen, nil
					case "load-buffer":
						b, _ := io.ReadAll(input)
						loaded = string(b)
						return "", nil
					case "paste-buffer":
						pasted = append(pasted, state)
						afterPaste = true
						return "", nil
					case "send-keys":
						afterPaste = false
						return "", nil
					}
					return "", fmt.Errorf("unexpected command %v", args)
				},
			}
			seat := &nativeSeat{name: "form-proof-worker", sessionID: "$42", paneID: "%23", variant: HarnessVariant{ID: harness.id}, brief: "/state/briefs/proof.md"}
			for _, paste := range []struct {
				dispatch string
				states   []string
			}{
				{"first-brief", []string{"busy-empty", "idle-typed", "idle-empty"}},
				{"peer-facilitator", []string{"busy-typed", "idle-typed", "idle-after-turn"}},
			} {
				states, shown = paste.states, nil
				if err := transport.Stage(ctx, "socket", seat, paste.dispatch, seatPointer(seat.brief)); err != nil {
					t.Fatalf("%s: %v", paste.dispatch, err)
				}
				if strings.Join(shown, ",") != strings.Join(paste.states, ",") {
					t.Fatalf("%s looked at %v, want every state %v before pasting", paste.dispatch, shown, paste.states)
				}
				if last := pasted[len(pasted)-1]; last != paste.states[len(paste.states)-1] {
					t.Fatalf("%s pasted while the pane was %s", paste.dispatch, last)
				}
			}
			if len(pasted) != 2 {
				t.Fatalf("pastes = %v, want one per staging", pasted)
			}

			// A pane that stays busy is waited on until the seat timeout.
			states = []string{"busy-empty"}
			short, stop := context.WithTimeout(ctx, 100*time.Millisecond)
			defer stop()
			if err := transport.Stage(short, "socket", seat, "stuck", seatPointer(seat.brief)); !errors.Is(err, context.DeadlineExceeded) || len(pasted) != 2 {
				t.Fatalf("busy pane: %v, pastes %v", err, pasted)
			}
		})
	}
}
