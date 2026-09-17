package coordinator

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

func getEvidence(c *Coordinator, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c.Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	return w
}

func decodeEvidence[T any](t *testing.T, w *httptest.ResponseRecorder, key string) T {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	var value T
	if err := json.Unmarshal(body.Data[key], &value); err != nil {
		t.Fatalf("decode %s: %v in %s", key, err, w.Body.String())
	}
	return value
}

func TestEvidenceRoutesServeNodeOutputsAndGateResponses(t *testing.T) {
	c, executor, _ := fixture(t)
	id := startRun(t, c)
	<-executor.entered
	executor.proceed <- struct{}{}
	seq := awaitState(t, c, id, "waiting_human").WaitingGates[0].RequestedSeq
	for _, invalid := range []string{`"-slot"`, `"slot work"`, `"` + strings.Repeat("s", 65) + `"`} {
		if w := post(t, c, "/api/formations/runs/"+id+"/gates/gate_review/verdict", `{"requestedSeq":`+strconv.Itoa(seq)+`,"verdict":"pass","relayedBy":`+invalid+`}`); w.Code != 400 || !strings.Contains(w.Body.String(), "relayedBy") {
			t.Fatalf("relayedBy %s = %d %s", invalid, w.Code, w.Body.String())
		}
	}
	if w := post(t, c, "/api/formations/runs/"+id+"/gates/gate_review/verdict", `{"requestedSeq":`+strconv.Itoa(seq)+`,"verdict":"pass","reason":"use Postgres","relayedBy":"slot_work-1"}`); w.Code != 202 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	<-executor.entered
	executor.proceed <- struct{}{}
	awaitState(t, c, id, "succeeded")

	base := "/api/formations/runs/" + id + "/evidence/nodes/"
	w := getEvidence(c, base+"fmn_work")
	work := decodeEvidence[formations.NodeEvidence](t, w, "evidence")
	if work.Kind != "formation" || len(work.Attempts) != 1 || work.Attempts[0].Output == nil {
		t.Fatalf("work evidence = %s", w.Body.String())
	}
	output := work.Attempts[0].Output
	if output.Text.Text != "PRIVATE-CAPTURE" || len(output.Ports) != 1 || output.Ports[0].Text.Text != "PRIVATE-OUTPUT" || output.Ports[0].Ref.External != "raw" {
		t.Fatalf("work output = %s", w.Body.String())
	}
	for _, private := range []string{"/private/", "sessionRef", "nativeSessionId", "paneId", "promptSha256", "briefPath", "reportRef"} {
		if strings.Contains(w.Body.String(), private) {
			t.Fatalf("node evidence leaked %q: %s", private, w.Body.String())
		}
	}

	w = getEvidence(c, base+"gate_review")
	gate := decodeEvidence[formations.NodeEvidence](t, w, "evidence")
	if len(gate.Evaluations) != 1 || gate.Evaluations[0].Criterion.Text != "PRIVATE-CRITERION" || gate.Evaluations[0].Input.Text.Text != "PRIVATE-OUTPUT" {
		t.Fatalf("gate evidence = %s", w.Body.String())
	}
	evaluation := gate.Evaluations[0]
	if len(evaluation.HumanRequests) != 1 || evaluation.HumanRequests[0].Pending || evaluation.HumanRequests[0].Decision.Response.Text != "use Postgres" ||
		evaluation.HumanRequests[0].Decision.DecidedBy != "human:operator" || evaluation.HumanRequests[0].Decision.RelayedBy != "slot_work-1" {
		t.Fatalf("human request = %s", w.Body.String())
	}
	if evaluation.Verdict == nil || evaluation.Verdict.Verdict != "pass" || evaluation.Verdict.RoutePort != "pass" {
		t.Fatalf("gate verdict = %s", w.Body.String())
	}

	for _, missing := range []string{base + "fmn_missing", "/api/formations/runs/run_missing/evidence/nodes/fmn_work", "/api/formations/runs/not-a-run/evidence/nodes/fmn_work"} {
		if w := getEvidence(c, missing); w.Code != 404 {
			t.Fatalf("%s: %d %s", missing, w.Code, w.Body.String())
		}
	}
}

