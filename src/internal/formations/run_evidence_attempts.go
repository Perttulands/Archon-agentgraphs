package formations

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"syscall"
)

// Run attempts evidence (archon-5tg1) lists, for each step of a run, every
// attempt in order: when it ran, what it output, the gate verdicts that judged
// that output, and the files in its own artifact directory. File bytes come
// through the artifact raw route by each file's artifact name, so a view can
// show a step's attempts side by side with their images inline.

// evidenceKindHeadBytes is how much of a file is read to classify it.
const evidenceKindHeadBytes = 512

// Attempt statuses.
const (
	AttemptRunning = "running"
	AttemptDone    = "done"
	AttemptStopped = "stopped"
)

type RunAttemptsEvidence struct {
	RunID string         `json:"runId"`
	Steps []StepAttempts `json:"steps"`
}

// StepAttempts is one step and its attempts, oldest first.
type StepAttempts struct {
	NodeID string `json:"nodeId"`
	Title  string `json:"title"`
	// JudgeOf names the gates this step judges for, when it is a judge.
	JudgeOf  []string        `json:"judgeOf,omitempty"`
	Outputs  []FormationPort `json:"outputs"`
	Attempts []StepAttempt   `json:"attempts"`
}

type StepAttempt struct {
	Attempt    int    `json:"attempt"`
	Status     string `json:"status"`
	StartedSeq int    `json:"startedSeq"`
	StartedAt  string `json:"startedAt,omitempty"`
	EndedAt    string `json:"endedAt,omitempty"`
	OutputSeq  int    `json:"outputSeq,omitempty"`
	// Directory is the attempt's artifact directory, when it exists.
	Directory      *AttemptDirectory `json:"directory,omitempty"`
	Outputs        []AttemptOutput   `json:"outputs"`
	Verdicts       []AttemptVerdict  `json:"verdicts"`
	Files          []AttemptFile     `json:"files"`
	FilesOmitted   int               `json:"filesOmitted,omitempty"`
	FilesTruncated bool              `json:"filesTruncated,omitempty"`
	// Picked marks the attempt a spent limit picked as the step's best
	// (archon-gihe).
	Picked *AttemptPick `json:"picked,omitempty"`
}

// AttemptPick is a pick that took this attempt: the gate whose pass route it
// went on through, the pick's verdict seq, why and who picked, the attempts it
// chose from, and what started it (whenSpent or resume).
type AttemptPick struct {
	GateID     string       `json:"gateId"`
	GateTitle  string       `json:"gateTitle,omitempty"`
	Seq        int          `json:"seq"`
	Reason     EvidenceText `json:"reason"`
	DecidedBy  string       `json:"decidedBy"`
	Candidates []int        `json:"candidates"`
	Trigger    string       `json:"trigger"`
}

// AttemptDirectory names an attempt's artifact directory: Name relative to the
// run's artifact directory, Path absolute.
type AttemptDirectory struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// AttemptOutput is one output port's accepted payload. Path is the file or
// folder the output is (its snapshot), or a text output's ref; Artifact is
// Path's name under the run's artifact directory, for the artifact routes.
type AttemptOutput struct {
	PortID   string       `json:"portId"`
	Label    string       `json:"label,omitempty"`
	Kind     string       `json:"kind"`
	Text     EvidenceText `json:"text"`
	Path     string       `json:"path,omitempty"`
	Artifact string       `json:"artifact,omitempty"`
	Size     int64        `json:"size,omitempty"`
	// FileKind classifies Path as the artifact preview does (image, pdf,
	// markdown, json, text, binary), or folder.
	FileKind string `json:"fileKind,omitempty"`
}

// AttemptVerdict is one gate evaluation of the attempt's output and the
// verdict it reached, or Pending while it waits. Superseded marks a human
// request a later evaluation of the gate replaced before anyone answered it;
// it never gets a verdict.
type AttemptVerdict struct {
	GateID          string         `json:"gateId"`
	GateTitle       string         `json:"gateTitle,omitempty"`
	EvaluationSeq   int            `json:"evaluationSeq"`
	Seq             int            `json:"seq,omitempty"`
	Verdict         string         `json:"verdict,omitempty"`
	Pending         bool           `json:"pending,omitempty"`
	Superseded      bool           `json:"superseded,omitempty"`
	Reason          EvidenceText   `json:"reason"`
	Evidence        []EvidenceItem `json:"evidence"`
	EvidenceOmitted int            `json:"evidenceOmitted,omitempty"`
	DecidedBy       string         `json:"decidedBy,omitempty"`
}

