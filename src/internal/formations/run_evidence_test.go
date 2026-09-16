package formations

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func evidenceEvent(seq int, eventType, nodeID string, data map[string]any) RunEvent {
	event := RunEvent{Seq: seq, Type: eventType, NodeID: nodeID, Data: data}
	if strings.HasPrefix(nodeID, "gate_") {
		event.GateID = nodeID
	}
	return event
}

func TestNodeEvidenceGroupsAttemptsAndOmitsSessionIdentity(t *testing.T) {
	artifacts := "/ws/.formations/artifacts/run_1"
	events := []RunEvent{
		evidenceEvent(1, RunEventNodeStarted, "fmn_work", map[string]any{"reason": "initial", "inputRefs": []any{
			map[string]any{"edgeId": "edge_in", "fromNodeId": "mis_a", "fromPortId": "out", "toPortId": "port_in", "text": "Build it", "ref": artifacts + "/brief.md"},
		}}),
		{Seq: 2, Type: RunEventSlotDispatch, NodeID: "fmn_work", SlotID: "slot_work", Attempt: 1, Data: map[string]any{
			"agentId": "codex-builder", "harness": "openai-codex", "dispatchId": "dsp_1", "briefPath": "/ws/briefs/seat-1.md",
			"sessionRef": "tmux:SESSION-REF", "promptSha256": "PROMPT-DIGEST", "promptRef": "PROMPT-REF",
		}},
		{Seq: 3, Type: "seat_created", NodeID: "fmn_work", SlotID: "slot_work", Data: map[string]any{"sessionId": "$TMUX-SESSION", "paneId": "%TMUX-PANE", "sessionName": "form-run_1-slot_work"}},
		{Seq: 4, Type: "seat_prompt_consumed", NodeID: "fmn_work", SlotID: "slot_work", Data: map[string]any{"nativeSessionId": "NATIVE-SESSION", "dispatchId": "dsp_1"}},
		{Seq: 5, Type: RunEventSlotResult, NodeID: "fmn_work", SlotID: "slot_work", Data: map[string]any{"dispatchId": "dsp_1", "status": "ok", "sentinel": map[string]any{"artifact": artifacts + "/plan.md"}}},
		{Seq: 51, Type: "seat_cleanup", NodeID: "fmn_work", SlotID: "slot_work", Data: map[string]any{"sessionName": "form-run_1-slot_work", "outcome": "left_cleanup_failed", "detail": "kill-session $TMUX-SESSION failed"}},
		evidenceEvent(6, RunEventNodeOutput, "fmn_work", map[string]any{
			"status": "done", "text": "Summary with token=hunter2", "reportRef": "tmux://run_1/fmn_work/report",
			"outputs": map[string]any{
				"port_z": map[string]any{"text": "second"},
				"port_a": map[string]any{"text": "# Plan", "ref": artifacts + "/nested/plan.md"},
				"port_x": map[string]any{"text": "outside", "ref": "/etc/private/notes.txt"},
			},
		}),
		evidenceEvent(7, RunEventGateEvaluating, "gate_review", map[string]any{"criterion": "Beads pass lint", "kinds": []any{"formation", "human"}, "judgeChain": []any{"fmn_judge"}, "inputRef": map[string]any{"edgeId": "edge_review", "fromNodeId": "fmn_work", "fromPortId": "port_a", "text": "# Plan", "ref": "ledger://run_1/edge_review"}}),
		evidenceEvent(8, RunEventJudgeAttemptFailed, "gate_review", map[string]any{"code": "invalid_judge_result", "reason": "missing verdict block"}),
		evidenceEvent(9, RunEventGateKindResult, "gate_review", map[string]any{"kind": "formation", "verdict": "pass", "reason": "Lint is clean", "evidence": []any{map[string]any{"kind": "formation", "text": "bd lint: no warnings"}, "plain evidence"}}),
		evidenceEvent(10, RunEventHumanInputRequested, "gate_review", map[string]any{"prompt": "Beads pass lint"}),
		evidenceEvent(11, RunEventHumanVerdictRecorded, "gate_review", map[string]any{"requestedSeq": 10, "verdict": "pass", "reason": "Q1: yes", "decidedBy": "human:operator"}),
		evidenceEvent(12, RunEventGateVerdict, "gate_review", map[string]any{"verdict": "pass", "reason": "judge and operator agree", "perKind": map[string]any{"formation": "pass", "human": "pass"}, "routePort": "pass", "evidence": []any{"bd lint: no warnings"}}),
		{Seq: 13, Type: RunEventNodeStarted, NodeID: "fmn_work", Attempt: 2, Data: map[string]any{"reason": "feedback"}},
		{Seq: 14, Type: RunEventBlocked, NodeID: "fmn_work", Data: map[string]any{"reason": "seat timed out", "blockedNodeId": "fmn_work", "resumeAllowed": true}},
	}
	work := projectNodeEvidence("run_1", "fmn_work", "formation", events, false, []string{artifacts})
	if len(work.Attempts) != 2 || work.Attempts[1].Attempt != 2 || work.Attempts[1].Output != nil {
		t.Fatalf("attempts = %+v", work.Attempts)
	}
	first := work.Attempts[0]
	if len(first.Inputs) != 1 || first.Inputs[0].Text.Text != "Build it" || first.Inputs[0].Ref.Artifact != "brief.md" {
		t.Fatalf("inputs = %+v", first.Inputs)
	}
	if len(first.Dispatches) != 1 || first.Dispatches[0] != (EvidenceDispatch{Seq: 2, SlotID: "slot_work", AgentID: "codex-builder", Harness: "openai-codex", Brief: true, ResultSeq: 5, Status: "ok"}) {
		t.Fatalf("dispatches = %+v", first.Dispatches)
	}
	if len(first.SeatCleanups) != 1 || first.SeatCleanups[0] != (EvidenceSeatCleanup{Seq: 51, SlotID: "slot_work", Outcome: "left_cleanup_failed"}) {
		t.Fatalf("seat cleanups = %+v", first.SeatCleanups)
	}
	output := first.Output
	if output == nil || output.Seq != 6 || output.Text.Text != "Summary with token=[REDACTED]" {
		t.Fatalf("output = %+v", output)
	}
	var ports []string
	for _, port := range output.Ports {
		ports = append(ports, port.PortID)
	}
	if strings.Join(ports, ",") != "port_a,port_x,port_z" || output.Ports[0].Ref.Artifact != "nested/plan.md" || *output.Ports[1].Ref != (EvidenceRef{External: "notes.txt"}) || output.Ports[2].Ref != nil {
		t.Fatalf("ports = %+v", output.Ports)
	}
	if len(work.Problems) != 1 || work.Problems[0].Reason.Text != "seat timed out" || work.Problems[0].ResumeAllowed == nil || !*work.Problems[0].ResumeAllowed {
		t.Fatalf("problems = %+v", work.Problems)
	}

	gate := projectNodeEvidence("run_1", "gate_review", "gate", events, false, []string{artifacts})
	if len(gate.Evaluations) != 1 {
		t.Fatalf("evaluations = %+v", gate.Evaluations)
	}
	evaluation := gate.Evaluations[0]
	if evaluation.Criterion.Text != "Beads pass lint" || strings.Join(evaluation.Kinds, ",") != "formation,human" || evaluation.JudgeChain[0] != "fmn_judge" || evaluation.Input.FromPortID != "port_a" || evaluation.Input.Ref != nil {
		t.Fatalf("evaluation = %+v", evaluation)
	}
	if len(evaluation.JudgeFailures) != 1 || evaluation.JudgeFailures[0].Reason.Text != "missing verdict block" {
		t.Fatalf("judge failures = %+v", evaluation.JudgeFailures)
	}
	if len(evaluation.KindResults) != 1 || evaluation.KindResults[0].Reason.Text != "Lint is clean" || len(evaluation.KindResults[0].Evidence) != 2 || evaluation.KindResults[0].Evidence[1].Text.Text != "plain evidence" {
		t.Fatalf("kind results = %+v", evaluation.KindResults)
	}
	request := evaluation.HumanRequests[0]
	if request.Pending || request.Decision == nil || request.Decision.Response.Text != "Q1: yes" || request.Decision.DecidedBy != "human:operator" {
		t.Fatalf("human request = %+v", request)
	}
	if evaluation.Verdict == nil || evaluation.Verdict.Reason.Text != "judge and operator agree" || evaluation.Verdict.PerKind["human"] != "pass" || evaluation.Verdict.RoutePort != "pass" {
		t.Fatalf("verdict = %+v", evaluation.Verdict)
	}

	raw, err := json.Marshal([]*NodeEvidence{work, gate})
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"SESSION-REF", "TMUX-SESSION", "TMUX-PANE", "NATIVE-SESSION", "PROMPT-DIGEST", "PROMPT-REF", "/ws/briefs", "tmux://", artifacts, "/etc/private", "dsp_1", "hunter2", "form-run_1-slot_work"} {
		if strings.Contains(string(raw), private) {
			t.Fatalf("node evidence leaked %q: %s", private, raw)
		}
	}
}

