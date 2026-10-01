package coordinator

import (
	"log"
	"net/http"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

func (c *Coordinator) events(w http.ResponseWriter, r *http.Request) {
	p, err := c.Project(r.PathValue("runId"))
	if err != nil {
		failure(w, err)
		return
	}
	reply(w, 200, map[string]any{"events": p.Events})
}

func (c *Coordinator) escalations(w http.ResponseWriter, r *http.Request) {
	p, err := c.Project(r.PathValue("runId"))
	if err != nil {
		failure(w, err)
		return
	}
	// Only public event identities leave this runtime. Raw escalation payloads
	// can contain prompts and paths and are not part of its projection.
	events := []formations.OpenEscalation{}
	for _, event := range p.Events {
		if event.Type == formations.RunEventEscalationRaised && !p.Final {
			events = append(events, formations.OpenEscalation{RunID: p.RunID, Seq: event.Seq, NodeID: event.NodeID, GateID: event.GateID, Severity: "needs-attention", Reason: "Agent requested attention", Source: "coordinator", Trigger: event.Type, Blocks: event.Blocks})
		}
	}
	reply(w, 200, map[string]any{"escalations": events})
}

// launch retains the admission until the executor has returned, including its
// cleanup. A requested cancellation becomes final only after those events exist.
// A verdict recorded while the worker executes kicks it to continue once more
// before it settles, so the verdict is routed (archon-o7p.11).
func (c *Coordinator) launch(id string, execute func() error) {
	c.mu.Lock()
	state := c.state(id)
	state.executing = true
	state.kicked = false
	c.mu.Unlock()
	go func() {
		defer c.release(id)
		err := execute()
		for {
			c.mu.Lock()
			again := err == nil && state.kicked && state.abort == nil
			state.kicked = false
			abort := state.abort
			if !again {
				state.settling = true // reject late cancellation commands and kicks
			}
			c.mu.Unlock()
			if !again {
				if abort != nil {
					c.endKeptSeatsBeforeCancel(id)
					_ = c.store.AppendRunEvent(id, *abort)
				} else {
					c.recordFailure(id, err)
				}
				return
			}
			_, err = c.engine.ContinueRun(id)
		}
	}()
}

func (c *Coordinator) abort(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reason      string `json:"reason"`
		RequestedBy string `json:"requestedBy"`
	}
	if !decode(w, r, &req) {
		return
	}
	c.admissions.Lock()
	admitting := true
	defer func() {
		if admitting {
			c.admissions.Unlock()
		}
	}()
	id := r.PathValue("runId")
	p, err := c.Project(id)
	if err != nil {
		failure(w, err)
		return
	}
	if p.Final {
		failure(w, formations.ErrRunFinal)
		return
	}
	event := formations.RunEvent{Type: formations.RunEventCanceled, Actor: req.RequestedBy, Data: map[string]any{"reason": req.Reason, "final": true}}
	c.mu.Lock()
	state := c.state(id)
	if c.closed || state.busy && (!state.executing || state.settling) {
		c.mu.Unlock()
		reply(w, 409, map[string]string{"error": "coordinator is executing another command"})
		return
	}
	if state.busy {
		state.abort = &event
		done := state.done
		state.cancel()
		c.mu.Unlock()
		c.admissions.Unlock()
		admitting = false
		select {
		case <-done:
			c.get(w, r)
		case <-r.Context().Done():
		}
		return
	}
	// Reserve the same admission guard while finalizing an idle/waiting run.
	c.reserve(id)
	c.workers.Add(1)
	c.mu.Unlock()
	c.admissions.Unlock()
	admitting = false
	defer c.release(id)
	c.endKeptSeatsBeforeCancel(id)
	if err := c.store.AppendRunEvent(id, event); err != nil {
		failure(w, err)
		return
	}
	c.get(w, r)
}

// endKeptSeatsBeforeCancel ends a run's kept seats and records their cleanup
// before run_canceled, since the ledger accepts nothing after a final event.
func (c *Coordinator) endKeptSeatsBeforeCancel(runID string) {
	if err := c.engine.EndKeptSeats(runID); err != nil {
		log.Printf("run %s: kept seats before cancel: %v", runID, err)
	}
}

func (c *Coordinator) resume(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Actor  string `json:"actor"`
		Mode   string `json:"mode"`
		Reason string `json:"reason"`
		Grant  bool   `json:"grant"`
	}
	if !decode(w, r, &req) {
		return
	}
	id := r.PathValue("runId")
	if !c.acquireSoon(r.Context(), id) {
		reply(w, 409, map[string]string{"error": "coordinator is executing"})
		return
	}
	p, err := c.Project(id)
	if err != nil {
		c.release(id)
		failure(w, err)
		return
	}
	// Gates still waiting keep waiting through the resume (archon-o7p.11).
	if p.Final || !p.ResumeAllowed {
		c.release(id)
		reply(w, 409, map[string]string{"error": "run is not resumable"})
		return
	}
	// A spent limit resumes only with a grant, and only it takes one (archon-o7p.8).
	grantable := p.ResumePolicy == formations.ResumePolicyGrant
	if grantable != req.Grant {
		c.release(id)
		message := formations.ErrRunNothingToGrant.Error()
		if grantable {
			message = formations.ErrRunGrantRequired.Error()
		}
		reply(w, 409, map[string]string{"error": message})
		return
	}
	c.launch(id, func() error {
		_, err := c.engine.ResumeRun(id, formations.RunResumeRequest{Actor: req.Actor, Mode: req.Mode, Reason: req.Reason, Grant: req.Grant})
		return err
	})
	reply(w, 202, p)
}

// RecoverInterruptedRuns is called before listening, under the directory lock.
// It never adopts old seats. Only completed native evidence can continue a run.
func (c *Coordinator) RecoverInterruptedRuns() error {
	runs, err := c.store.ListRuns(formations.RunListFilter{})
	if err != nil {
		return err
	}
	for _, run := range runs {
		if run.Final || run.Status == formations.RunStatusBlocked && !run.ResumeAllowed {
			continue
		}
		waiting, err := c.engine.PreservePendingHumanGate(run.RunID)
		if err != nil {
			return err
		}
		if waiting {
			continue
		}
		if run.Status != formations.RunStatusBlocked {
			if err := c.engine.BlockInterruptedRun(run.RunID); err != nil {
				return err
			}
		}
		if err := c.engine.ValidateCompletedRecovery(run.RunID); err != nil {
			// The blocking event already names all unresolved dispatches. The
			// journal carries the evidence rejection; resumability is unchanged.
			log.Printf("run %s: not recovered at startup: %v", run.RunID, err)
			continue
		}
		if err := c.ResumeCompletedRun(run.RunID); err != nil {
			return err
		}
	}
	return nil
}
