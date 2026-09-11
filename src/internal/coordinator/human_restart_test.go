package coordinator

import (
	"strconv"
	"testing"

	"github.com/Perttulands/chrote-agent-formations/internal/formations"
)

func TestPendingHumanGateSurvivesRestart(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		for _, verdict := range []string{"pass", "fail"} {
			t.Run(strconv.FormatBool(legacy)+"/"+verdict, func(t *testing.T) {
				c, executor, root := fixture(t)
				id := startRun(t, c)
				<-executor.entered
				executor.proceed <- struct{}{}
				before := awaitState(t, c, id, "waiting_human")
				seq := before.WaitingGates[0].RequestedSeq
				if legacy {
					if err := c.engine.BlockInterruptedRun(id); err != nil {
						t.Fatal(err)
					}
				}
				original, _ := c.store.ReadRunEvents(id)
				for i := 0; i < 2; i++ {
					if err := c.Close(); err != nil {
						t.Fatal(err)
					}
					var err error
					c, err = Open(root, c.personas, func(*formations.Store) formations.FormationExecutor { return executor })
					if err != nil {
						t.Fatal(err)
					}
					if err = c.RecoverInterruptedRuns(); err != nil {
						t.Fatal(err)
					}
					p := awaitState(t, c, id, "waiting_human")
					if p.WaitingGates[0].RequestedSeq != seq || p.Final || p.ResumeAllowed {
						t.Fatalf("request changed: %+v", p)
					}
				}
				defer c.Close()
				events, _ := c.store.ReadRunEvents(id)
				extra := 0
				if legacy {
					extra = 1
				}
				if len(events) != len(original)+extra {
					t.Fatalf("restart added unexpected events: %+v", events)
				}
				path := "/api/formations/runs/" + id + "/gates/gate_review/verdict"
				if w := post(t, c, path, `{"requestedSeq":999,"verdict":"pass"}`); w.Code != 409 {
					t.Fatal(w.Code)
				}
				body := `{"requestedSeq":` + strconv.Itoa(seq) + `,"verdict":"` + verdict + `"}`
				if w := post(t, c, path, body); w.Code != 202 {
					t.Fatalf("%d %s", w.Code, w.Body.String())
				}
				if verdict == "pass" {
					<-executor.entered
					executor.proceed <- struct{}{}
					awaitState(t, c, id, "succeeded")
				} else {
					awaitState(t, c, id, "blocked")
				}
				if w := post(t, c, path, body); w.Code != 409 {
					t.Fatal(w.Code)
				}
			})
		}
	}
}

func TestPendingHumanGateDoesNotRepairUnrelatedBlock(t *testing.T) {
	c, e, _ := fixture(t)
	id := startRun(t, c)
	<-e.entered
	e.proceed <- struct{}{}
	awaitState(t, c, id, "waiting_human")
	if err := c.store.AppendRunEvent(id, formations.RunEvent{Type: formations.RunEventBlocked, Data: map[string]any{"reason": "other failure", "resumeAllowed": true}}); err != nil {
		t.Fatal(err)
	}
	if err := c.RecoverInterruptedRuns(); err != nil {
		t.Fatal(err)
	}
	p := awaitState(t, c, id, "blocked")
	if !p.ResumeAllowed {
		t.Fatal(p)
	}
}
