package formations

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrDispatchDeadPane = errors.New("archon dispatch dead pane")
	ErrDispatchTimeout  = errors.New("archon dispatch timeout")
)

type DispatchAdapter interface {
	SendSlotDispatch(SlotDispatchPayload) error
}

type SlotDispatcher struct {
	store   *Store
	adapter DispatchAdapter
}

type SlotDispatchRequest struct {
	BriefPath   string
	NodeID      string
	SlotID      string
	AgentID     string
	Harness     string
	SessionStem string
	SessionRef  string
	Prompt      string
	Phase       string
	Attempt     int
}

type SlotDispatchPayload struct {
	RunID      string
	DispatchID string
	NodeID     string
	SlotID     string
	SessionRef string
	Prompt     string
	Phase      string
}

type SlotDispatchLease struct {
	RunID      string
	DispatchID string
	NodeID     string
	SlotID     string
}

type CompletionSentinel struct {
	RunID    string
	Status   string
	Artifact string
}

func NewSlotDispatcher(store *Store, adapter DispatchAdapter) *SlotDispatcher {
	return &SlotDispatcher{store: store, adapter: adapter}
}

func (d *SlotDispatcher) DispatchSlot(runID string, req SlotDispatchRequest) (SlotDispatchLease, error) {
	if d == nil || d.store == nil {
		return SlotDispatchLease{}, fmt.Errorf("%w: dispatch store required", ErrNotFound)
	}
	dispatchID := newDispatchID(runID, req.NodeID, req.SlotID, req.Attempt)
	lease := SlotDispatchLease{
		RunID:      runID,
		DispatchID: dispatchID,
		NodeID:     req.NodeID,
		SlotID:     req.SlotID,
	}
	if err := d.store.AppendRunEvent(runID, RunEvent{
		Type:    RunEventSlotDispatch,
		NodeID:  req.NodeID,
		SlotID:  req.SlotID,
		Attempt: req.Attempt,
		Data: map[string]any{
			"dispatchId":         dispatchID,
			"nodeId":             req.NodeID,
			"slotId":             req.SlotID,
			"agentId":            req.AgentID,
			"harness":            req.Harness,
			"sessionStem":        req.SessionStem,
			"sessionRef":         req.SessionRef,
			"phase":              req.Phase,
			"promptSha256":       etag([]byte(req.Prompt)),
			"promptRef":          "",
			"briefPath":          req.BriefPath,
			"nativeAck":          false,
			"recordedBeforeSend": true,
		},
	}); err != nil {
		return lease, err
	}
	payload := SlotDispatchPayload{
		RunID:      runID,
		DispatchID: dispatchID,
		NodeID:     req.NodeID,
		SlotID:     req.SlotID,
		SessionRef: req.SessionRef,
		Prompt:     req.Prompt,
		Phase:      req.Phase,
	}
	if d.adapter == nil {
		return lease, nil
	}
	if err := d.adapter.SendSlotDispatch(payload); err != nil {
		// The run's worker records the failure and blocks the run once its
		// other work settles (runStop).
		return lease, runSlotExecutionError(dispatchErrorCode(err), redactPromptFromLedgerText(err.Error(), req.Prompt), "tmux", err, req.NodeID, req.SlotID, dispatchID)
	}
	return lease, nil
}

func (d *SlotDispatcher) CompleteFromCapture(runID, dispatchID, captured string) error {
	if d == nil || d.store == nil {
		return fmt.Errorf("%w: dispatch store required", ErrNotFound)
	}
	// A failure is returned for the run's worker to record (runStop).
	dispatch := d.dispatchEvent(runID, dispatchID)
	sentinel, ok := ParseCompletionSentinel(captured, runID)
	if !ok {
		return runSlotExecutionError("completion_sentinel_timeout", "completion sentinel timeout", "adapter", ErrDispatchTimeout, dispatch.NodeID, dispatch.SlotID, dispatchID)
	}
	if dispatch.Type == "" {
		message := fmt.Sprintf("unknown dispatch %q", dispatchID)
		return runSlotExecutionError("unknown_dispatch", message, "adapter", errors.New(message), "", "", dispatchID)
	}
	return d.store.AppendRunEvent(runID, RunEvent{
		Type:    RunEventSlotResult,
		NodeID:  dispatch.NodeID,
		SlotID:  dispatch.SlotID,
		Attempt: dispatch.Attempt,
		Data: map[string]any{
			"dispatchId": dispatchID,
			"nodeId":     dispatch.NodeID,
			"slotId":     dispatch.SlotID,
			"status":     redactLedgerText(sentinel.Status),
			"sentinel": map[string]any{
				"runId":    sentinel.RunID,
				"status":   redactLedgerText(sentinel.Status),
				"artifact": redactLedgerText(sentinel.Artifact),
			},
		},
	})
}

func (d *SlotDispatcher) dispatchEvent(runID, dispatchID string) RunEvent {
	if d == nil || d.store == nil || dispatchID == "" {
		return RunEvent{}
	}
	events, err := d.store.ReadRunEvents(runID)
	if err != nil {
		return RunEvent{}
	}
	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		if event.Type != RunEventSlotDispatch || event.Data == nil {
			continue
		}
		if stringFromAny(event.Data["dispatchId"]) == dispatchID {
			return event
		}
	}
	return RunEvent{}
}

func ParseCompletionSentinel(captured, runID string) (CompletionSentinel, bool) {
	sentinels := completionSentinels(captured, runID)
	if len(sentinels) == 0 {
		return CompletionSentinel{}, false
	}
	return sentinels[len(sentinels)-1], true
}

func countCompletionSentinels(captured, runID string) int {
	return len(completionSentinels(captured, runID))
}

func completionSentinels(captured, runID string) []CompletionSentinel {
	remaining := captured
	var sentinels []CompletionSentinel
	for {
		start := strings.Index(remaining, "<<<ARCHON-DONE ")
		if start == -1 {
			return sentinels
		}
		remaining = remaining[start+len("<<<ARCHON-DONE "):]
		end := strings.Index(remaining, ">>>")
		if end == -1 {
			return sentinels
		}
		fields := parseSentinelFields(remaining[:end])
		remaining = remaining[end+len(">>>"):]
		if fields["run-id"] != runID {
			continue
		}
		sentinels = append(sentinels, CompletionSentinel{
			RunID:    fields["run-id"],
			Status:   fields["status"],
			Artifact: fields["artifact"],
		})
	}
}

func parseSentinelFields(raw string) map[string]string {
	fields := map[string]string{}
	for _, part := range strings.Fields(raw) {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		fields[key] = strings.Trim(value, `"`)
	}
	return fields
}

func dispatchErrorCode(err error) string {
	if errors.Is(err, ErrDispatchDeadPane) {
		return "dead_pane"
	}
	return "dispatch_failed"
}

func newDispatchID(runID, nodeID, slotID string, attempt int) string {
	return fmt.Sprintf("dsp_%s_%s_%s_%d_%s", runID, nodeID, slotID, attempt, randomCrockford(8))
}
