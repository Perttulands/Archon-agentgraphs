package coordinator

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// Storage maintenance has its own cancellable lifetime. A final ledger event
// can precede the executor's last file write; only settled workers are eligible.
func (c *Coordinator) startStorageMaintenance() {
	ctx, cancel := context.WithCancel(context.Background())
	c.storageCancel = cancel
	c.storageWake = make(chan struct{}, 1)
	c.storageDone = make(chan struct{})
	go func() {
		defer close(c.storageDone)
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			if err := c.sweepStorage(ctx); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("run storage maintenance: %v", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			case <-c.storageWake:
			}
		}
	}()
}

func (c *Coordinator) sweepStorage(ctx context.Context) error {
	// Admission installs frozen input references, and commands reserve a worker,
	// under this mutex. Neither can appear between checking liveness and disposal.
	c.admissions.Lock()
	defer c.admissions.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	var busy []string
	for id, state := range c.runs {
		if state.busy {
			busy = append(busy, id)
		}
	}
	c.mu.Unlock()
	return c.store.SweepRunStorage(ctx, busy...)
}

func (c *Coordinator) wakeStorageMaintenance() {
	select {
	case c.storageWake <- struct{}{}:
	default:
	}
}

func (c *Coordinator) runStorage(w http.ResponseWriter, r *http.Request) {
	report, err := c.store.RunStorage(r.PathValue("runId"))
	if err != nil {
		storageFailure(w, err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"storage": report})
}

func (c *Coordinator) changeRunStorage(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Action string `json:"action"`
	}
	if !decode(w, r, &request) {
		return
	}
	if request.Action != "archive" && request.Action != "unarchive" && request.Action != "dispose" {
		reply(w, http.StatusBadRequest, map[string]string{"error": "action must be archive, unarchive or dispose"})
		return
	}
	c.admissions.Lock()
	defer c.admissions.Unlock()
	id := r.PathValue("runId")
	c.mu.Lock()
	state := c.runs[id]
	busy := state != nil && state.busy
	closed := c.closed
	c.mu.Unlock()
	if closed {
		reply(w, http.StatusServiceUnavailable, map[string]string{"error": "coordinator shutting down"})
		return
	}
	if busy {
		reply(w, http.StatusConflict, map[string]string{"error": "run storage is in use; wait for its command to settle"})
		return
	}
	var err error
	var report *formations.RunStorageReport
	switch request.Action {
	case "archive":
		report, err = c.store.SetRunArchive(id, true)
	case "unarchive":
		report, err = c.store.SetRunArchive(id, false)
	case "dispose":
		report, err = c.store.DisposeRunArtifacts(id)
	}
	if err != nil {
		storageFailure(w, err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"storage": report})
	c.wakeStorageMaintenance()
}

func storageFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, formations.ErrRunStorageActive), errors.Is(err, formations.ErrRunStorageReferenced), errors.Is(err, formations.ErrRunStorageExpired):
		reply(w, http.StatusConflict, map[string]string{"error": err.Error()})
	case errors.Is(err, formations.ErrNotFound), errors.Is(err, os.ErrNotExist):
		failure(w, err)
	default:
		reply(w, http.StatusInternalServerError, map[string]string{"error": "run storage unavailable: " + err.Error()})
	}
}
