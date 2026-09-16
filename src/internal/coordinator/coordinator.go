// Package coordinator owns the standalone trusted local runtime.
package coordinator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Perttulands/Archon-agentgraphs/internal/api"
	"github.com/Perttulands/Archon-agentgraphs/internal/core"
	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
	"github.com/Perttulands/Archon-agentgraphs/internal/terminal"
)

type GateRequest struct {
	GateID       string `json:"gateId"`
	RequestedSeq int    `json:"requestedSeq"`
}
type Event struct {
	Seq         int    `json:"seq"`
	Type        string `json:"type"`
	NodeID      string `json:"nodeId,omitempty"`
	SlotID      string `json:"slotId,omitempty"`
	GateID      string `json:"gateId,omitempty"`
	Status      string `json:"status,omitempty"`
	Verdict     string `json:"verdict,omitempty"`
	SessionName string `json:"sessionName,omitempty"`
	Outcome     string `json:"outcome,omitempty"`
	Blocks      bool   `json:"blocks,omitempty"`
}
type Projection struct {
	*formations.RunStatusProjection
	ProjectionVersion string        `json:"projectionVersion"`
	WaitingGates      []GateRequest `json:"waitingGates"`
	Events            []Event       `json:"events"`
}
type Coordinator struct {
	admissions       sync.Mutex
	personas         *formations.PersonaStore
	store            *formations.Store
	engine           *formations.RunEngine
	lock             *os.File
	mu               sync.Mutex
	closed           bool
	workers          sync.WaitGroup
	runs             map[string]*executionState
	executionBase    context.Context
	detach           context.CancelCauseFunc
	stopping         chan struct{}
	shutdownDone     chan struct{}
	shutdownExpired  chan struct{}
	shutdownOnce     sync.Once
	shutdownGrace    time.Duration
	shutdownTimeout  time.Duration
	closeErr         error
	terminalObserver *terminal.Observer
	needsYou         *needsYouDispatcher
	agentLiveness    api.AgentLivenessProvider
}

type executionState struct {
	executing bool
	busy      bool
	settling  bool
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	changed   chan struct{}
	abort     *formations.RunEvent
}

// Open takes one kernel lock for the lifetime of the coordinator. StateDir is
// private runtime storage and also the offline Archon definition workspace.
func Open(stateDir string, personas *formations.PersonaStore, makeExecutor func(*formations.Store) formations.FormationExecutor) (*Coordinator, error) {
	if !filepath.IsAbs(stateDir) {
		return nil, errors.New("state directory must be absolute")
	}
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(stateDir, "coordinator.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return nil, errors.New("another coordinator owns this state directory")
	}
	store := formations.NewStore(stateDir)
	base, detach := context.WithCancelCause(context.Background())
	c := &Coordinator{store: store, personas: personas, lock: lock, runs: make(map[string]*executionState),
		executionBase: base, detach: detach, stopping: make(chan struct{}), shutdownDone: make(chan struct{}), shutdownExpired: make(chan struct{}),
		shutdownGrace: 5 * time.Second, shutdownTimeout: 10 * time.Second}
	store.OnRunEvent = func(event formations.RunEvent) {
		c.mu.Lock()
		state := c.state(event.RunID)
		close(state.changed)
		state.changed = make(chan struct{})
		c.mu.Unlock()
	}
	c.engine = formations.NewRunEngine(store, personas, makeExecutor(store))
	c.engine.SetExecutionContext(func(runID string) context.Context {
		c.mu.Lock()
		defer c.mu.Unlock()
		state := c.state(runID)
		if state.ctx == nil {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			return ctx
		}
		return state.ctx
	})
	c.engine.SetGateEvaluator(formations.NewCodeGateEvaluator())
	return c, nil
}

// BeginShutdown fences admission and further dispatch immediately. Existing
// turns get five seconds to finish, then observation is canceled without abort.
func (c *Coordinator) BeginShutdown() {
	c.shutdownOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		close(c.stopping)
		c.mu.Unlock()
		deadline := time.AfterFunc(c.shutdownTimeout, func() { close(c.shutdownExpired) })
		done := make(chan struct{})
		go func() { c.workers.Wait(); close(done) }()
		go func() {
			grace := time.NewTimer(c.shutdownGrace)
			defer grace.Stop()
			select {
			case <-done:
			case <-grace.C:
			}
			c.detach(formations.ErrCoordinatorShutdown)
			<-done
			c.closeErr = c.lock.Close()
			deadline.Stop()
			close(c.shutdownDone)
		}()
	})
}