func TestRunProblemsServeBlocksThatNameNoNode(t *testing.T) {
	events := []RunEvent{
		evidenceEvent(1, RunEventNodeStarted, "fmn_map", nil),
		{Seq: 2, Type: RunEventError, Data: map[string]any{"code": "coordinator_interrupted", "message": "coordinator restarted with open dispatches", "openDispatches": []any{map[string]any{"dispatchId": "dsp_1", "nodeId": "fmn_map", "slotId": "slot_scout"}}}},
		{Seq: 3, Type: RunEventBlocked, Data: map[string]any{"reason": "coordinator restarted; completed-turn evidence required", "openDispatches": []any{map[string]any{"nodeId": "fmn_map"}}, "resumeAllowed": true}},
		{Seq: 4, Type: RunEventResumed},
		{Seq: 5, Type: RunEventError, Data: map[string]any{"code": "wall_clock_exceeded", "message": "wall clock limit exceeded", "nodeId": ""}},
		{Seq: 6, Type: RunEventBlocked, Data: map[string]any{"reason": "wall clock limit exceeded", "openDispatches": []map[string]any{}, "resumeAllowed": true}},
	}

	// A restart block names its nodes only through their open dispatches.
	mapped := projectNodeEvidence("run_1", "fmn_map", "formation", events, false, nil)
	if len(mapped.Problems) != 2 || mapped.Problems[1].Seq != 3 || mapped.Problems[1].Reason.Text != "coordinator restarted; completed-turn evidence required" {
		t.Fatalf("map problems = %+v", mapped.Problems)
	}

	problems := projectRunProblems(events)
	if len(problems) != 4 {
		t.Fatalf("run problems = %+v", problems)
	}
	restart, clock := problems[1], problems[3]
	if restart.Seq != 3 || strings.Join(restart.NodeIDs, ",") != "fmn_map" || restart.ResumeAllowed == nil || !*restart.ResumeAllowed {
		t.Fatalf("restart block = %+v", restart)
	}
	if clock.Seq != 6 || clock.Type != RunEventBlocked || clock.Reason.Text != "wall clock limit exceeded" || len(clock.NodeIDs) != 0 {
		t.Fatalf("wall clock block = %+v", clock)
	}
	if problems[2].Code != "wall_clock_exceeded" || problems[2].Reason.Text != "wall clock limit exceeded" {
		t.Fatalf("wall clock error = %+v", problems[2])
	}
	raw, err := json.Marshal(clock)
	if err != nil || !strings.Contains(string(raw), `"nodeIds":[]`) || strings.Contains(string(raw), "dsp_1") {
		t.Fatalf("wall clock block JSON = %s, %v", raw, err)
	}
}

