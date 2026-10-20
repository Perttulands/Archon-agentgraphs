package coordinator

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

func TestRunSummaryRetainsOpenSessionsAndBoundedInput(t *testing.T) {
	status := &formations.RunStatusProjection{RunID: "run_index", Status: formations.RunStatusRunning, BoardSlug: "proof", BoardID: "brd_proof", BoardRev: 1, MissionID: "mis_proof", EventCount: 8, Inputs: []formations.RunInput{{Name: "folder", Kind: "folder", Value: "/work"}, {Name: "brief", Kind: "text", Value: "  one\n\t" + strings.Repeat("ö", 100)}}}
	seat := func(kind, name, node, outcome string) formations.RunEvent {
		return formations.RunEvent{Type: kind, NodeID: node, SlotID: "slot_" + node, Data: map[string]any{"sessionName": name, "outcome": outcome}}
	}
	events := []formations.RunEvent{
		seat(formations.RunEventSeatCreated, "one", "work", ""),
		seat(formations.RunEventSeatCreated, "two", "judge", ""),
		seat(formations.RunEventSeatCleanup, "one", "work", "kept_on_call"),
		seat(formations.RunEventSeatCleanup, "two", "judge", "ended"),
		seat(formations.RunEventSeatCreated, "two", "after", ""),
		{Seq: 8, Type: formations.RunEventHumanInputRequested, GateID: "gate_review"},
	}
	summary := summarizeRun(status, events)
	if summary.Status != "waiting_human" || summary.EventCount != 8 {
		t.Fatalf("summary state=%+v", summary)
	}
	want := []RunSessionSummary{{SessionName: "one", NodeID: "work", SlotID: "slot_work"}, {SessionName: "two", NodeID: "after", SlotID: "slot_after"}}
	if !reflect.DeepEqual(summary.Sessions, want) {
		t.Fatalf("sessions=%+v want %+v", summary.Sessions, want)
	}
	if !strings.HasPrefix(summary.InputExcerpt, "one ") || utf8.RuneCountInString(summary.InputExcerpt) != 90 || !strings.HasSuffix(summary.InputExcerpt, "…") {
		t.Fatalf("excerpt=%q", summary.InputExcerpt)
	}
	status.Status = formations.RunStatusBlocked
	if got := summarizeRun(status, events); got.Status != formations.RunStatusBlocked {
		t.Fatalf("waiting gate masked block: %+v", got)
	}
	status.Status = formations.RunStatusPicked
	status.Final = true
	if got := summarizeRun(status, events); len(got.Sessions) != 0 || got.Status != formations.RunStatusPicked {
		t.Fatalf("final summary=%+v", got)
	}
	if got := runInputExcerpt([]formations.RunInput{{Kind: "file", Value: "  /work/report.md\n"}}); got != "/work/report.md" {
		t.Fatalf("non-text excerpt=%q", got)
	}
}

// Match the audit's 51-run / ~3,222-event scale without launching seats. The
// HTTP index must not serialize those histories or the full input values.
func TestRunIndexKeepsDiscoveryCompactAcrossLargeHistories(t *testing.T) {
	c, _, _ := fixture(t)
	const runs = 51
	var previous []Projection
	for i := 0; i < runs; i++ {
		started, err := c.store.StartRun("proof", formations.RunStartRequest{MissionID: "mis_proof", Personas: c.personas, Inputs: map[string]string{"brief": strings.Repeat(fmt.Sprintf("run %d input ", i), 400)}})
		if err != nil {
			t.Fatal(err)
		}
		events, err := c.store.ReadRunEvents(started.RunID)
		if err != nil {
			t.Fatal(err)
		}
		file, err := os.OpenFile(filepath.Join(c.store.Workspace, ".archon", "runs", "proof", started.RunID+".ndjson"), os.O_WRONLY|os.O_APPEND, 0)
		if err != nil {
			t.Fatal(err)
		}
		encoder := json.NewEncoder(file)
		for seq := 2; seq <= 63; seq++ {
			event := formations.RunEvent{Timestamp: events[0].Timestamp, RunID: started.RunID, Seq: seq, Actor: "agent:test", Type: formations.RunEventSeatState, NodeID: "fmn_work", SlotID: "slot_work", Data: map[string]any{"state": "working"}}
			if seq == 63 {
				event.Type = formations.RunEventSucceeded
				event.Data = map[string]any{"final": true}
			}
			if err := encoder.Encode(event); err != nil {
				file.Close()
				t.Fatal(err)
			}
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		detail, err := c.Project(started.RunID)
		if err != nil {
			t.Fatal(err)
		}
		previous = append(previous, *detail)
	}
	old, err := json.Marshal(previous)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	c.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/runs", nil))
	var response struct {
		Data []map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || w.Code != 200 || len(response.Data) != runs {
		t.Fatalf("index=%d %s (%v)", w.Code, w.Body.String(), err)
	}
	for _, summary := range response.Data {
		for _, forbidden := range []string{"events", "inputs", "outputs", "waitingGates", "onCallSeats", "contextPaths", "cwd"} {
			if _, present := summary[forbidden]; present {
				t.Fatalf("index contains %s", forbidden)
			}
		}
		if string(summary["sessions"]) != "[]" {
			t.Fatalf("final sessions=%s", summary["sessions"])
		}
	}
	if w.Body.Len() >= len(old)/5 {
		t.Fatalf("compact index %d bytes vs former projections %d", w.Body.Len(), len(old))
	}
	t.Logf("51 runs / %d events: former full list %d bytes; compact HTTP index %d bytes (%.1f%% smaller)", runs*63, len(old), w.Body.Len(), 100*(1-float64(w.Body.Len())/float64(len(old))))
	// Discovery must include a run admitted externally after the last index,
	// even when every run the previous index held was final.
	external, err := c.store.StartRun("proof", formations.RunStartRequest{MissionID: "mis_proof", Personas: c.personas, Inputs: map[string]string{"brief": "new external start"}})
	if err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	c.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/runs?mission=proof", nil))
	var updated struct {
		Data []RunSummary `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &updated); err != nil || len(updated.Data) != runs+1 {
		t.Fatalf("external discovery failed: %v %s", err, w.Body.String())
	}
	found := false
	for _, summary := range updated.Data {
		found = found || summary.RunID == external.RunID && summary.Status == formations.RunStatusRunning
	}
	if !found {
		t.Fatal("new external run was omitted")
	}
	// A selected read retains its full authoritative history.
	detail := httptest.NewRecorder()
	c.Handler().ServeHTTP(detail, httptest.NewRequest("GET", "/api/runs/"+previous[0].RunID, nil))
	var selected struct {
		Data Projection `json:"data"`
	}
	if err := json.Unmarshal(detail.Body.Bytes(), &selected); err != nil || len(selected.Data.Events) != 63 {
		t.Fatalf("selected history was truncated: %v %s", err, detail.Body.String())
	}
}