// Close is bounded and idempotent. If an executor fails to stop, the writer
// lock stays owned until its worker exits (or the process exits). A timed return
// never permits a second coordinator to overlap an old writer.
func (c *Coordinator) Close() error {
	c.BeginShutdown()
	select {
	case <-c.shutdownDone:
		return c.closeErr
	case <-c.shutdownExpired:
		return errors.New("coordinator shutdown exceeded ten-second budget; writer lock retained until workers exit")
	}
}
func (c *Coordinator) Store() *formations.Store { return c.store }

// ResumeCompletedRun is an explicit startup operation under the coordinator's
// writer lock. The executor must validate the selected completed-turn evidence.
// It is intentionally not a general HTTP resume or live-session recovery API.
func (c *Coordinator) ResumeCompletedRun(runID string) error {
	if !c.acquire(runID) {
		return errors.New("coordinator is executing or closed")
	}
	p, err := c.Project(runID)
	if err != nil {
		c.release(runID)
		return err
	}
	if p.Status != "blocked" || !p.ResumeAllowed || p.Final {
		c.release(runID)
		return errors.New("completed-turn recovery requires a resumable blocked run")
	}
	if err := c.engine.ValidateCompletedRecovery(runID); err != nil {
		c.release(runID)
		return err
	}
	c.launch(runID, func() error {
		_, err := c.engine.ResumeRun(runID, formations.RunResumeRequest{Actor: "operator:standalone", Mode: "completed-native-turn", Reason: "explicitly selected completed Codex turn"})
		return err
	})
	return nil
}

func Listen(address string) (net.Listener, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return nil, errors.New("listen address must use a literal IP")
	}
	return net.Listen("tcp", address)
}

func (c *Coordinator) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/formations/runs/{runId}/seats", c.seats)
	mux.HandleFunc("GET /api/formations/runs/{runId}/seats/{createdSeq}/terminal", c.viewTerminal)
	mux.HandleFunc("GET /api/formations/runs/{runId}/gates/{gateId}/request", c.pendingGateRequest)
	c.registerEvidenceRoutes(mux)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 200, map[string]string{"status": "ok", "runtime": "standalone-trusted-v1"})
	})
	// Route ownership: internal/api wins both board reads and all authoring routes.
	// Its injected runtime delegates POST runs to coordinator admission (limits,
	// durable 202), GET run to the closed projection, GET stream to live SSE,
	// and verdict to the exact-request coordinator guard. No duplicate patterns
	// or request-local engines are mounted. Resume and abort share this owner.
	h := api.NewFormationsHandlerWithStores(c.store, c.personas)
	h.SetRuntime(api.RuntimeHandlers{Start: c.start, Get: c.get, Events: c.events, Stream: c.stream, Resume: c.resume, Abort: c.abort, Verdict: c.verdict, Escalations: c.escalations})
	h.RegisterRoutes(mux)
	c.mu.Lock()
	liveness := c.agentLiveness
	c.mu.Unlock()
	api.NewAgentsHandlerWithStoreAndLiveness(c.personas, liveness).RegisterRoutes(mux)
	mux.HandleFunc("GET /api/formations/runs", func(w http.ResponseWriter, r *http.Request) {
		// An optional board filter lets a cockpit poll only its board's runs.
		runs, err := c.store.ListRuns(formations.RunListFilter{BoardSlug: r.URL.Query().Get("board")})
		if err != nil {
			failure(w, err)
			return
		}
		projections := make([]*Projection, 0, len(runs))
		for _, run := range runs {
			p, err := c.Project(run.RunID)
			if err != nil {
				failure(w, err)
				return
			}
			projections = append(projections, p)
		}
		reply(w, 200, projections)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			// Authoring handlers write definitions too. Retain writer ownership
			// through every admitted mutation, including HTTP body decoding.
			if !c.acquire("") {
				reply(w, http.StatusServiceUnavailable, map[string]string{"error": "coordinator shutting down"})
				return
			}
			defer c.release("")
		}
		mux.ServeHTTP(w, r)
	})
}

