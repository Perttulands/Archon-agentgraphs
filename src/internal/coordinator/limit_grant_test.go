package coordinator

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// archon-o7p.8.1: a run stopped at a spent Limit card tells the owner to grant
// one more round, refuses a resume without the grant, and goes on with it.
func TestASpentLimitResumesOnlyWithAGrant(t *testing.T) {
	c, e, _ := fixture(t)
	// The mission may run one step: Work. The approved gate starts After,
	// which finds the mission's rounds spent.
	limited := testBoard + "[[limit]]\nid = \"lim_mission\"\ntitle = \"Cap\"\ntarget = \"mis_proof\"\nrounds = 1\n"
	if err := os.WriteFile(c.store.BoardPath("proof"), []byte(limited), 0600); err != nil {
		t.Fatal(err)
	}
	notifier := &recordingNotifier{}
	c.EnableNeedsYou(NeedsYouConfig{Notifier: notifier, ServerURL: "http://127.0.0.1:8091", RetryInterval: 20 * time.Millisecond})
	id := startRun(t, c)
	<-e.entered
	e.proceed <- struct{}{}
	waiting := awaitState(t, c, id, "waiting_human")
	if v := post(t, c, "/api/runs/"+id+"/gates/gate_review/verdict", `{"requestedSeq":`+strconv.Itoa(waiting.WaitingGates[0].RequestedSeq)+`,"verdict":"pass"}`); v.Code != 202 {
		t.Fatalf("verdict: %d %s", v.Code, v.Body.String())
	}
	blocked := awaitState(t, c, id, "blocked")
	if blocked.ResumePolicy != formations.ResumePolicyGrant || !blocked.ResumeAllowed {
		t.Fatalf("blocked = %+v", blocked.RunStatusProjection)
	}
	// The gate's ask may never go out when the verdict beats it; the block's does.
	body := ""
	for deadline := time.Now().Add(5 * time.Second); body == "" && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		sent, _ := notifier.snapshot()
		for _, notification := range sent {
			if notification.Kind == formations.NeedsYouKindBlocked {
				body = notification.Body
			}
		}
	}
	if !strings.Contains(body, "Reason: The mission used 1 of 1 round\n") {
		t.Fatalf("the block's message does not say what the run used:\n%s", body)
	}
	if !strings.Contains(body, "run resume "+id+" --grant") || !strings.Contains(body, "run abort "+id) {
		t.Fatalf("the block's message does not offer the grant:\n%s", body)
	}
	if w := post(t, c, "/api/runs/"+id+"/resume", `{"mode":"reattach","reason":"go on"}`); w.Code != 409 || !strings.Contains(w.Body.String(), "--grant") {
		t.Fatalf("resume without a grant: %d %s", w.Code, w.Body.String())
	}
	if w := post(t, c, "/api/runs/"+id+"/resume", `{"mode":"reattach","reason":"After deserves its round","grant":true}`); w.Code != 202 {
		t.Fatalf("resume with a grant: %d %s", w.Code, w.Body.String())
	}
	if node := <-e.entered; node != "fmn_after" {
		t.Fatalf("the grant ran %s, want After", node)
	}
	e.proceed <- struct{}{}
	awaitState(t, c, id, "succeeded")
	events, err := c.store.ReadRunEvents(id)
	if err != nil {
		t.Fatal(err)
	}
	granted := 0
	for _, event := range events {
		if event.Type != formations.RunEventResumed {
			continue
		}
		grant, _ := event.Data["grant"].(map[string]any)
		if grant["limitId"] != "lim_mission" || grant["kind"] != "rounds" || event.Actor == "" {
			t.Fatalf("resume evidence = %+v", event)
		}
		granted++
	}
	if granted != 1 {
		t.Fatalf("grants recorded = %d, want one", granted)
	}
}