// The newest problems are served first when the response budget runs out.
func TestRunProblemsSpendTheBudgetOnTheLatest(t *testing.T) {
	long := strings.Repeat("x", EvidenceTextMaxBytes)
	events := []RunEvent{}
	for seq := 1; seq <= EvidenceNodeBudgetBytes/EvidenceTextMaxBytes+2; seq++ {
		events = append(events, RunEvent{Seq: seq, Type: RunEventError, Data: map[string]any{"message": long}})
	}
	events = append(events, RunEvent{Seq: len(events) + 1, Type: RunEventBlocked, Data: map[string]any{"reason": "the latest block"}})
	problems := projectRunProblems(events)
	if last := problems[len(problems)-1]; last.Reason.Text != "the latest block" || last.Reason.Truncated {
		t.Fatalf("latest block = %+v", last.EvidenceProblem)
	}
	if first := problems[0]; first.Reason.Text != "" || !first.Reason.Truncated {
		t.Fatalf("oldest error kept %d bytes", len(first.Reason.Text))
	}
}

func TestNodeEvidenceMarksAnUnansweredRequestPendingUntilTheRunEnds(t *testing.T) {
	events := []RunEvent{
		evidenceEvent(1, RunEventGateEvaluating, "gate_review", map[string]any{"kinds": []any{"human"}}),
		evidenceEvent(2, RunEventHumanInputRequested, "gate_review", nil),
	}
	if got := projectNodeEvidence("run_1", "gate_review", "gate", events, false, nil).Evaluations[0].HumanRequests[0]; !got.Pending {
		t.Fatalf("open run request = %+v, want pending", got)
	}
	if got := projectNodeEvidence("run_1", "gate_review", "gate", events, true, nil).Evaluations[0].HumanRequests[0]; got.Pending {
		t.Fatalf("final run request = %+v, want not pending", got)
	}
	events = append(events, evidenceEvent(3, RunEventHumanInputRequested, "gate_review", nil))
	requests := projectNodeEvidence("run_1", "gate_review", "gate", events, false, nil).Evaluations[0].HumanRequests
	if len(requests) != 2 || requests[0].Pending || !requests[1].Pending {
		t.Fatalf("superseded requests = %+v, want only the latest pending", requests)
	}
}