// state is accessed only with mu held. Signals outlive each execution so a
// subscriber can follow a waiting run through verdict and continuation.
func (c *Coordinator) state(id string) *executionState {
	state := c.runs[id]
	if state == nil {
		state = &executionState{changed: make(chan struct{})}
		c.runs[id] = state
	}
	return state
}
func (c *Coordinator) reserve(id string) {
	state := c.state(id)
	state.busy, state.settling, state.executing = true, false, false
	state.abort = nil
	state.ctx, state.cancel = context.WithCancel(formations.WithShutdownFence(c.executionBase, c.stopping))
	state.done = make(chan struct{})
}
func (c *Coordinator) acquire(id string) bool {
	if id != "" {
		c.admissions.Lock()
		defer c.admissions.Unlock()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || id != "" && c.state(id).busy {
		return false
	}
	if id != "" {
		c.reserve(id)
	}
	c.workers.Add(1)
	return true
}
func (c *Coordinator) release(id string) {
	c.mu.Lock()
	notify := c.needsYou
	if id != "" {
		state := c.state(id)
		state.cancel()
		state.busy = false
		close(state.done)
		close(state.changed)
		state.changed = make(chan struct{})
	}
	c.mu.Unlock()
	if id != "" && notify != nil {
		// The run's command has settled; only now may it announce a block.
		notify.settled(id)
	}
	c.workers.Done()
}
func (c *Coordinator) nextChange(id string) <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state(id).changed
}

func (c *Coordinator) start(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Cwd         string               `json:"cwd"`
		Brief       string               `json:"brief"`
		BeadID      string               `json:"beadId"`
		Actor       string               `json:"actor"`
		FormationID string               `json:"formationId"`
		Board       string               `json:"board"`
		MissionID   string               `json:"missionId"`
		ExpectedRev int                  `json:"expectedRev"`
		Limits      formations.RunLimits `json:"limits"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Board == "" || (req.MissionID == "") == (req.FormationID == "") || req.ExpectedRev <= 0 || req.Limits.MaxDispatch <= 0 || req.Limits.MaxAttempts <= 0 || req.Limits.WallClockSeconds <= 0 || req.Limits.Redact {
		reply(w, 400, map[string]string{"error": "board, missionId, expectedRev and positive limits required; redacted execution is not supported"})
		return
	}
	if req.MissionID != "" {
		info, err := os.Stat(req.Cwd)
		if !filepath.IsAbs(req.Cwd) || err != nil || !info.IsDir() || strings.TrimSpace(req.Brief) == "" || req.BeadID != "" && !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`).MatchString(req.BeadID) {
			reply(w, 400, map[string]string{"error": "absolute existing cwd, nonempty brief and safe beadId required"})
			return
		}
	}
	c.admissions.Lock()
	defer c.admissions.Unlock()
	if !c.acquire("") {
		reply(w, 409, map[string]string{"error": "coordinator is executing"})
		return
	}
	startedWorker := false
	defer func() {
		if !startedWorker {
			c.release("")
		}
	}()
	board, err := c.store.ReadBoard(req.Board)
	if err != nil {
		failure(w, err)
		return
	}
	// One report lists every problem on the revision the operator has seen.
	// The fail-fast checks below and in the engine remain as defence in depth.
	if match := r.Header.Get("If-Match"); req.ExpectedRev != board.Rev || match != "" && match != board.ETag {
		failure(w, formations.ErrConflict)
		return
	}
	if err := formations.CheckRunAdmission(board, c.personas, formations.RunAdmissionScope{MissionID: req.MissionID, FormationID: req.FormationID}); err != nil {
		var admission *formations.RunAdmissionError
		if errors.As(err, &admission) {
			api.WriteRunAdmissionError(w, admission)
			return
		}
		failure(w, err)
		return
	}
	// Admit the schedules supported by the shared seat executor.
	for _, node := range board.Formations {
		if node.Type != formations.FormationTypeSolo && node.Type != formations.FormationTypePeer && node.Type != formations.FormationTypeOrchestrated || len(node.Slots) == 0 {
			reply(w, 422, map[string]string{"error": "standalone runtime requires a supported formation with staffed slots"})
			return
		}
	}
	if req.FormationID != "" {
		started, execute, err := c.engine.PrepareFormationRun(req.Board, req.FormationID, formations.FormationRunRequest{Actor: req.Actor, Personas: c.personas, Limits: req.Limits, ExpectedBoardRev: req.ExpectedRev, ExpectedBoardETag: r.Header.Get("If-Match")})
		if err != nil {
			failure(w, err)
			return
		}
		c.mu.Lock()
		c.reserve(started.RunID)
		c.mu.Unlock()
		startedWorker = true
		c.launch(started.RunID, func() error { _, err := execute(); return err })
		reply(w, 202, map[string]string{"runId": started.RunID})
		return
	}
	connected := false
	for _, edge := range board.Connections {
		if strings.HasPrefix(edge.From, req.MissionID+":") {
			connected = true
		}
	}
	if !connected {
		reply(w, 422, map[string]string{"error": "wire the mission to a formation"})
		return
	}
	started, err := c.store.StartRun(req.Board, formations.RunStartRequest{Cwd: req.Cwd, Brief: req.Brief, BeadID: req.BeadID, MissionID: req.MissionID, ExpectedBoardRev: req.ExpectedRev, ExpectedBoardETag: r.Header.Get("If-Match"), Actor: "operator:standalone", Personas: c.personas, Limits: req.Limits})
	if err != nil {
		failure(w, err)
		return
	}
	c.mu.Lock()
	c.reserve(started.RunID)
	c.mu.Unlock()
	startedWorker = true
	c.launch(started.RunID, func() error { _, err := c.engine.ExecuteStartedMission(started.RunID); return err })
	reply(w, http.StatusAccepted, map[string]string{"runId": started.RunID})
}

