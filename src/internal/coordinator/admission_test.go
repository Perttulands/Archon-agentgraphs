package coordinator

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

const draftBoard = `schema = 1
id = "brd_draft"
slug = "draft"
title = "Draft"
rev = 4
[[mission]]
id = "mis_draft"
title = "Draft"
goal = "Sketch"
[[formation]]
id = "fmn_plan"
type = "solo"
title = "Plan"
[[formation.input]]
id = "port_in"
label = "Input"
[[formation.output]]
id = "port_out"
label = "Output"
[[formation.slot]]
id = "slot_plan"
label = "Planner"
[[gate]]
id = "gate_lint"
title = "Lint"
kinds = ["code"]
criterion = ""
check = "output_absent"
checkVersion = "1"
[[gate]]
id = "gate_review"
title = "Review"
kinds = ["formation"]
criterion = ""
[[connection]]
id = "edge_start"
from = "mis_draft:out"
to = "fmn_plan:port_in"
[[connection]]
id = "edge_lint"
from = "fmn_plan:port_out"
to = "gate_lint:in"
[[connection]]
id = "edge_review"
from = "gate_lint:pass"
to = "gate_review:in"
`

func TestRunStartReturnsEveryAdmissionFindingAs422(t *testing.T) {
	c, _, _ := fixture(t)
	if err := os.WriteFile(c.store.BoardPath("draft"), []byte(draftBoard), 0600); err != nil {
		t.Fatal(err)
	}
	start := func(expectedRev int) *httptest.ResponseRecorder {
		return post(t, c, "/api/formations/runs", `{"cwd":`+strconv.Quote(c.store.Workspace)+`,"brief":"sketch","board":"draft","missionId":"mis_draft","expectedRev":`+strconv.Itoa(expectedRev)+`,"limits":{"maxDispatch":3,"maxAttempts":1,"wallClockSeconds":600,"redact":false}}`)
	}

	if stale := start(3); stale.Code != 409 {
		t.Fatalf("stale revision start = %d %s, want 409 before findings", stale.Code, stale.Body.String())
	}
	w := start(4)
	if w.Code != 422 {
		t.Fatalf("draft start = %d %s, want 422", w.Code, w.Body.String())
	}
	var body struct {
		Success bool `json:"success"`
		Error   struct {
			Code     string                    `json:"code"`
			Message  string                    `json:"message"`
			Findings []formations.BoardFinding `json:"findings"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"fmn_plan":    formations.FindingUnstaffedSlot,
		"gate_lint":   formations.FindingGateNotRoutable,
		"gate_review": formations.FindingGateNotRoutable,
	}
	got := map[string]string{}
	for _, finding := range body.Error.Findings {
		if finding.Message == "" {
			t.Fatalf("finding without message: %+v", finding)
		}
		got[finding.NodeID] = finding.Code
	}
	if body.Success || body.Error.Code != "RUN_ADMISSION_FAILED" || body.Error.Message != "The run needs 3 fixes before it can start" || len(got) != len(want) {
		t.Fatalf("422 body = %s, want every finding", w.Body.String())
	}
	for node, code := range want {
		if got[node] != code {
			t.Fatalf("finding for %s = %q, want %q in %s", node, got[node], code, w.Body.String())
		}
	}
	runs, err := c.store.ListRuns(formations.RunListFilter{})
	if err != nil || len(runs) != 0 {
		t.Fatalf("rejected admission recorded runs %+v (%v)", runs, err)
	}

	validation := httptest.NewRecorder()
	c.Handler().ServeHTTP(validation, httptest.NewRequest("GET", "/api/formations/boards/draft/validation", nil))
	var report struct {
		Data struct {
			BoardRev int                       `json:"boardRev"`
			Errors   []formations.BoardFinding `json:"errors"`
		} `json:"data"`
	}
	if err := json.Unmarshal(validation.Body.Bytes(), &report); err != nil || validation.Code != 200 {
		t.Fatalf("validation route = %d %s (%v)", validation.Code, validation.Body.String(), err)
	}
	if report.Data.BoardRev != 4 || len(report.Data.Errors) != 3 {
		t.Fatalf("validation route body = %s, want the same three findings", validation.Body.String())
	}
}