func TestNodeEvidenceCapsEachTextAndTheResponse(t *testing.T) {
	long := strings.Repeat("ä", EvidenceTextMaxBytes) // two bytes per rune
	items := make([]any, EvidenceItemsMax+5)
	for i := range items {
		items[i] = "item"
	}
	outputs := map[string]any{}
	for i := 0; i < 40; i++ {
		outputs["port_"+strings.Repeat("a", i+1)] = map[string]any{"text": long}
	}
	events := []RunEvent{
		evidenceEvent(1, RunEventGateKindResult, "gate_review", map[string]any{"kind": "formation", "reason": long + "x", "evidence": items}),
		evidenceEvent(2, RunEventNodeOutput, "gate_review", map[string]any{"text": "short", "outputs": outputs}),
	}
	evidence := projectNodeEvidence("run_1", "gate_review", "gate", events, false, nil)
	result := evidence.Evaluations[0].KindResults[0]
	if !result.Reason.Truncated || result.Reason.Bytes != len(long)+1 || len(result.Reason.Text) > EvidenceTextMaxBytes || !utf8.ValidString(result.Reason.Text) {
		t.Fatalf("reason cap: truncated=%v bytes=%d served=%d", result.Reason.Truncated, result.Reason.Bytes, len(result.Reason.Text))
	}
	if len(result.Evidence) != EvidenceItemsMax || result.EvidenceOmitted != 5 {
		t.Fatalf("evidence = %d items, %d omitted", len(result.Evidence), result.EvidenceOmitted)
	}
	served := len(result.Reason.Text) + len("item")*EvidenceItemsMax
	emptied := 0
	for _, port := range evidence.Attempts[0].Output.Ports {
		served += len(port.Text.Text)
		if port.Text.Text == "" && port.Text.Truncated && port.Text.Bytes == len(long) {
			emptied++
		}
	}
	if served > EvidenceNodeBudgetBytes || emptied == 0 {
		t.Fatalf("served %d bytes with %d emptied texts; budget %d", served, emptied, EvidenceNodeBudgetBytes)
	}
}