// AttemptFile is one regular file in an attempt's artifact directory. Name is
// relative to the run's artifact directory, as the artifact routes take it.
type AttemptFile struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Size       int64  `json:"size"`
	ModifiedAt string `json:"modifiedAt"`
	Kind       string `json:"kind"`
}

// ProjectRunAttempts reads every attempt of every step of a run, or of the
// one step nodeID names. An unknown run or step is ErrNotFound.
func (s *Store) ProjectRunAttempts(runID, nodeID string) (*RunAttemptsEvidence, error) {
	events, err := s.ReadRunEvents(runID)
	if err != nil {
		return nil, evidenceNotFound(err)
	}
	board, err := s.ReadRunBoard(runID)
	if err != nil {
		return nil, err
	}
	if nodeID != "" {
		if _, ok := findFormation(board.Formations, nodeID); !ok {
			return nil, fmt.Errorf("%w: step %q", ErrNotFound, nodeID)
		}
	}
	status, err := ProjectRunEvents(runID, events)
	if err != nil {
		return nil, err
	}
	workspace, err := s.workspaceAbsolutePath()
	if err != nil {
		return nil, err
	}
	artifactsDir := filepath.Join(workspace, ".archon", "artifacts", runID)
	evidence := projectRunAttempts(runID, nodeID, board, events, status, s.runArtifactRoots(runID))
	for i := range evidence.Steps {
		step := &evidence.Steps[i]
		for j := range step.Attempts {
			attempt := &step.Attempts[j]
			name := step.NodeID + "/" + strconv.Itoa(attempt.Attempt)
			dir := filepath.Join(artifactsDir, step.NodeID, strconv.Itoa(attempt.Attempt))
			if info, err := os.Stat(dir); err != nil || !info.IsDir() {
				continue
			}
			attempt.Directory = &AttemptDirectory{Name: name, Path: dir}
			var cited []string
			for _, verdict := range attempt.Verdicts {
				for _, item := range verdict.Evidence {
					cited = append(cited, item.Text.Text)
				}
			}
			if attempt.Files, attempt.FilesTruncated, attempt.FilesOmitted, err = s.listAttemptFiles(runID, artifactsDir, step.NodeID, attempt.Attempt, attempt.Outputs, cited...); err != nil {
				return nil, err
			}
		}
	}
	return evidence, nil
}

// verdictAt locates one evaluation's verdict: the step, its attempt number
// and the verdict's index in that attempt.
type verdictAt struct {
	nodeID  string
	attempt int
	index   int
}

// openEvaluation is a gate's latest evaluation still waiting on its verdict;
// asked marks one that asked a human.
type openEvaluation struct {
	at    verdictAt
	asked bool
}