func (c *Coordinator) recordFailure(runID string, err error) {
	if err == nil {
		return
	}
	// The operator's journal always sees the reason, even when the ledger
	// cannot take another event because the run is already blocked or final.
	log.Printf("run %s: %v", runID, err)
	status, readErr := c.store.ProjectRun(runID)
	if readErr == nil && !status.Final && status.Status != formations.RunStatusBlocked {
		// Private ledger keeps the diagnostic. Public projection exposes no raw errors.
		_ = c.store.AppendRunEvent(runID, formations.RunEvent{Type: formations.RunEventFailed, Data: map[string]any{"reason": "coordinator_execution_failed", "detail": err.Error(), "final": true}})
	}
}

// Project is the only service run read model. Raw prompts, captures, refs,
// paths, native session ids and arbitrary event data cannot enter this DTO.
func (c *Coordinator) Project(runID string) (*Projection, error) {
	events, err := c.store.ReadRunEvents(runID)
	if err != nil {
		return nil, err
	}
	status, err := formations.ProjectRunEvents(runID, events)
	if err != nil {
		return nil, err
	}
	return project(status, events), nil
}
func project(status *formations.RunStatusProjection, events []formations.RunEvent) *Projection {
	p := &Projection{RunStatusProjection: status, ProjectionVersion: "standalone-trusted-v1", WaitingGates: []GateRequest{}, Events: []Event{}}
	waiting := map[string]int{}
	for _, raw := range events {
		if raw.Type == formations.RunEventHumanInputRequested {
			waiting[raw.GateID] = raw.Seq
		}
		if raw.Type == formations.RunEventHumanVerdictRecorded {
			delete(waiting, raw.GateID)
		}
		e := Event{Seq: raw.Seq, Type: raw.Type, NodeID: raw.NodeID, SlotID: raw.SlotID, GateID: raw.GateID}
		e.Status, _ = raw.Data["status"].(string)
		e.Verdict, _ = raw.Data["verdict"].(string)
		e.SessionName, _ = raw.Data["sessionName"].(string)
		e.Outcome, _ = raw.Data["outcome"].(string)
		e.Blocks, _ = raw.Data["blocks"].(bool)
		p.Events = append(p.Events, e)
	}
	if !status.Final {
		for _, event := range events {
			if waiting[event.GateID] == event.Seq {
				p.WaitingGates = append(p.WaitingGates, GateRequest{event.GateID, event.Seq})
			}
		}
	}
	if len(p.WaitingGates) > 0 && status.Status != formations.RunStatusBlocked {
		p.Status = "waiting_human"
	}
	return p
}