func TestEvidenceRouteServesBlocksThatNameNoNode(t *testing.T) {
	c, executor, _ := fixture(t)
	id := startRun(t, c)
	<-executor.entered
	executor.proceed <- struct{}{}
	awaitState(t, c, id, "waiting_human")
	for _, event := range []formations.RunEvent{
		{Type: formations.RunEventError, Data: map[string]any{"code": "wall_clock_exceeded", "message": "wall clock limit exceeded"}},
		{Type: formations.RunEventBlocked, Data: map[string]any{"reason": "wall clock limit exceeded", "resumeAllowed": true}},
	} {
		if err := c.store.AppendRunEvent(id, event); err != nil {
			t.Fatal(err)
		}
	}
	w := getEvidence(c, "/api/formations/runs/"+id+"/evidence/problems")
	problems := decodeEvidence[[]formations.RunProblem](t, w, "problems")
	if len(problems) != 2 {
		t.Fatalf("problems = %s", w.Body.String())
	}
	if block := problems[1]; block.Type != formations.RunEventBlocked || block.Reason.Text != "wall clock limit exceeded" || len(block.NodeIDs) != 0 || block.ResumeAllowed == nil || !*block.ResumeAllowed {
		t.Fatalf("block = %s", w.Body.String())
	}
	for _, missing := range []string{"/api/formations/runs/run_missing/evidence/problems", "/api/formations/runs/not-a-run/evidence/problems"} {
		if w := getEvidence(c, missing); w.Code != 404 {
			t.Fatalf("%s: %d %s", missing, w.Code, w.Body.String())
		}
	}
}

func TestNodeEvidenceKeepsFrozenDefinitionAfterBoardEdits(t *testing.T) {
	c, executor, _ := fixture(t)
	id := startRun(t, c)
	<-executor.entered
	executor.proceed <- struct{}{}
	seq := awaitState(t, c, id, "waiting_human").WaitingGates[0].RequestedSeq
	if w := post(t, c, "/api/formations/runs/"+id+"/gates/gate_review/verdict", `{"requestedSeq":`+strconv.Itoa(seq)+`,"verdict":"pass"}`); w.Code != 202 {
		t.Fatalf("verdict: %d %s", w.Code, w.Body.String())
	}
	<-executor.entered
	executor.proceed <- struct{}{}
	awaitState(t, c, id, "succeeded")

	path := "/api/formations/runs/" + id + "/evidence/nodes/fmn_work"
	before := decodeEvidence[formations.NodeEvidence](t, getEvidence(c, path), "evidence")
	if before.Definition == nil || before.Definition.Title != "Work" || len(before.Definition.Outputs) != 1 || len(before.Definition.Outgoing) != 1 {
		t.Fatalf("missing frozen definition: %+v", before.Definition)
	}
	original, err := os.ReadFile(c.store.BoardPath("proof"))
	if err != nil {
		t.Fatal(err)
	}
	for name, edited := range map[string]string{
		"rename":   strings.ReplaceAll(strings.ReplaceAll(string(original), `title = "Work"`, `title = "Renamed work"`), `label = "Output"`, `label = "Renamed output"`),
		"rewiring": strings.ReplaceAll(string(original), `to = "gate_review:in"`, `to = "fmn_after:port_after_in"`),
		"deletion": "schema = 1\nid = \"brd_proof\"\nslug = \"proof\"\ntitle = \"Empty now\"\nrev = 2\n",
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(c.store.BoardPath("proof"), []byte(edited), 0600); err != nil {
				t.Fatal(err)
			}
			after := decodeEvidence[formations.NodeEvidence](t, getEvidence(c, path), "evidence")
			if !reflect.DeepEqual(before.Definition, after.Definition) || !reflect.DeepEqual(before.Attempts, after.Attempts) {
				t.Fatalf("current-board %s changed historical evidence: before=%+v after=%+v", name, before, after)
			}
		})
	}
}

