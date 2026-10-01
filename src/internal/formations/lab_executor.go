package formations

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const defaultLabOutputCapBytes = 8192

type RunExecutionError struct {
	Code       string
	Message    string
	Boundary   string
	Cause      error
	NodeID     string
	SlotID     string
	DispatchID string
}

func (e *RunExecutionError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *RunExecutionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

type LabExecutorConfig struct {
	Harnesses []string
	// Cwd is the working directory of the dispatch being executed: its run's
	// recorded cwd. It is set per dispatch and never configured (archon-12qt).
	Cwd            string
	OutputCapBytes int
}

type LabFormationExecutor struct {
	store    *Store
	personas *PersonaStore
	config   LabExecutorConfig
}

func NewConfiguredFormationExecutorFromEnv(store *Store, personas *PersonaStore, boundary string) FormationExecutor {
	labConfig := LabExecutorConfigFromEnv()
	if len(labConfig.Harnesses) != 0 {
		return NewLabFormationExecutor(store, personas, labConfig)
	}
	tmuxConfig := TmuxExecutorConfigFromEnv()
	if len(tmuxConfig.Harnesses) != 0 {
		return NewTmuxFormationExecutor(store, personas, tmuxConfig)
	}
	return NewUnavailableFormationExecutor(boundary)
}

func LabExecutorConfigFromEnv() LabExecutorConfig {
	capBytes := defaultLabOutputCapBytes
	if raw := strings.TrimSpace(os.Getenv("ARCHON_LAB_OUTPUT_CAP_BYTES")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			capBytes = parsed
		}
	}
	return LabExecutorConfig{
		Harnesses:      splitLabCSV(os.Getenv("ARCHON_LAB_HARNESSES")),
		OutputCapBytes: capBytes,
	}
}

func NewLabFormationExecutor(store *Store, personas *PersonaStore, config LabExecutorConfig) *LabFormationExecutor {
	if config.OutputCapBytes <= 0 {
		config.OutputCapBytes = defaultLabOutputCapBytes
	}
	return &LabFormationExecutor{store: store, personas: personas, config: config}
}

func (e *LabFormationExecutor) ExecuteFormation(req FormationExecution) (FormationExecutionResult, error) {
	return e.ExecuteFormationContext(context.Background(), req)
}

func (e *LabFormationExecutor) ExecuteFormationContext(ctx context.Context, req FormationExecution) (FormationExecutionResult, error) {
	copy := *e
	copy.config.Cwd = req.Cwd
	return copy.executeFormation(ctx, req)
}
func (e *LabFormationExecutor) executeFormation(ctx context.Context, req FormationExecution) (FormationExecutionResult, error) {
	if e == nil || e.store == nil {
		return FormationExecutionResult{}, runExecutionError("missing_executor", "lab executor store is not configured", "executor", ErrRunExecutorUnavailable)
	}
	if err := e.validateConfiguredBoundary(); err != nil {
		return FormationExecutionResult{}, err
	}
	if len(req.Formation.Slots) == 0 {
		return FormationExecutionResult{}, runExecutionError("missing_slot", fmt.Sprintf("formation %q has no slots to dispatch", req.NodeID), "executor", nil)
	}

	allowed := e.allowedHarnesses()
	dispatcher := NewSlotDispatcher(e.store, nil)
	outputs := make([]string, 0, len(req.Formation.Slots))
	for _, slot := range req.Formation.Slots {
		if !slot.Staffed() {
			return FormationExecutionResult{}, runExecutionError("missing_agent", fmt.Sprintf("slot %q is not staffed", slot.ID), "executor", nil)
		}
		card, variant, err := e.store.readRunPersonaBinding(req.RunID, req.NodeID, slot)
		if err != nil {
			return FormationExecutionResult{}, err
		}
		if !allowed[variant.ID] {
			return FormationExecutionResult{}, runExecutionError("unconfigured_harness", fmt.Sprintf("lab executor is not configured for harness %q", variant.ID), "executor", nil)
		}
		if variant.SessionStem == "" {
			return FormationExecutionResult{}, runExecutionError("missing_session", fmt.Sprintf("agent %q harness %q has no session stem", card.ID, variant.ID), "executor", nil)
		}

		prompt := e.renderPrompt(req, slot, *card, variant)
		if err := dispatchContextError(ctx); err != nil {
			return FormationExecutionResult{}, err
		}
		// Keep the brief a real seat would read, so lab runs show routed context.
		briefPath, err := writeBriefFile(e.store.Workspace, "lab-*.md", prompt)
		if err != nil {
			return FormationExecutionResult{}, runExecutionError("brief_write_failed", "lab executor could not write the brief", "executor", err)
		}
		lease, err := dispatcher.DispatchSlot(req.RunID, SlotDispatchRequest{
			BriefPath:   briefPath,
			NodeID:      req.NodeID,
			SlotID:      slot.ID,
			AgentID:     card.ID,
			Harness:     variant.ID,
			SessionStem: variant.SessionStem,
			SessionRef:  "lab:" + variant.SessionStem,
			Prompt:      prompt,
			Attempt:     req.Attempt,
		})
		if err != nil {
			return FormationExecutionResult{}, err
		}
		if err := dispatcher.CompleteFromCapture(req.RunID, lease.DispatchID, fmt.Sprintf("<<<ARCHON-DONE run-id=%s status=ok artifact=lab-%s.md>>>", req.RunID, slot.ID)); err != nil {
			return FormationExecutionResult{}, err
		}
		outputs = append(outputs, e.renderSlotOutput(req, slot, *card, variant))
	}

	text := collapseRepeatedLabVerdicts(strings.Join(outputs, "\n\n"))
	if len(text) > e.config.OutputCapBytes {
		text = text[:e.config.OutputCapBytes]
	}
	return FormationExecutionResult{
		Status:    "done",
		ReportRef: fmt.Sprintf("lab://%s/%s/report.md", req.RunID, req.NodeID),
		Text:      text,
		Outputs:   labOutputPayloads(req.Formation, text),
	}, nil
}