// projectRunAttempts reads the attempts from the ledger. Each node_output
// names its attempt, and each gate evaluation's input names the output it
// judges (RunInputRef.Origin), whichever route brought it there, so a verdict
// joins the attempt it judged. A judge's gates come from the run's board.
func projectRunAttempts(runID, onlyNode string, board *BoardDocument, events []RunEvent, status *RunStatusProjection, artifactRoots []string) *RunAttemptsEvidence {
	evidence := &RunAttemptsEvidence{RunID: runID, Steps: []StepAttempts{}}
	// Each text is capped here; the response's budget is spent newest
	// attempt first once every attempt is read (spendAttemptsBudget).
	capper := &evidenceCapper{remaining: math.MaxInt}
	judges := judgedGates(board)
	stepIndex := map[string]int{}
	// picking marks a step a pick settled; a resume reattaches none of its
	// attempts.
	picking := map[string]bool{}
	current := func(nodeID string) *StepAttempt {
		index, ok := stepIndex[nodeID]
		if !ok || len(evidence.Steps[index].Attempts) == 0 {
			return nil
		}
		attempts := evidence.Steps[index].Attempts
		return &attempts[len(attempts)-1]
	}
	find := func(nodeID string, number int) *StepAttempt {
		index, ok := stepIndex[nodeID]
		if !ok {
			return nil
		}
		attempts := evidence.Steps[index].Attempts
		for i := len(attempts) - 1; i >= 0; i-- {
			if attempts[i].Attempt == number {
				return &attempts[i]
			}
		}
		return nil
	}
	verdict := func(at verdictAt) *AttemptVerdict {
		if attempt := find(at.nodeID, at.attempt); attempt != nil && at.index < len(attempt.Verdicts) {
			return &attempt.Verdicts[at.index]
		}
		return nil
	}
	open := map[string]openEvaluation{}
	asked := map[int]verdictAt{}
	for index, event := range events {
		switch event.Type {
		case RunEventNodeStarted:
			formation, ok := findFormation(board.Formations, event.NodeID)
			if !ok || onlyNode != "" && event.NodeID != onlyNode {
				continue
			}
			if previous := current(event.NodeID); previous != nil && previous.Status == AttemptRunning {
				previous.Status = AttemptStopped
				if previous.EndedAt == "" {
					previous.EndedAt = event.Timestamp
				}
			}
			at, ok := stepIndex[event.NodeID]
			if !ok {
				at = len(evidence.Steps)
				stepIndex[event.NodeID] = at
				outputs := make([]FormationPort, 0, len(formation.Outputs))
				for _, port := range formation.Outputs {
					port.Label = capper.text(port.Label).Text
					port.Kind = PortKind(port)
					outputs = append(outputs, port)
				}
				evidence.Steps = append(evidence.Steps, StepAttempts{NodeID: formation.ID, Title: capper.text(formation.Title).Text, JudgeOf: judges[formation.ID], Outputs: outputs, Attempts: []StepAttempt{}})
			}
			step := &evidence.Steps[at]
			delete(picking, event.NodeID)
			// A restart before dispatch records the same attempt again.
			if last := current(event.NodeID); last != nil && last.Attempt == max(event.Attempt, 1) && last.OutputSeq == 0 {
				last.Status = AttemptRunning
				last.EndedAt = ""
				continue
			}
			step.Attempts = append(step.Attempts, StepAttempt{
				Attempt: max(event.Attempt, 1), Status: AttemptRunning, StartedSeq: event.Seq, StartedAt: event.Timestamp,
				Outputs: []AttemptOutput{}, Verdicts: []AttemptVerdict{}, Files: []AttemptFile{},
			})
		case RunEventNodeOutput:
			attempt := find(event.NodeID, event.Attempt)
			if attempt == nil {
				continue
			}
			step := &evidence.Steps[stepIndex[event.NodeID]]
			attempt.Status, attempt.OutputSeq, attempt.EndedAt = AttemptDone, event.Seq, event.Timestamp
			attempt.Outputs = attemptOutputs(step.Outputs, outputPayloadsFromAny(event.Data["outputs"]), capper, artifactRoots)
		case RunEventGateEvaluating:
			gateID := eventGateID(event)
			// A new evaluation replaces a request still waiting: the gate
			// takes a verdict only on its latest request.
			if waiting, ok := open[gateID]; ok {
				if replaced := verdict(waiting.at); replaced != nil && waiting.asked {
					replaced.Pending, replaced.Superseded = false, true
				}
				delete(open, gateID)
			}
			origin := runInputRefFromAny(event.Data["inputRef"]).Origin
			if origin == nil {
				continue
			}
			attempt := find(origin.NodeID, origin.Attempt)
			if attempt == nil {
				continue
			}
			judged := AttemptVerdict{GateID: gateID, EvaluationSeq: event.Seq, Pending: !status.Final, Reason: capper.text(""), Evidence: []EvidenceItem{}}
			if gate, ok := findGate(board.Gates, gateID); ok {
				judged.GateTitle = capper.text(gate.Title).Text
			}
			attempt.Verdicts = append(attempt.Verdicts, judged)
			open[gateID] = openEvaluation{at: verdictAt{nodeID: origin.NodeID, attempt: origin.Attempt, index: len(attempt.Verdicts) - 1}}
		case RunEventHumanInputRequested:
			if waiting, ok := open[eventGateID(event)]; ok {
				waiting.asked = true
				open[eventGateID(event)] = waiting
				asked[event.Seq] = waiting.at
			}
		case RunEventHumanVerdictRecorded:
			at, ok := asked[intFromRunEventData(event.Data["requestedSeq"])]
			if !ok {
				continue
			}
			if decided := verdict(at); decided != nil {
				decided.Verdict, decided.Pending = normalizeGateVerdict(stringFromEventData(event, "verdict")), false
				decided.Reason = capper.text(stringFromEventData(event, "reason"))
				decided.DecidedBy = stringFromEventData(event, "decidedBy")
			}
			if waiting, ok := open[eventGateID(event)]; ok && waiting.at == at {
				delete(open, eventGateID(event))
			}
		case RunEventPickStarted:
			// The step runs no more: an attempt its limit cut short stopped.
			picking[event.NodeID] = true
			if attempt := current(event.NodeID); attempt != nil && attempt.Status == AttemptRunning {
				attempt.Status, attempt.EndedAt = AttemptStopped, event.Timestamp
			}
		case RunEventGateVerdict:
			if pick := pickData(event); pick != nil {
				markPickedAttempt(find, board, event, pick, capper)
				continue
			}
			gateID := eventGateID(event)
			waiting, ok := open[gateID]
			at := waiting.at
			if requestedSeq := intFromRunEventData(event.Data["requestedSeq"]); requestedSeq > 0 {
				at, ok = asked[requestedSeq]
			}
			if !ok {
				continue
			}
			if waiting.at == at {
				delete(open, gateID)
			}
			decided := verdict(at)
			if decided == nil {
				continue
			}
			decided.Seq, decided.Verdict, decided.Pending, decided.Superseded = event.Seq, stringFromEventData(event, "verdict"), false, false
			decided.Reason = capper.text(stringFromEventData(event, "reason"))
			decided.Evidence, decided.EvidenceOmitted = evidenceItems(event.Data["evidence"], capper)
			if decidedBy := stringFromEventData(event, "decidedBy"); decidedBy != "" {
				decided.DecidedBy = decidedBy
			}
		case RunEventBlocked, RunEventError, RunEventFailed, RunEventCanceled:
			for _, nodeID := range runProblemNodes(events, index) {
				if attempt := current(nodeID); attempt != nil && attempt.Status == AttemptRunning && event.Type != RunEventError {
					attempt.Status, attempt.EndedAt = AttemptStopped, event.Timestamp
				}
			}
		case RunEventResumed:
			// A resume carries on only the attempts it reattaches, or whose
			// pending outputs it keeps; a redispatch runs them again instead.
			// A step a pick settled runs no more.
			if stringFromEventData(event, "resumeMode") == "redispatch" {
				continue
			}
			var resumed []*StepAttempt
			for _, ref := range enrichOpenDispatchRefs(events[:index], openDispatchRefsFromEvent(event)) {
				if !picking[ref.NodeID] {
					resumed = append(resumed, current(ref.NodeID))
				}
			}
			if pending := pendingOutputFromEvent(event); pending != nil && !picking[pending.NodeID] {
				resumed = append(resumed, find(pending.NodeID, pending.Attempt))
			}
			for _, attempt := range resumed {
				if attempt != nil && attempt.Status == AttemptStopped && attempt.OutputSeq == 0 {
					attempt.Status, attempt.EndedAt = AttemptRunning, ""
				}
			}
		}
	}
	if status.Final {
		for i := range evidence.Steps {
			for j := range evidence.Steps[i].Attempts {
				if attempt := &evidence.Steps[i].Attempts[j]; attempt.Status == AttemptRunning {
					attempt.Status, attempt.EndedAt = AttemptStopped, status.UpdatedAt
				}
			}
		}
	}
	spendAttemptsBudget(evidence)
	return evidence
}