func TestRunEvidenceReadsNodesArtifactsAndBriefsInsideTheRun(t *testing.T) {
	store, _, runID := startHumanGateRun(t)
	evidence, err := store.ProjectNodeEvidence(runID, "gate_review")
	if err != nil || evidence.Kind != "gate" || len(evidence.Evaluations) != 1 || !evidence.Evaluations[0].HumanRequests[0].Pending {
		t.Fatalf("gate evidence = %+v, %v", evidence, err)
	}
	for _, unknown := range [][2]string{{runID, "fmn_missing"}, {"run_missing", "gate_review"}, {"not-a-run", "gate_review"}} {
		if _, err := store.ProjectNodeEvidence(unknown[0], unknown[1]); !errors.Is(err, ErrNotFound) {
			t.Fatalf("%v: err = %v, want ErrNotFound", unknown, err)
		}
	}

	if artifacts, truncated, err := store.ListRunArtifacts(runID); err != nil || len(artifacts) != 0 || truncated {
		t.Fatalf("run without artifacts = %+v, %v, %v", artifacts, truncated, err)
	}
	root := filepath.Join(store.Workspace, ".formations", "artifacts", runID)
	outside := t.TempDir()
	mustWrite := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(filepath.Join(root, "plan.md"), "# Plan\napi_key: sk-abcdefghijklmnop\n")
	mustWrite(filepath.Join(root, "logs", "run.log"), "ok\n")
	mustWrite(filepath.Join(root, "data.json"), `{"a":1}`)
	mustWrite(filepath.Join(root, "blob.bin"), "a\x00b")
	mustWrite(filepath.Join(root, "shot.png"), "\x89PNG")
	mustWrite(filepath.Join(root, "paper.pdf"), "%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	mustWrite(filepath.Join(root, "fake.pdf"), "plain text named pdf")
	mustWrite(filepath.Join(root, "big.txt"), strings.Repeat("x", EvidenceArtifactPreviewMaxBytes+10))
	mustWrite(filepath.Join(outside, "secret.txt"), "outside secret")
	mustWrite(filepath.Join(outside, "dir", "inner.txt"), "outside dir")
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "dir"), filepath.Join(root, "linkdir")); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(outside, "secret.txt"), filepath.Join(root, "hard.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "huge.log"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(filepath.Join(root, "huge.log"), EvidenceArtifactRawMaxBytes+1); err != nil {
		t.Fatal(err)
	}

	artifacts, truncated, err := store.ListRunArtifacts(runID)
	if err != nil || truncated {
		t.Fatalf("list = %v, %v", truncated, err)
	}
	var names []string
	for _, artifact := range artifacts {
		names = append(names, artifact.Name)
	}
	if got := strings.Join(names, ","); got != "big.txt,blob.bin,data.json,fake.pdf,huge.log,logs/run.log,paper.pdf,plan.md,shot.png" {
		t.Fatalf("listed %s", got)
	}

	kinds := map[string]string{"plan.md": "markdown", "logs/run.log": "text", "data.json": "json", "blob.bin": "binary", "shot.png": "image", "paper.pdf": "pdf", "fake.pdf": "text"}
	for name, kind := range kinds {
		preview, err := store.PreviewRunArtifact(runID, name)
		if err != nil || preview.Kind != kind || (preview.Text != nil) != (kind != "binary" && kind != "image" && kind != "pdf") {
			t.Fatalf("%s preview = %+v, %v; want kind %s", name, preview, err, kind)
		}
	}
	plan, _ := store.PreviewRunArtifact(runID, "plan.md")
	if plan.Text.Text != "# Plan\napi_key=[REDACTED]\n" {
		t.Fatalf("plan preview = %q", plan.Text.Text)
	}
	big, err := store.PreviewRunArtifact(runID, "big.txt")
	if err != nil || !big.Text.Truncated || len(big.Text.Text) != EvidenceArtifactPreviewMaxBytes || big.Text.Bytes != EvidenceArtifactPreviewMaxBytes+10 {
		t.Fatalf("big preview = %d bytes of %d, truncated %v, %v", len(big.Text.Text), big.Text.Bytes, big.Text.Truncated, err)
	}
	content, err := store.ReadRunArtifact(runID, "plan.md")
	if err != nil || content.ContentType != "text/plain; charset=utf-8" || string(content.Body) != "# Plan\napi_key=[REDACTED]\n" {
		t.Fatalf("raw plan = %+v, %v", content, err)
	}
	if paper, err := store.ReadRunArtifact(runID, "paper.pdf"); err != nil || paper.ContentType != "application/pdf" {
		t.Fatalf("raw pdf = %+v, %v", paper, err)
	}
	if fake, err := store.ReadRunArtifact(runID, "fake.pdf"); err != nil || fake.ContentType != "text/plain; charset=utf-8" {
		t.Fatalf("raw text named pdf = %+v, %v", fake, err)
	}
	if _, err := store.ReadRunArtifact(runID, "huge.log"); !errors.Is(err, ErrEvidenceTooLarge) {
		t.Fatalf("huge raw read err = %v", err)
	}
	for _, name := range []string{"link.txt", "linkdir/inner.txt", "hard.txt", "../" + runID + "/plan.md", "logs/../plan.md", "/etc/passwd", "", "logs//run.log", "missing.md", "logs"} {
		if _, err := store.PreviewRunArtifact(runID, name); !errors.Is(err, ErrNotFound) {
			t.Fatalf("preview %q err = %v, want ErrNotFound", name, err)
		}
		if _, err := store.ReadRunArtifact(runID, name); !errors.Is(err, ErrNotFound) {
			t.Fatalf("raw %q err = %v, want ErrNotFound", name, err)
		}
	}
	if _, _, err := store.ListRunArtifacts("run_missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown run list err = %v", err)
	}

	briefs := filepath.Join(store.Workspace, "briefs")
	mustWrite(filepath.Join(briefs, "seat-good.md"), "brief: build\npassword: letmein\n")
	mustWrite(filepath.Join(outside, "stolen.md"), "outside brief")
	if err := os.Symlink(filepath.Join(outside, "stolen.md"), filepath.Join(briefs, "seat-link.md")); err != nil {
		t.Fatal(err)
	}
	dispatch := func(path string) int {
		t.Helper()
		if err := store.AppendRunEvent(runID, RunEvent{Type: RunEventSlotDispatch, NodeID: "fmn_work", SlotID: "slot_work", Attempt: 1, Data: map[string]any{"briefPath": path}}); err != nil {
			t.Fatal(err)
		}
		events, err := store.ReadRunEvents(runID)
		if err != nil {
			t.Fatal(err)
		}
		return events[len(events)-1].Seq
	}
	good := dispatch(filepath.Join(briefs, "seat-good.md"))
	brief, err := store.ReadRunBrief(runID, good)
	if err != nil || brief.NodeID != "fmn_work" || brief.SlotID != "slot_work" || brief.Text.Text != "brief: build\npassword=[REDACTED]\n" {
		t.Fatalf("brief = %+v, %v", brief, err)
	}
	for _, path := range []string{filepath.Join(briefs, "seat-link.md"), filepath.Join(outside, "stolen.md"), briefs + "/../briefs/seat-good.md", "briefs/seat-good.md", ""} {
		if _, err := store.ReadRunBrief(runID, dispatch(path)); !errors.Is(err, ErrNotFound) {
			t.Fatalf("brief at %q err = %v, want ErrNotFound", path, err)
		}
	}
	if _, err := store.ReadRunBrief(runID, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-dispatch seq err = %v", err)
	}
}
