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

func TestIdleSeatRecoversOnceThenBlocks(t *testing.T) {
	for _, h := range fixtureHarnesses {
		for _, background := range []bool{false, true} {
			if background && h.id != "claude-code" {
				continue
			}
			for _, completes := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/completes=%v/background=%v", h.name, completes, background), func(t *testing.T) {
					root := t.TempDir()
					pointer := "Read /work/brief.md"
					path := filepath.Join(root, "native.jsonl")
					raw := nativeFixture(h.id, root, pointer, "API Error: Connection lost mid-response")
					if !completes {
						if h.id == "openai-codex" {
							raw = raw[:strings.LastIndex(raw, `{"type":"event_msg"`)]
						} else {
							raw = strings.ReplaceAll(raw, `,"stop_reason":"end_turn"`, "")
						}
					}
					if background {
						lines := strings.SplitN(raw, "\n", 2)
						raw = lines[0] + "\n" + `{"type":"assistant","sessionId":"native","message":{"content":[{"type":"tool_use","id":"bg-tool","name":"Bash","input":{"run_in_background":true}}]}}` + "\n" + lines[1]
					}
					if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
						t.Fatal(err)
					}
					watch, err := filewatch.New(root)
					if err != nil {
						t.Fatal(err)
					}
					defer watch.Close()
					screen, x, y := paneFixture(t, h.name+"-idle-after-turn")
					typed := ""
					recoveries, captures := 0, 0
					backgroundFinished := !background
					transport := realSeatTransport{idleTimeout: 10 * time.Millisecond, checkInterval: time.Millisecond,
						command: func(_ context.Context, _ string, in *strings.Reader, args ...string) (string, error) {
							switch args[0] {
							case "display-message":
								return fmt.Sprintf("%d %d 0", x, y), nil
							case "capture-pane":
								captures++
								if background && !backgroundFinished && captures == 30 {
									backgroundFinished = true
									raw += `{"type":"queue-operation","operation":"enqueue","sessionId":"native","content":"<task-notification><tool-use-id>bg-tool</tool-use-id><status>completed</status></task-notification>"}` + "\n"
									if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
										return "", err
									}
								}
								if typed != "" {
									lines := strings.Split(screen, "\n")
									lines[y] = seatInputPrompt[h.id] + " " + typed
									return strings.Join(lines, "\n"), nil
								}
								return screen, nil
							case "load-buffer":
								if !backgroundFinished {
									t.Fatal("recovery interrupted pending background task")
								}

								b := make([]byte, in.Len())
								in.Read(b)
								typed = string(b)
								return "", nil
							case "paste-buffer":
								return "", nil
							case "send-keys":
								recoveries++
								typed = ""
								if completes {
									return "", os.WriteFile(path, []byte(raw+nativeFixture(h.id, root, "continue", "done\n<<<ARCHON-DONE run-id=run_test status=ok artifact=result>>>")), 0600)
								}
								return "", nil
							}
							return "0", nil
						},
					}
					seat := &nativeSeat{variant: HarnessVariant{ID: h.id}, root: root, runID: "run_test", watch: watch, control: &seatControl{events: make(chan struct{}, 10)}}
					ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
					defer cancel()
					turn, err := transport.WaitTurn(ctx, seat, root, pointer, func(codexTranscriptTurn) error { return nil })
					if recoveries != 1 {
						t.Fatalf("submits=%d err=%v", recoveries, err)
					}
					if completes {
						if err != nil || !turn.Complete {
							t.Fatalf("turn=%+v err=%v", turn, err)
						}
					} else {
						var failure *RunExecutionError
						if !errors.As(err, &failure) || failure.Code != "seat_idle_without_sentinel" {
							t.Fatalf("want resumable missing-sentinel error code: %v", err)
						}
					}
				})
			}
		}
	}
}

func TestIdleRecoveryLeavesBusyAndOperatorInputAlone(t *testing.T) {
	for _, h := range fixtureHarnesses {
		for _, state := range []string{"busy-empty", "busy-typed", "idle-typed"} {
			t.Run(h.name+"/"+state, func(t *testing.T) {
				root := t.TempDir()
				pointer := "Read /work/brief.md"
				if err := os.WriteFile(filepath.Join(root, "native.jsonl"), []byte(nativeFixture(h.id, root, pointer, "Goal stalled")), 0600); err != nil {
					t.Fatal(err)
				}
				watch, err := filewatch.New(root)
				if err != nil {
					t.Fatal(err)
				}
				defer watch.Close()
				screen, x, y := paneFixture(t, h.name+"-"+state)
				transport := realSeatTransport{idleTimeout: 5 * time.Millisecond, checkInterval: time.Millisecond, command: func(_ context.Context, _ string, _ *strings.Reader, args ...string) (string, error) {
					switch args[0] {
					case "display-message":
						return fmt.Sprintf("%d %d 0", x, y), nil
					case "capture-pane":
						return screen, nil
					default:
						t.Errorf("unexpected recovery command: %v", args)
						return "0", nil
					}
				}}
				seat := &nativeSeat{variant: HarnessVariant{ID: h.id}, root: root, runID: "run_test", watch: watch}
				ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
				defer cancel()
				_, err = transport.WaitTurn(ctx, seat, root, pointer, func(codexTranscriptTurn) error { return nil })
				if err != context.DeadlineExceeded {
					t.Fatalf("want waiting, got %v", err)
				}
			})
		}
	}
}

func TestClaudeBackgroundWorkTracksIndividualCompletions(t *testing.T) {
	pending := map[string]bool{}
	launch := `[{"type":"tool_use","id":"first","name":"Bash","input":{"run_in_background":true}},{"type":"tool_use","id":"second","name":"Monitor"}]`
	updateClaudeBackgroundWork(pending, "assistant", []byte(launch), "", "")
	updateClaudeBackgroundWork(pending, "queue-operation", nil, "<task-notification><tool-use-id>first</tool-use-id><status>completed</status></task-notification>", "")
	if len(pending) != 1 || !pending["second"] {
		t.Fatalf("completed one of two tasks: %v", pending)
	}
	updateClaudeBackgroundWork(pending, "user", []byte(`[{"type":"tool_result","tool_use_id":"second","is_error":true,"content":"launch failed"}]`), "", "")
	if len(pending) != 0 {
		t.Fatalf("failed task launch left pending: %v", pending)
	}
}
