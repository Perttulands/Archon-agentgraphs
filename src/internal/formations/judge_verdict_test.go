package formations

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func judgeBlock(verdict, reason string, evidence ...string) string {
	if evidence == nil {
		evidence = []string{}
	}
	raw, _ := json.Marshal(map[string]any{"verdict": verdict, "reason": reason, "evidence": evidence})
	return "```chrote-verdict\n" + string(raw) + "\n```"
}

func TestParseJudgeVerdict(t *testing.T) {
	for _, verdict := range []string{"pass", "fail"} {
		t.Run(verdict, func(t *testing.T) {
			result, err := parseJudgeVerdict("Review complete.\n" + judgeBlock(verdict, "test reason", "report.md") + "\nDone.")
			if err != nil || result.Verdict != verdict || result.Reason != "test reason" || !reflect.DeepEqual(result.Evidence, []GateEvidenceRef{{Kind: "formation", Text: "report.md"}}) {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
	invalid := map[string]string{
		"bare pass":     "pass",
		"bare fail":     "fail",
		"prose":         "looks good",
		"missing close": "```chrote-verdict\n{}",
		"multiple":      judgeBlock("pass", "ok") + "\n" + judgeBlock("fail", "no"),
		"malformed":     "{",
		"missing key":   `{"verdict":"pass","reason":"ok"}`,
		"extra key":     `{"verdict":"pass","reason":"ok","evidence":[],"extra":true}`,
		"duplicate key": `{"verdict":"fail","verdict":"pass","reason":"ok","evidence":[]}`,
		"wrong case":    `{"Verdict":"pass","reason":"ok","evidence":[]}`,
		"wrong verdict": `{"verdict":"PASS","reason":"ok","evidence":[]}`,
		"null reason":   `{"verdict":"pass","reason":null,"evidence":[]}`,
		"null evidence": `{"verdict":"pass","reason":"ok","evidence":null}`,
		"null item":     `{"verdict":"pass","reason":"ok","evidence":[null]}`,
		"numeric item":  `{"verdict":"pass","reason":"ok","evidence":[1]}`,
		"trailing JSON": `{"verdict":"pass","reason":"ok","evidence":[]} {}`,
	}
	for name, output := range invalid {
		t.Run(name, func(t *testing.T) {
			if strings.HasPrefix(output, "{") {
				output = "```chrote-verdict\n" + output + "\n```"
			}
			if result, err := parseJudgeVerdict(output); err == nil {
				t.Fatalf("accepted invalid output as %+v", result)
			}
		})
	}
}

func TestGateFeedbackRoundTripAndPromptContext(t *testing.T) {
	input := RunInputRef{Ref: "ledger://run/work", Text: "draft one", ReportRef: "report.md", ArtifactRef: "draft.md"}
	next := gateFailInput("run", BoardConnection{ID: "pushback"}, "review", 2, input, "add coverage", []GateEvidenceRef{{Kind: "formation", Text: "missing scenario"}})
	raw, err := json.Marshal(next)
	if err != nil {
		t.Fatal(err)
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if got := runInputRefFromAny(decoded); !reflect.DeepEqual(got, next) {
		t.Fatalf("round trip: %+v, want %+v", got, next)
	}
	if next.Feedback.OriginalRef != input.Ref || next.Feedback.OriginalText != input.Text || next.ReportRef != input.ReportRef || next.ArtifactRef != input.ArtifactRef {
		t.Fatalf("original work lost: %+v", next)
	}
	req := FormationExecution{RunID: "run", MissionBeadID: "mission-task", Brief: FormationBrief{Goal: "revise", BeadID: "brief-task", Files: []string{"src/work.go", "src/work_test.go"}, Links: []string{"docs/contract.md", "docs/review.md"}}, Inputs: []RunInputRef{next}}
	card := PersonaCard{ID: "reviewer", Summary: "Check acceptance criteria"}
	variant := HarnessVariant{ID: "openai-codex"}
	for name, prompt := range map[string]string{
		"lab":  (&LabFormationExecutor{config: LabExecutorConfig{Cwd: t.TempDir()}}).renderPrompt(req, FormationSlot{ID: "worker"}, card, variant),
		"tmux": (&TmuxFormationExecutor{config: TmuxExecutorConfig{Cwd: t.TempDir()}}).renderPromptWithContext(req, FormationSlot{ID: "worker"}, card, variant, "", nil),
	} {
		t.Run(name, func(t *testing.T) {
			for _, text := range []string{"mission bead: mission-task", "brief bead: brief-task", "brief file: src/work.go", "brief file: src/work_test.go", "brief link: docs/contract.md", "brief link: docs/review.md", "persona summary: Check acceptance criteria", "gate feedback from review, attempt 2:", "verdict: fail", "reason: add coverage", "evidence: missing scenario", "original input ref: ledger://run/work", "original input: draft one"} {
				if !strings.Contains(prompt, text) {
					t.Errorf("prompt missing %q: %s", text, prompt)
				}
			}
			if strings.Index(prompt, "original input:") < strings.Index(prompt, "gate feedback from") {
				t.Fatal("feedback must precede original input")
			}
		})
	}
}

type scriptedLabExecutor struct {
	lab       *LabFormationExecutor
	responses map[string]map[int]string
	calls     []FormationExecution
}

func (e *scriptedLabExecutor) ExecuteFormation(req FormationExecution) (FormationExecutionResult, error) {
	e.calls = append(e.calls, req)
	text, ok := e.responses[req.NodeID][req.Attempt]
	if !ok {
		return FormationExecutionResult{}, fmt.Errorf("unscripted node %s attempt %d", req.NodeID, req.Attempt)
	}
	result, err := e.lab.ExecuteFormation(req)
	if err != nil {
		return result, err
	}
	result.Text = text
	result.Outputs = labOutputPayloads(req.Formation, text)
	return result, nil
}

func TestLabJudgePushbackLoop(t *testing.T) {
	for _, malformed := range []bool{false, true} {
		t.Run(fmt.Sprintf("malformed=%t", malformed), func(t *testing.T) {
			store, personas := s4RunFixture(t)
			createS4Persona(t, personas, "scout")
			fixture := strings.Replace(s4JudgeChainRunBoardFixture(), `kinds = ["code", "formation"]`, `kinds = ["formation"]`, 1)
			fixture += "\n[[connection]]\nid = \"edge_gate_fail_work\"\nfrom = \"gate_review:fail\"\nto = \"fmn_work:port_work_in\"\n"
			writeFixture(t, store.BoardPath("session-search"), fixture)
			board, err := store.ReadBoard("session-search")
			if err != nil {
				t.Fatal(err)
			}
			cwd := t.TempDir()
			firstVerdict := judgeBlock("fail", "add the missing test", "coverage report: retry untested")
			if malformed {
				firstVerdict = "```chrote-verdict\n{invalid JSON}\n```"
			}
			executor := &scriptedLabExecutor{lab: NewLabFormationExecutor(store, personas, LabExecutorConfig{Cwd: cwd, Roots: []string{cwd}, Harnesses: []string{"openai-codex"}}), responses: map[string]map[int]string{
				"fmn_work": {1: "draft one", 2: "draft two with tests"},
				"fmn_j1":   {1: "first review", 2: "second review"},
				"fmn_j2":   {1: firstVerdict, 2: judgeBlock("pass", "tests cover retry", "test report: green")},
				"fmn_ship": {1: "shipped"},
			}}
			engine := NewRunEngine(store, personas, executor)
			status, err := engine.RunMission("session-search", RunStartRequest{MissionID: "mis_showcase", ExpectedBoardETag: board.ETag, ExpectedBoardRev: board.Rev, Limits: RunLimits{MaxDispatch: 8, MaxAttempts: 3}})
			if err != nil {
				t.Fatal(err)
			}
			events, err := store.ReadRunEvents(status.RunID)
			if err != nil {
				t.Fatal(err)
			}
			for _, event := range events {
				if event.Type == RunEventGateKindResult || event.Type == RunEventGateVerdict || event.Type == RunEventJudgeAttemptFailed || event.Type == RunEventBlocked || event.Type == RunEventSucceeded || (event.Type == RunEventNodeStarted && event.NodeID == "fmn_work") {
					raw, _ := json.Marshal(event)
					t.Log(string(raw))
				}
			}
			if malformed {
				if status.Status != RunStatusBlocked || status.ResumeAllowed {
					t.Fatalf("invalid output status: %+v", status)
				}
				failed := eventOfType(t, events, RunEventJudgeAttemptFailed)
				if failed.Data["code"] != "invalid_judge_result" || failed.Attempt != 1 || failed.Data["reason"] == "" {
					t.Fatalf("failure event: %+v", failed)
				}
				if _, err := engine.ResumeRun(status.RunID, RunResumeRequest{Mode: "reattach"}); !errors.Is(err, ErrRunResumeNotAllowed) {
					t.Fatalf("resume invalid verdict: %v", err)
				}
				if got := eventNodeOrder(events, RunEventGateVerdict); len(got) != 0 {
					t.Fatalf("malformed verdict routed: %v", got)
				}
				if len(executor.calls) != 3 {
					t.Fatalf("invalid output dispatched beyond judge: %+v", executor.calls)
				}
				return
			}
			if status.Status != RunStatusSucceeded || !status.Final {
				t.Fatalf("loop status: %+v", status)
			}
			work := callsByNode(executor.calls)["fmn_work"]
			if len(work) != 2 || work[1].Attempt != 2 {
				t.Fatalf("work attempts: %+v", work)
			}
			feedback := work[1].Inputs[0].Feedback
			if feedback == nil || feedback.GateID != "gate_review" || feedback.GateAttempt != 1 || feedback.Verdict != "fail" || feedback.Reason != "add the missing test" || feedback.OriginalText != "draft one" || feedback.OriginalRef == "" || !reflect.DeepEqual(feedback.Evidence, []GateEvidenceRef{{Kind: "formation", Text: "coverage report: retry untested"}}) {
				t.Fatalf("pushback feedback: %+v", feedback)
			}
			slot := work[1].Formation.Slots[0]
			card, err := personas.ReadPersona(slot.AgentID)
			if err != nil {
				t.Fatal(err)
			}
			variant, err := card.SelectHarnessVariant(slot.Harness)
			if err != nil {
				t.Fatal(err)
			}
			prompt := executor.lab.renderPrompt(work[1], slot, *card, variant)
			matchedDispatch := false
			for _, event := range events {
				if event.Type == RunEventSlotDispatch && event.NodeID == "fmn_work" && event.Attempt == 2 {
					if stringFromEventData(event, "promptSha256") != etag([]byte(prompt)) {
						t.Fatal("rendered prompt differs from actual lab dispatch")
					}
					matchedDispatch = true
				}
				if event.Type == RunEventGateKindResult || event.Type == RunEventGateVerdict {
					wantReason, wantEvidence := "add the missing test", "coverage report: retry untested"
					if event.Data["verdict"] == "pass" {
						wantReason, wantEvidence = "tests cover retry", "test report: green"
					}
					if event.Data["reason"] != wantReason || !reflect.DeepEqual(gateEvidenceRefsFromRunEventData(event.Data["evidence"]), []GateEvidenceRef{{Kind: "formation", Text: wantEvidence}}) {
						t.Fatalf("verdict evidence: %+v", event)
					}
				}
			}
			if !matchedDispatch {
				t.Fatal("missing second work dispatch")
			}
			for _, text := range []string{"gate feedback from gate_review, attempt 1:", "reason: add the missing test", "evidence: coverage report: retry untested", "original input: draft one", "mission bead: " + board.Missions[0].BeadID} {
				if !strings.Contains(prompt, text) {
					t.Errorf("second dispatched lab prompt missing %q: %s", text, prompt)
				}
			}
		})
	}
}
