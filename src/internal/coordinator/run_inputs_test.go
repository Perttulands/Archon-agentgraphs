package coordinator

import (
	"encoding/json"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// A run start supplies the mission's inputs by name, for a mission and a
// single step alike, and the projection shows them (archon-o7p.3).
func TestRunStartSuppliesInputsAndAdmissionNamesWhatIsMissing(t *testing.T) {
	c, e, _ := fixture(t)
	declared := strings.Replace(testBoard, `goal = "PRIVATE-OBJECTIVE"`, `goal = "PRIVATE-OBJECTIVE"
inputs = [{ name = "topic", kind = "text", required = true, description = "What to explore" }, { name = "repo", kind = "folder" }]`, 1)
	if err := os.WriteFile(c.store.BoardPath("proof"), []byte(declared), 0600); err != nil {
		t.Fatal(err)
	}
	findings := func(body string) []formations.BoardFinding {
		t.Helper()
		w := post(t, c, "/api/runs", body)
		if w.Code != 422 {
			t.Fatalf("start = %d %s, want 422", w.Code, w.Body.String())
		}
		var envelope struct {
			Error struct {
				Code     string                    `json:"code"`
				Findings []formations.BoardFinding `json:"findings"`
			} `json:"error"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil || envelope.Error.Code != "RUN_ADMISSION_FAILED" {
			t.Fatalf("refusal %s (%v)", w.Body.String(), err)
		}
		return envelope.Error.Findings
	}
	for _, target := range []string{`"inputCardId":"mis_proof"`, `"formationId":"fmn_work"`} {
		got := findings(`{"mission":"proof",` + target + `,"expectedRev":1,"inputs":{"repo":"relative/dir","colour":"blue"}}`)
		want := []formations.BoardFinding{
			{Code: formations.FindingInvalidInputValue, NodeID: "mis_proof", Message: `input repo must be the absolute path of an existing directory; "relative/dir" is a relative path`},
			{Code: formations.FindingMissingInput, NodeID: "mis_proof", Message: "input topic is required: What to explore"},
			{Code: formations.FindingUnknownInput, NodeID: "mis_proof", Message: `mission proof has no input named "colour"; its inputs are topic, repo`},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s findings = %+v, want %+v", target, got, want)
		}
		// Omitting inputs is the same as supplying none.
		if got := findings(`{"mission":"proof",` + target + `,"expectedRev":1}`); len(got) != 1 || got[0].Code != formations.FindingMissingInput {
			t.Fatalf("%s without inputs = %+v", target, got)
		}
	}
	// The brief is an input now; the old field is refused by name.
	if w := post(t, c, "/api/runs", `{"mission":"proof","inputCardId":"mis_proof","expectedRev":1,"brief":"x"}`); w.Code != 400 || !strings.Contains(w.Body.String(), `unknown field \"brief\"`) {
		t.Fatalf("brief field = %d %s, want 400 naming it", w.Code, w.Body.String())
	}

	repo := t.TempDir()
	w := post(t, c, "/api/runs", `{"mission":"proof","formationId":"fmn_work","expectedRev":1,"beadId":"archon-o7p.3","contextPaths":[`+strconv.Quote(repo)+`],"inputs":{"topic":"captions","repo":`+strconv.Quote(repo)+`}}`)
	if w.Code != 202 {
		t.Fatalf("single step start = %d %s", w.Code, w.Body.String())
	}
	var receipt struct {
		Data struct {
			RunID string `json:"runId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	<-e.entered
	e.proceed <- struct{}{}
	p := awaitState(t, c, receipt.Data.RunID, "succeeded")
	if !reflect.DeepEqual(p.Inputs, []formations.RunInput{{Name: "topic", Kind: "text", Value: "captions"}, {Name: "repo", Kind: "folder", Value: repo}}) {
		t.Fatalf("projected inputs = %+v", p.Inputs)
	}
	if p.BeadID != "archon-o7p.3" || !reflect.DeepEqual(p.ContextPaths, []string{repo}) || !strings.HasSuffix(p.Cwd, receipt.Data.RunID) {
		t.Fatalf("single step run fields = cwd %q bead %q context %v", p.Cwd, p.BeadID, p.ContextPaths)
	}
}