func (e *LabFormationExecutor) validateConfiguredBoundary() error {
	if strings.TrimSpace(e.config.Cwd) == "" {
		return runExecutionError("missing_cwd", "the run records no working directory; start a new run", "executor", nil)
	}
	cwd, err := filepath.Abs(e.config.Cwd)
	if err != nil {
		return runExecutionError("invalid_cwd", "the run's working directory is invalid", "executor", err)
	}
	if info, err := os.Stat(cwd); err != nil || !info.IsDir() {
		return runExecutionError("unavailable_cwd", fmt.Sprintf("the run's working directory %s is unavailable", cwd), "executor", err)
	}
	return nil
}

func (e *LabFormationExecutor) allowedHarnesses() map[string]bool {
	allowed := make(map[string]bool, len(e.config.Harnesses))
	for _, harness := range e.config.Harnesses {
		if harness != "" {
			allowed[harness] = true
		}
	}
	return allowed
}

func (e *LabFormationExecutor) renderPrompt(req FormationExecution, slot FormationSlot, card PersonaCard, variant HarnessVariant) string {
	var b strings.Builder
	b.WriteString("run: " + req.RunID + "\n")
	b.WriteString("node: " + req.NodeID + "\n")
	b.WriteString("slot: " + slot.ID + "\n")
	b.WriteString(roleLine(card, variant))
	b.WriteString("harness: " + variant.ID + "\n")
	b.WriteString("cwd: " + e.config.Cwd + "\n")
	renderBriefAndInputs(&b, req, card)
	return b.String()
}

func (e *LabFormationExecutor) renderSlotOutput(req FormationExecution, slot FormationSlot, card PersonaCard, variant HarnessVariant) string {
	inputs := make([]string, 0, len(req.Inputs))
	for _, input := range req.Inputs {
		if input.Text != "" {
			inputs = append(inputs, input.Text)
		}
	}
	inputText := strings.Join(inputs, "\n")
	if inputText == "" {
		inputText = req.Brief.Goal
	}
	return fmt.Sprintf("lab-fake output from %s using %s for %s\nslot: %s\ninput: %s", roleName(card), variant.ID, req.Title, slot.ID, inputText)
}

// collapseRepeatedLabVerdicts keeps one copy of a lab judge fixture. Lab seats
// echo their input, so a multi-seat formation repeats a archon-verdict block
// from the run brief once per seat, and a formation gate downstream would reject
// the duplicates. Identical blocks collapse to the first; differing blocks stay,
// so a real conflict still fails at the judge.
func collapseRepeatedLabVerdicts(text string) string {
	lines := strings.Split(text, "\n")
	type block struct{ start, end int }
	var blocks []block
	for i := 0; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "```archon-verdict" {
			continue
		}
		for j := i + 1; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == "```" {
				blocks = append(blocks, block{i, j})
				i = j
				break
			}
		}
	}
	if len(blocks) < 2 {
		return text
	}
	body := func(b block) string {
		parts := make([]string, 0, b.end-b.start-1)
		for _, line := range lines[b.start+1 : b.end] {
			parts = append(parts, strings.TrimSpace(line))
		}
		return strings.Join(parts, "\n")
	}
	first := body(blocks[0])
	for _, b := range blocks[1:] {
		if body(b) != first {
			return text
		}
	}
	kept := make([]string, 0, len(lines))
	next := 1
	for i := 0; i < len(lines); i++ {
		if next < len(blocks) && i == blocks[next].start {
			i = blocks[next].end
			next++
			continue
		}
		kept = append(kept, lines[i])
	}
	return strings.Join(kept, "\n")
}

func labOutputPayloads(formation FormationNode, text string) map[string]FormationOutputPayload {
	outputs := make(map[string]FormationOutputPayload, len(formation.Outputs))
	multiOutput := len(formation.Outputs) > 1
	for _, port := range formation.Outputs {
		payloadText := text
		if multiOutput {
			payloadText = fmt.Sprintf("%s\noutput-port: %s", text, port.ID)
		}
		outputs[port.ID] = FormationOutputPayload{
			Text: payloadText,
		}
	}
	return outputs
}

func runExecutionError(code, message, boundary string, cause error) error {
	return &RunExecutionError{
		Code:     code,
		Message:  redactLedgerText(message),
		Boundary: boundary,
		Cause:    cause,
	}
}

func splitLabCSV(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			values = append(values, part)
		}
	}
	return values
}
