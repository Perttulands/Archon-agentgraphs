package coordinator

import (
	"net/http"

	"github.com/Perttulands/chrote-agent-formations/internal/formations"
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
func (c *Coordinator) launch(id string, execute func() error) {
	c.mu.Lock()
	state := c.state(id)
	state.executing = true
	c.mu.Unlock()
	go func() {
		defer c.release(id)
		err := execute()
		c.mu.Lock()
		abort := state.abort
		state.settling = true // reject late cancellation commands
		c.mu.Unlock()
		if abort != nil {
			_ = c.store.AppendRunEvent(id, *abort)
		} else {
			c.recordFailure(id, err)
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
	if err := c.store.AppendRunEvent(id, event); err != nil {
		failure(w, err)
		return
	}
	c.get(w, r)
}

func (c *Coordinator) resume(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Actor  string `json:"actor"`
		Mode   string `json:"mode"`
		Reason string `json:"reason"`
	}
	if !decode(w, r, &req) {
		return
	}
	id := r.PathValue("runId")
	if !c.acquire(id) {
		reply(w, 409, map[string]string{"error": "coordinator is executing"})
		return
	}
	p, err := c.Project(id)
	if err != nil {
		c.release(id)
		failure(w, err)
		return
	}
	if p.Final || !p.ResumeAllowed || len(p.WaitingGates) > 0 {
		c.release(id)
		reply(w, 409, map[string]string{"error": "run is not resumable"})
		return
	}
	c.launch(id, func() error {
		_, err := c.engine.ResumeRun(id, formations.RunResumeRequest{Actor: req.Actor, Mode: req.Mode, Reason: req.Reason})
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
		if run.Status != formations.RunStatusBlocked {
			if err := c.engine.BlockInterruptedRun(run.RunID); err != nil {
				return err
			}
		}
		if err := c.engine.ValidateCompletedRecovery(run.RunID); err != nil {
			// The blocking event already names all unresolved dispatches. Preserve
			// the evidence rejection privately without changing resumability.
			continue
		}
		if err := c.ResumeCompletedRun(run.RunID); err != nil {
			return err
		}
	}
	return nil
}
