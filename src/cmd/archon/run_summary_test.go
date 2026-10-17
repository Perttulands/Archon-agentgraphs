package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

func TestRunAttemptsFilesAreOptInAndJSONKeepsEvidence(t *testing.T) {
	evidence := formations.RunAttemptsEvidence{RunID: "run_proof", Steps: []formations.StepAttempts{{
		NodeID: "fmn_work", Title: "Work", Attempts: []formations.StepAttempt{{
			Attempt: 1, Status: "done", Directory: &formations.AttemptDirectory{Path: "/artifacts/work/1"},
			Outputs:  []formations.AttemptOutput{{Label: "report", Kind: "file", Path: "/artifacts/report.md"}},
			Verdicts: []formations.AttemptVerdict{{GateID: "gate_review", Verdict: "fail", Reason: formations.EvidenceText{Text: "Missing citations\nDetailed findings"}}},
			Files:    []formations.AttemptFile{{Name: "work/1/transcript.json", Size: 100, Kind: "json"}}, FilesTruncated: true,
		}},
	}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/runs/run_proof/evidence/attempts" || r.URL.Query().Get("node") != "fmn_work" {
			t.Errorf("unexpected request %s", r.URL)
		}
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"attempts": evidence}})
	}))
	defer server.Close()
	for _, tc := range []struct {
		name        string
		flags       []string
		files, json bool
	}{
		{name: "default"}, {name: "files", flags: []string{"--files"}, files: true},
		{name: "json", flags: []string{"--json"}, json: true},
		{name: "json with files", flags: []string{"--files", "--json"}, files: true, json: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, stderr bytes.Buffer
			args := append([]string{"run", "attempts", "run_proof", "--node", "fmn_work"}, tc.flags...)
			if code := runRemote(server.URL, args, &out, &stderr); code != 0 {
				t.Fatalf("exit %d: %s", code, &stderr)
			}
			got := out.String()
			if tc.json {
				var envelope struct {
					Data struct {
						Attempts formations.RunAttemptsEvidence `json:"attempts"`
					} `json:"data"`
				}
				if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
					t.Fatal(err)
				}
				attempt := envelope.Data.Attempts.Steps[0].Attempts[0]
				if len(attempt.Files) != 1 || !attempt.FilesTruncated || attempt.Directory == nil || !strings.Contains(attempt.Verdicts[0].Reason.Text, "Detailed findings") {
					t.Fatalf("JSON lost evidence: %s", got)
				}
				return
			}
			for _, want := range []string{"step fmn_work", "attempt 1 done", "output \"report\" file /artifacts/report.md", "verdict gate_review fail: Missing citations"} {
				if !strings.Contains(got, want) {
					t.Errorf("missing %q in %s", want, got)
				}
			}
			if strings.Contains(got, "Detailed findings") {
				t.Errorf("verdict is not one line: %s", got)
			}
			for _, detail := range []string{"directory /artifacts/work/1", "file work/1/transcript.json", "not listed"} {
				if strings.Contains(got, detail) != tc.files {
					t.Errorf("files=%v, output %s", tc.files, got)
				}
			}
		})
	}
}

func TestFinalRunStatusExplainsTerminalOutcome(t *testing.T) {
	for _, tc := range []struct{ status, event, code, reason, actor string }{
		{"failed", formations.RunEventFailed, "limit_reached", "Work used 5 of 5 rounds", "archond"},
		{"canceled", formations.RunEventCanceled, "", "The operator stopped this run", "operator:perttu"},
		{"succeeded", "", "", "", ""}, {"picked", "", "", "", ""},
	} {
		t.Run(tc.status, func(t *testing.T) {
			status := formations.RunStatusProjection{RunID: "run_proof", Status: tc.status, Final: true, EndedBy: tc.actor}
			problems := []formations.RunProblem{{EvidenceProblem: formations.EvidenceProblem{Type: formations.RunEventBlocked, Code: "executor_failed", Reason: formations.EvidenceText{Text: "old resumed block"}}}}
			if tc.event != "" {
				problems = append(problems, formations.RunProblem{NodeIDs: []string{"fmn_work"}, EvidenceProblem: formations.EvidenceProblem{Type: tc.event, Code: tc.code, Reason: formations.EvidenceText{Text: tc.reason}, Actor: tc.actor}})
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/runs/run_proof":
					json.NewEncoder(w).Encode(map[string]any{"data": status})
				case "/api/runs/run_proof/evidence/mission":
					fmt.Fprint(w, `{"data":{"mission":{"graph":{"formations":[{"id":"fmn_work","title":"Frozen work"}]}}}}`)
				case "/api/runs/run_proof/evidence/problems":
					json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"problems": problems}})
				default:
					t.Errorf("unexpected request %s", r.URL)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			var out, stderr bytes.Buffer
			if code := runRemote(server.URL, []string{"run", "status", "run_proof"}, &out, &stderr); code != 0 {
				t.Fatalf("exit %d: %s", code, &stderr)
			}
			for _, want := range []string{"outcome\t" + tc.status, tc.reason, tc.code} {
				if !strings.Contains(out.String(), want) {
					t.Errorf("missing %q: %s", want, &out)
				}
			}
			if tc.event != "" {
				if !strings.Contains(out.String(), `step	fmn_work "Frozen work"`) {
					t.Fatalf("missing titled stop: %s", &out)
				}
				want := "reason\t"
				if tc.code != "" {
					want += tc.code + "\t"
				}
				want += tc.reason + "\n"
				if !strings.Contains(out.String(), want) {
					t.Errorf("missing exact reason line %q: %s", want, &out)
				}
			}
			if tc.actor != "" && !strings.Contains(out.String(), "endedBy\t"+tc.actor) {
				t.Errorf("missing ending actor: %s", &out)
			}
			if strings.Contains(out.String(), "old resumed block") {
				t.Errorf("reported old block as terminal reason: %s", &out)
			}
		})
	}
}

func TestRunAttemptAndStatusPrintReadableTimeAndStop(t *testing.T) {
	var out bytes.Buffer
	writeRunAttempts(&out, &formations.RunAttemptsEvidence{Steps: []formations.StepAttempts{{NodeID: "fmn_work", Attempts: []formations.StepAttempt{{Attempt: 1, Status: "stopped", StartedAt: "2026-10-17T10:00:00.123456789Z", EndedAt: "2026-10-17T10:02:03.123456789Z", StopReason: &formations.EvidenceText{Text: "executor disconnected"}}}}}}, false)
	if got := out.String(); !strings.Contains(got, "2m3s") || !strings.Contains(got, "reason executor disconnected") || strings.Contains(got, "123456789") {
		t.Fatalf("attempt text: %s", got)
	}
	out.Reset()
	writeRunStatusLines(&out, &formations.RunStatusProjection{BoardRev: 7, StartedBy: "agent:tester", StartedAt: "2026-10-17T10:00:00.123456789Z", UpdatedAt: "2026-10-17T10:02:03.123456789Z", Final: true})
	writeCurrentRunBlock(&out, []formations.RunProblem{{NodeIDs: []string{"fmn_work"}, EvidenceProblem: formations.EvidenceProblem{Type: formations.RunEventBlocked, Reason: formations.EvidenceText{Text: "executor disconnected"}}}}, &formations.BoardDocument{Formations: []formations.FormationNode{{ID: "fmn_work", Title: "Frozen work"}}})
	for _, want := range []string{"missionRev\t7", "startedBy\tagent:tester", "duration\t2m3s", "step\tfmn_work \"Frozen work\""} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q: %s", want, out.String())
		}
	}
}