func TestEvidenceRoutesCapBriefsArtifactsAndNodeText(t *testing.T) {
	c, executor, root := fixture(t)
	id := startRun(t, c)
	<-executor.entered
	executor.proceed <- struct{}{}
	awaitState(t, c, id, "waiting_human")
	runPath := "/api/formations/runs/" + id

	long := strings.Repeat("y", formations.EvidenceTextMaxBytes+100)
	if err := c.store.AppendRunEvent(id, formations.RunEvent{Type: formations.RunEventNodeOutput, NodeID: "fmn_after", Data: map[string]any{"status": "done", "text": long}}); err != nil {
		t.Fatal(err)
	}
	after := decodeEvidence[formations.NodeEvidence](t, getEvidence(c, runPath+"/evidence/nodes/fmn_after"), "evidence")
	if text := after.Attempts[0].Output.Text; !text.Truncated || text.Bytes != len(long) || len(text.Text) != formations.EvidenceTextMaxBytes {
		t.Fatalf("node text cap = %d of %d, truncated %v", len(text.Text), text.Bytes, text.Truncated)
	}

	briefs := filepath.Join(root, "briefs")
	if err := os.MkdirAll(briefs, 0o700); err != nil {
		t.Fatal(err)
	}
	briefPath := filepath.Join(briefs, "seat-evidence.md")
	if err := os.WriteFile(briefPath, []byte("run the proof\n"+strings.Repeat("b", formations.EvidenceBriefMaxBytes)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := c.store.AppendRunEvent(id, formations.RunEvent{Type: formations.RunEventSlotDispatch, NodeID: "fmn_after", SlotID: "slot_after", Attempt: 1, Data: map[string]any{"briefPath": briefPath, "sessionRef": "tmux:PRIVATE-SESSION"}}); err != nil {
		t.Fatal(err)
	}
	events, err := c.store.ReadRunEvents(id)
	if err != nil {
		t.Fatal(err)
	}
	dispatchSeq := strconv.Itoa(events[len(events)-1].Seq)
	w := getEvidence(c, runPath+"/evidence/briefs/"+dispatchSeq)
	brief := decodeEvidence[formations.RunBriefEvidence](t, w, "brief")
	if brief.NodeID != "fmn_after" || !strings.HasPrefix(brief.Text.Text, "run the proof\n") || !brief.Text.Truncated || len(brief.Text.Text) != formations.EvidenceBriefMaxBytes {
		t.Fatalf("brief = %+v", brief.Text.Truncated)
	}
	if strings.Contains(w.Body.String(), "PRIVATE-SESSION") || strings.Contains(w.Body.String(), briefs) {
		t.Fatalf("brief leaked identity or path")
	}
	for _, missing := range []string{"/evidence/briefs/1", "/evidence/briefs/0", "/evidence/briefs/abc", "/evidence/briefs/99999"} {
		if w := getEvidence(c, runPath+missing); w.Code != 404 {
			t.Fatalf("%s: %d %s", missing, w.Code, w.Body.String())
		}
	}

	artifacts := filepath.Join(root, ".formations", "artifacts", id)
	outside := t.TempDir()
	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(artifacts, "report.md"), "# Report\nsecret: sk-abcdefghijklmnop\n")
	write(filepath.Join(artifacts, "page.html"), "<script>alert(1)</script>")
	write(filepath.Join(artifacts, "blob.bin"), "\x00\x01")
	write(filepath.Join(artifacts, "paper.pdf"), "%PDF-1.7\n")
	write(filepath.Join(artifacts, "big.log"), strings.Repeat("z", formations.EvidenceArtifactPreviewMaxBytes+1))
	write(filepath.Join(outside, "secret.txt"), "OUTSIDE-SECRET")
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(artifacts, "escape.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(artifacts, "huge.log"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(filepath.Join(artifacts, "huge.log"), formations.EvidenceArtifactRawMaxBytes+1); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < formations.EvidenceArtifactListMax; i++ {
		write(filepath.Join(artifacts, "many", "file-"+strconv.Itoa(1000+i)+".txt"), "x")
	}

	w = getEvidence(c, runPath+"/evidence/artifacts")
	list := decodeEvidence[[]formations.RunArtifactEntry](t, w, "artifacts")
	truncated := decodeEvidence[bool](t, w, "truncated")
	if len(list) != formations.EvidenceArtifactListMax || !truncated || list[0].Name != "big.log" || strings.Contains(w.Body.String(), "escape.txt") || strings.Contains(w.Body.String(), artifacts) {
		t.Fatalf("artifact list: %d entries, truncated %v, first %+v", len(list), truncated, list[0])
	}

	preview := decodeEvidence[formations.RunArtifactPreview](t, getEvidence(c, runPath+"/evidence/artifacts/report.md"), "artifact")
	if preview.Kind != "markdown" || preview.Text.Text != "# Report\nsecret=[REDACTED]\n" {
		t.Fatalf("report preview = %+v", preview)
	}
	big := decodeEvidence[formations.RunArtifactPreview](t, getEvidence(c, runPath+"/evidence/artifacts/big.log"), "artifact")
	if !big.Text.Truncated || len(big.Text.Text) != formations.EvidenceArtifactPreviewMaxBytes || big.Size != int64(formations.EvidenceArtifactPreviewMaxBytes+1) {
		t.Fatalf("big preview truncated %v, %d bytes", big.Text.Truncated, len(big.Text.Text))
	}

	raw := getEvidence(c, runPath+"/artifacts/report.md")
	if raw.Code != 200 || raw.Body.String() != "# Report\nsecret=[REDACTED]\n" {
		t.Fatalf("raw report %d %q", raw.Code, raw.Body.String())
	}
	for header, want := range map[string]string{
		"Content-Type":            "text/plain; charset=utf-8",
		"X-Content-Type-Options":  "nosniff",
		"Cache-Control":           "no-store",
		"Content-Disposition":     `inline; filename=report.md`,
		"Content-Security-Policy": "sandbox; default-src 'none'; img-src 'self'; style-src 'unsafe-inline'",
	} {
		if got := raw.Header().Get(header); got != want {
			t.Fatalf("%s = %q, want %q", header, got, want)
		}
	}
	if page := getEvidence(c, runPath+"/artifacts/page.html"); page.Code != 200 || page.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("html artifact served as %q", page.Header().Get("Content-Type"))
	}
	if blob := getEvidence(c, runPath+"/artifacts/blob.bin"); blob.Header().Get("Content-Type") != "application/octet-stream" || !strings.HasPrefix(blob.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("binary artifact headers = %v", blob.Header())
	}
	if pdf := getEvidence(c, runPath+"/artifacts/paper.pdf"); pdf.Header().Get("Content-Type") != "application/pdf" || pdf.Header().Get("Content-Disposition") != "inline; filename=paper.pdf" || !strings.HasPrefix(pdf.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("pdf artifact headers = %v", pdf.Header())
	}
	if huge := getEvidence(c, runPath+"/artifacts/huge.log"); huge.Code != 413 {
		t.Fatalf("huge raw artifact: %d", huge.Code)
	}
	for _, missing := range []string{
		"/artifacts/escape.txt", "/evidence/artifacts/escape.txt",
		"/artifacts/..%2F..%2F..%2Fbriefs%2Fseat-evidence.md", "/evidence/artifacts/many%2F..%2F..%2Fsecret.txt",
		"/artifacts/missing.md", "/evidence/artifacts/many",
	} {
		// The evidence handler, not a routing miss, refuses these names.
		if w := getEvidence(c, runPath+missing); w.Code != 404 || !strings.Contains(w.Body.String(), "run evidence not found") {
			t.Fatalf("%s: %d %s", missing, w.Code, w.Body.String())
		}
	}
	for _, unknown := range []string{"/api/formations/runs/run_missing/evidence/artifacts", "/api/formations/runs/run_missing/artifacts/report.md", "/api/formations/runs/run_missing/evidence/briefs/" + dispatchSeq} {
		if w := getEvidence(c, unknown); w.Code != 404 {
			t.Fatalf("%s: %d %s", unknown, w.Code, w.Body.String())
		}
	}
}
