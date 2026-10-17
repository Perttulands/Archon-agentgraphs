package formations

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestFailLimitPreservesJudgedAttemptsWithoutBlocking(t *testing.T) {
	for _, target := range []string{"fmn_work", "mis_showcase"} {
		t.Run(target, func(t *testing.T) {
			rounds := "3"
			if target == "mis_showcase" {
				rounds = "9"
			}
			card := strings.Replace(workCard("rounds = "+rounds+"\nwhenSpent = \"fail\""), "target = \"fmn_work\"", "target = \""+target+"\"", 1)
			store, _, _, status := pickRun(t, pickBoardFixture(`["formation"]`, card), threeFailedDrafts(""))
			if status.Status != RunStatusFailed || !status.Final || status.ResumeAllowed {
				t.Fatalf("status: %+v", status)
			}
			events, err := store.ReadRunEvents(status.RunID)
			if err != nil {
				t.Fatal(err)
			}
			last := events[len(events)-1]
			if last.Type != RunEventFailed || last.Data["code"] != RunBlockLimitReached || !strings.Contains(stringFromEventData(last, "reason"), "used "+rounds+" of "+rounds+" rounds") {
				t.Fatalf("end: %+v", last)
			}
			if len(eventsOfType(events, RunEventBlocked)) != 0 || len(eventsOfType(events, RunEventError)) != 0 {
				t.Fatalf("phantom block/error: %+v", events)
			}
			evidence, err := store.ProjectRunAttempts(status.RunID, "fmn_work")
			if err != nil {
				t.Fatal(err)
			}
			if len(evidence.Steps) != 1 || len(evidence.Steps[0].Attempts) != 3 {
				t.Fatalf("attempts: %+v", evidence)
			}
			for _, attempt := range evidence.Steps[0].Attempts {
				if len(attempt.Outputs) == 0 || len(attempt.Verdicts) == 0 || attempt.Verdicts[0].Verdict != "fail" {
					t.Fatalf("lost evidence: %+v", attempt)
				}
			}
		})
	}
}

func TestFailTokenLimitCutsOffActiveAttempt(t *testing.T) {
	for _, target := range []string{"fmn_research", "mis_showcase"} {
		t.Run(target, func(t *testing.T) {
			store, personas := s4RunFixture(t)
			createS4Persona(t, personas, "scout")
			fixture := strings.Replace(s4RunBoardFixture(), "title = \"Research\"\n", "title = \"Research\"\n\n[formation.brief]\ngoal = \"Research. archon-lab-tokens: 15000\"\n", 1)
			fixture += "\n[[limit]]\nid = \"lim_cap\"\ntitle = \"Budget\"\ntarget = \"" + target + "\"\ntokens = 10000\nwhenSpent = \"fail\"\n"
			writeFixture(t, store.BoardPath("session-search"), fixture)
			engine := NewRunEngine(store, personas, NewLabFormationExecutor(store, personas, LabExecutorConfig{Cwd: t.TempDir(), Harnesses: []string{"openai-codex"}}))
			status, err := engine.RunMission("session-search", RunStartRequest{MissionID: "mis_showcase", Actor: "agent:test"})
			if err != nil {
				t.Fatal(err)
			}
			events := mustEvents(t, store, status.RunID)
			if status.Status != RunStatusFailed || len(eventsOfType(events, RunEventBlocked)) != 0 {
				t.Fatalf("status: %+v; events: %s", status, eventTypeTrail(events))
			}
			last := events[len(events)-1]
			if last.Data["code"] != RunBlockLimitReached || !strings.Contains(stringFromEventData(last, "reason"), "15,000 of 10,000 tokens") {
				t.Fatalf("end: %+v", last)
			}
		})
	}
}

func TestOnlyRunOwnerCancellationSuppressesExecutionFailure(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(map[bool]string{false: "executor", true: "owner"}[canceled], func(t *testing.T) {
			store, engine, _, status := pickRun(t, pickBoardFixture(`["formation"]`, workCard("rounds = 1")), threeFailedDrafts(""))
			if err := store.AppendRunEvent(status.RunID, RunEvent{Type: RunEventResumed, Actor: "agent:test"}); err != nil {
				t.Fatal(err)
			}
			before, _ := store.ReadRunEvents(status.RunID)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if canceled {
				cancel()
			}
			engine.SetExecutionContext(func(string) context.Context { return ctx })
			err := engine.appendExecutionFailureAndBlock(status.RunID, "fmn_work", errors.Join(errors.New("executor stopped"), context.Canceled))
			if err != nil {
				t.Fatal(err)
			}
			after, _ := store.ReadRunEvents(status.RunID)
			if canceled && len(after) != len(before) {
				t.Fatal("owner cancellation added failure evidence")
			}
			if !canceled && len(after) == len(before) {
				t.Fatal("executor cancellation lost failure evidence")
			}
		})
	}
}

func TestFailTimeLimitCutsOffActiveAttempt(t *testing.T) {
	for _, target := range []string{"fmn_research", "mis_showcase"} {
		t.Run(target, func(t *testing.T) {
			store, _, executor, _, status := timedRun(t, s4CascadeBoardFixture(), map[string][]time.Duration{
				"fmn_research": {90 * time.Second},
			}, func(store *Store) {
				board, err := store.ReadBoard("session-search")
				if err != nil {
					t.Fatal(err)
				}
				_, err = store.CreateLimit("session-search", LimitCreateRequest{Title: "Clock", Target: target, Seconds: 60, WarnSeconds: 10, WhenSpent: "fail"}, WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev})
				if err != nil {
					t.Fatal(err)
				}
			})
			events := mustEvents(t, store, status.RunID)
			if status.Status != RunStatusFailed || len(eventsOfType(events, RunEventBlocked)) != 0 || len(executor.left["fmn_ship"]) != 0 {
				t.Fatalf("status: %+v; events: %s", status, eventTypeTrail(events))
			}
			if last := events[len(events)-1]; last.Data["code"] != RunBlockLimitReached || limitFromAny(last.Data["limit"]).Kind != LimitKindTime {
				t.Fatalf("end: %+v", last)
			}
			if warnings := executor.warnings["fmn_research"]; len(warnings) != 1 || !strings.Contains(warnings[0].Text, "run fails") {
				t.Fatalf("warning: %+v", warnings)
			}
		})
	}
}