// spendAttemptsBudget spends the response's text budget on the step titles
// and port labels first, then attempt by attempt, newest first, so a long run
// keeps its latest attempts whole and its oldest give way.
func spendAttemptsBudget(evidence *RunAttemptsEvidence) {
	remaining := EvidenceNodeBudgetBytes
	spendString := func(text *string) {
		*text, _ = CapEvidenceText(*text, remaining)
		remaining -= len(*text)
	}
	spend := func(text *EvidenceText) {
		capped, truncated := CapEvidenceText(text.Text, remaining)
		text.Text, text.Truncated = capped, text.Truncated || truncated
		remaining -= len(capped)
	}
	var newestFirst []*StepAttempt
	for i := range evidence.Steps {
		step := &evidence.Steps[i]
		spendString(&step.Title)
		for j := range step.Outputs {
			spendString(&step.Outputs[j].Label)
		}
		for j := range step.Attempts {
			newestFirst = append(newestFirst, &step.Attempts[j])
		}
	}
	sort.SliceStable(newestFirst, func(i, j int) bool { return newestFirst[i].StartedSeq > newestFirst[j].StartedSeq })
	for _, attempt := range newestFirst {
		for i := range attempt.Outputs {
			spend(&attempt.Outputs[i].Text)
		}
		for i := range attempt.Verdicts {
			verdict := &attempt.Verdicts[i]
			spendString(&verdict.GateTitle)
			spend(&verdict.Reason)
			for j := range verdict.Evidence {
				spend(&verdict.Evidence[j].Text)
			}
		}
		if pick := attempt.Picked; pick != nil {
			spendString(&pick.GateTitle)
			spend(&pick.Reason)
		}
	}
}