func (c *Coordinator) get(w http.ResponseWriter, r *http.Request) {
	p, err := c.Project(r.PathValue("runId"))
	if err != nil {
		failure(w, err)
		return
	}
	reply(w, 200, p)
}
func (c *Coordinator) verdict(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Actor        string `json:"actor"`
		RequestedSeq int    `json:"requestedSeq"`
		Verdict      string `json:"verdict"`
		Reason       string `json:"reason"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.RequestedSeq <= 0 || req.Verdict != "pass" && req.Verdict != "fail" {
		reply(w, 400, map[string]string{"error": "requestedSeq and pass or fail verdict required"})
		return
	}
	runID, gateID := r.PathValue("runId"), r.PathValue("gateId")
	if !c.acquire(runID) {
		reply(w, 409, map[string]string{"error": "coordinator is executing"})
		return
	}
	startedWorker := false
	defer func() {
		if !startedWorker {
			c.release(runID)
		}
	}()
	p, err := c.Project(runID)
	if err != nil {
		failure(w, err)
		return
	}
	match := false
	for _, gate := range p.WaitingGates {
		if gate.GateID == gateID && gate.RequestedSeq == req.RequestedSeq {
			match = true
		}
	}
	if !match {
		reply(w, 409, map[string]string{"error": "human gate request is no longer pending"})
		return
	}
	status, err := c.engine.RecordHumanGateVerdict(runID, formations.HumanGateVerdictRequest{GateID: gateID, Verdict: req.Verdict, Reason: req.Reason, Actor: "human:operator"})
	if err != nil {
		failure(w, err)
		return
	}
	if status.ResumeAllowed {
		// Verdict and continuation are one operator action. Execution outlives HTTP.
		startedWorker = true
		c.launch(runID, func() error {
			_, err := c.engine.ResumeRun(runID, formations.RunResumeRequest{Actor: "coordinator", Mode: "reattach", Reason: "human verdict recorded"})
			return err
		})
	}
	reply(w, 202, map[string]string{"runId": runID})
}

func (c *Coordinator) stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		reply(w, 500, map[string]string{"error": "streaming unavailable"})
		return
	}
	since := 0
	if raw := r.Header.Get("Last-Event-ID"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			reply(w, 400, map[string]string{"error": "invalid Last-Event-ID"})
			return
		}
		since = n
	}
	sent := false
	for {
		changed := c.nextChange(r.PathValue("runId")) // subscribe before reading, so no append is missed
		p, err := c.Project(r.PathValue("runId"))
		if err != nil {
			if sent {
				fmt.Fprint(w, "event: error\ndata: {\"error\":\"run projection unavailable\"}\n\n")
				flusher.Flush()
			} else {
				failure(w, err)
			}
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		if p.EventCount > since {
			raw, _ := json.Marshal(core.NewSuccessResponse(p))
			fmt.Fprintf(w, "id: %d\nevent: projection\ndata: %s\n\n", p.EventCount, raw)
			flusher.Flush()
			since = p.EventCount
		}
		flusher.Flush()
		sent = true
		if p.Final {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-c.stopping:
			return
		case <-changed:
		}
	}
}
func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		reply(w, 400, map[string]string{"error": "invalid JSON request"})
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		reply(w, 400, map[string]string{"error": "expected one JSON object"})
		return false
	}
	return true
}
func failure(w http.ResponseWriter, err error) {
	code := 422
	if errors.Is(err, formations.ErrConflict) || errors.Is(err, formations.ErrRunFinal) {
		code = 409
	}
	if errors.Is(err, formations.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
		code = 404
	}
	reply(w, code, map[string]string{"error": http.StatusText(code)})
}
func reply(w http.ResponseWriter, code int, value any) {
	if code >= 400 {
		message := http.StatusText(code)
		if detail, ok := value.(map[string]string); ok && detail["error"] != "" {
			message = detail["error"]
		}
		core.WriteError(w, code, http.StatusText(code), message)
		return
	}
	core.WriteJSON(w, code, core.NewSuccessResponse(value))
}
