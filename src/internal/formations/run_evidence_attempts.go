package formations

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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

// attemptAt locates one attempt: its step's index and its own.
type attemptAt struct{ step, attempt int }

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
	FilesTruncated bool              `json:"filesTruncated,omitempty"`
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
// verdict it reached, or Pending while it waits.
type AttemptVerdict struct {
	GateID          string         `json:"gateId"`
	GateTitle       string         `json:"gateTitle,omitempty"`
	EvaluationSeq   int            `json:"evaluationSeq"`
	Seq             int            `json:"seq,omitempty"`
	Verdict         string         `json:"verdict,omitempty"`
	Pending         bool           `json:"pending,omitempty"`
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
			if attempt.Files, attempt.FilesTruncated, err = s.listAttemptFiles(runID, artifactsDir, step.NodeID, attempt.Attempt); err != nil {
				return nil, err
			}
		}
	}
	return evidence, nil
}

func projectRunAttempts(runID, onlyNode string, board *BoardDocument, events []RunEvent, status *RunStatusProjection, artifactRoots []string) *RunAttemptsEvidence {
	evidence := &RunAttemptsEvidence{RunID: runID, Steps: []StepAttempts{}}
	capper := &evidenceCapper{remaining: EvidenceNodeBudgetBytes}
	stepIndex := map[string]int{}
	// outputAttempt maps a node_output seq to the attempt that produced it.
	outputAttempt := map[int]attemptAt{}
	latestOutput := map[string]int{}
	verdictAt := map[string]attemptAt{}
	verdictIndex := map[string]int{}
	current := func(nodeID string) *StepAttempt {
		index, ok := stepIndex[nodeID]
		if !ok || len(evidence.Steps[index].Attempts) == 0 {
			return nil
		}
		attempts := evidence.Steps[index].Attempts
		return &attempts[len(attempts)-1]
	}
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
				evidence.Steps = append(evidence.Steps, StepAttempts{NodeID: formation.ID, Title: capper.text(formation.Title).Text, Outputs: outputs, Attempts: []StepAttempt{}})
			}
			step := &evidence.Steps[at]
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
			attempt := current(event.NodeID)
			if attempt == nil {
				continue
			}
			step := &evidence.Steps[stepIndex[event.NodeID]]
			attempt.Status, attempt.OutputSeq, attempt.EndedAt = AttemptDone, event.Seq, event.Timestamp
			attempt.Outputs = attemptOutputs(step.Outputs, outputPayloadsFromAny(event.Data["outputs"]), capper, artifactRoots)
			outputAttempt[event.Seq] = attemptAt{stepIndex[event.NodeID], len(step.Attempts) - 1}
			latestOutput[event.NodeID] = event.Seq
		case RunEventGateEvaluating:
			input := runInputRefFromAny(event.Data["inputRef"])
			outputSeq, ok := latestOutput[input.FromNodeID]
			if !ok {
				continue
			}
			at := outputAttempt[outputSeq]
			attempt := &evidence.Steps[at.step].Attempts[at.attempt]
			verdict := AttemptVerdict{GateID: event.GateID, EvaluationSeq: event.Seq, Pending: !status.Final, Reason: capper.text(""), Evidence: []EvidenceItem{}}
			if gate, ok := findGate(board.Gates, event.GateID); ok {
				verdict.GateTitle = capper.text(gate.Title).Text
			}
			attempt.Verdicts = append(attempt.Verdicts, verdict)
			verdictAt[event.GateID] = at
			verdictIndex[event.GateID] = len(attempt.Verdicts) - 1
			for _, judge := range stringsFromEventData(event.Data["judgeChain"]) {
				if step, ok := stepIndex[judge]; ok && !containsToolString(evidence.Steps[step].JudgeOf, event.GateID) {
					evidence.Steps[step].JudgeOf = append(evidence.Steps[step].JudgeOf, event.GateID)
				}
			}
		case RunEventHumanVerdictRecorded:
			if verdict := pendingAttemptVerdict(evidence, verdictAt, verdictIndex, event.GateID); verdict != nil {
				verdict.DecidedBy = stringFromEventData(event, "decidedBy")
			}
		case RunEventGateVerdict:
			verdict := pendingAttemptVerdict(evidence, verdictAt, verdictIndex, event.GateID)
			if verdict == nil {
				continue
			}
			verdict.Seq, verdict.Verdict, verdict.Pending = event.Seq, stringFromEventData(event, "verdict"), false
			verdict.Reason = capper.text(stringFromEventData(event, "reason"))
			verdict.Evidence, verdict.EvidenceOmitted = evidenceItems(event.Data["evidence"], capper)
			if decidedBy := stringFromEventData(event, "decidedBy"); decidedBy != "" {
				verdict.DecidedBy = decidedBy
			}
			delete(verdictAt, event.GateID)
		case RunEventBlocked, RunEventError, RunEventFailed, RunEventCanceled:
			for _, nodeID := range runProblemNodes(events, index) {
				if attempt := current(nodeID); attempt != nil && attempt.Status == AttemptRunning && event.Type != RunEventError {
					attempt.Status, attempt.EndedAt = AttemptStopped, event.Timestamp
				}
			}
		case RunEventResumed:
			// A resumed run may reattach a stopped attempt and record its output.
			for _, step := range evidence.Steps {
				if attempt := current(step.NodeID); attempt != nil && attempt.Status == AttemptStopped && attempt.OutputSeq == 0 {
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
	return evidence
}

// pendingAttemptVerdict is the evaluation of a gate still waiting on its verdict.
func pendingAttemptVerdict(evidence *RunAttemptsEvidence, at map[string]attemptAt, index map[string]int, gateID string) *AttemptVerdict {
	where, ok := at[gateID]
	if !ok {
		return nil
	}
	return &evidence.Steps[where.step].Attempts[where.attempt].Verdicts[index[gateID]]
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
func (s *Store) listAttemptFiles(runID, artifactsDir, nodeID string, attempt int) ([]AttemptFile, bool, error) {
	root, err := s.openRunArtifactsRoot(runID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return []AttemptFile{}, false, nil
		}
		return nil, false, err
	}
	current := root
	for _, component := range []string{nodeID, strconv.Itoa(attempt)} {
		next, err := openFollowingAt(current, component, true)
		_ = current.Close()
		if err != nil {
			return []AttemptFile{}, false, nil
		}
		current = next
	}
	defer current.Close()
	entries := []RunArtifactEntry{}
	truncated := false
	if err := listRunArtifactsAt(current, nodeID+"/"+strconv.Itoa(attempt)+"/", 3, &entries, &truncated); err != nil {
		return nil, false, err
	}
	files := make([]AttemptFile, 0, len(entries))
	for _, entry := range entries {
		path := filepath.Join(artifactsDir, filepath.FromSlash(entry.Name))
		files = append(files, AttemptFile{Name: entry.Name, Path: path, Size: entry.Size, ModifiedAt: entry.ModifiedAt, Kind: classifyEvidenceFile(path, entry.Size)})
	}
	return files, truncated, nil
}