// markPickedAttempt marks the attempt a pick's verdict took (archon-gihe).
func markPickedAttempt(find func(nodeID string, number int) *StepAttempt, board *BoardDocument, event RunEvent, pick map[string]any, capper *evidenceCapper) {
	attempt := find(stringFromAny(pick["nodeId"]), intFromRunEventData(pick["attempt"]))
	if attempt == nil || attempt.OutputSeq == 0 {
		return
	}
	picked := &AttemptPick{
		GateID: event.GateID, Seq: event.Seq, Reason: capper.text(stringFromEventData(event, "reason")),
		DecidedBy: stringFromAny(pick["decidedBy"]), Candidates: intsFromAny(pick["candidates"]), Trigger: stringFromAny(pick["trigger"]),
	}
	attempt.Picked = picked
	if gate, ok := findGate(board.Gates, event.GateID); ok {
		picked.GateTitle = capper.text(gate.Title).Text
	}
}

// attemptOutputs lists an output's payloads in the step's port order, with
// each file or folder classified for previews.
func attemptOutputs(ports []FormationPort, payloads map[string]FormationOutputPayload, capper *evidenceCapper, artifactRoots []string) []AttemptOutput {
	outputs := []AttemptOutput{}
	seen := map[string]bool{}
	add := func(port FormationPort, payload FormationOutputPayload) {
		seen[port.ID] = true
		kind := payload.Kind
		if kind == "" {
			kind = PortKindText
		}
		output := AttemptOutput{PortID: port.ID, Label: port.Label, Kind: kind, Text: capper.text(payload.Text)}
		if ref := evidenceRef(payload.Ref, artifactRoots); ref != nil {
			output.Path = filepath.Clean(payload.Ref)
			output.Artifact = ref.Artifact
			output.Size, output.FileKind = attemptPathKind(output.Path)
		}
		outputs = append(outputs, output)
	}
	for _, port := range ports {
		if payload, ok := payloads[port.ID]; ok {
			add(port, payload)
		}
	}
	for portID, payload := range payloads {
		if !seen[portID] {
			add(FormationPort{ID: portID}, payload)
		}
	}
	return outputs
}

// attemptPathKind sizes and classifies a file, or names a folder.
func attemptPathKind(path string) (int64, string) {
	info, err := os.Stat(path)
	switch {
	case err != nil:
		return 0, ""
	case info.IsDir():
		return 0, PortKindFolder
	case !info.Mode().IsRegular():
		return info.Size(), "binary"
	}
	return info.Size(), classifyEvidenceFile(path, info.Size())
}

func classifyEvidenceFile(path string, size int64) string {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "binary"
	}
	defer file.Close()
	head := make([]byte, evidenceKindHeadBytes)
	n, err := io.ReadFull(file, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return "binary"
	}
	return evidenceArtifactKind(filepath.Base(path), head[:n], int64(n) < size)
}

// listAttemptFiles lists the regular files in an attempt's artifact directory,
// named relative to the run's artifact directory.
func (s *Store) listAttemptFiles(runID, artifactsDir, nodeID string, attempt int, outputs []AttemptOutput, evidence ...string) ([]AttemptFile, bool, int, error) {
	root, err := s.openRunArtifactsRoot(runID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return []AttemptFile{}, false, 0, nil
		}
		return nil, false, 0, err
	}
	current := root
	for _, component := range []string{nodeID, strconv.Itoa(attempt)} {
		next, err := openFollowingAt(current, component, true)
		_ = current.Close()
		if err != nil {
			return []AttemptFile{}, false, 0, nil
		}
		current = next
	}
	defer current.Close()
	entries := []RunArtifactEntry{}
	truncated := false
	if err := listRunArtifactsAt(current, nodeID+"/"+strconv.Itoa(attempt)+"/", 3, &entries, &truncated); err != nil {
		return nil, false, 0, err
	}
	entries, omitted := selectEvidenceArtifacts(entries, outputs, evidence...)
	files := make([]AttemptFile, 0, len(entries))
	for _, entry := range entries {
		path := filepath.Join(artifactsDir, filepath.FromSlash(entry.Name))
		files = append(files, AttemptFile{Name: entry.Name, Path: path, Size: entry.Size, ModifiedAt: entry.ModifiedAt, Kind: classifyEvidenceFile(path, entry.Size)})
	}
	return files, truncated || omitted > 0, omitted, nil
}
