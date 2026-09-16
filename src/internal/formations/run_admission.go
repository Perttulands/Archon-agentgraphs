package formations

import (
	"errors"
	"fmt"
	"strings"
)

// Finding codes added by ValidateRunAdmission on top of ValidateBoard.
const (
	FindingUnsupportedFormationType = "unsupported_formation_type"
	FindingFormationWithoutSlots    = "formation_without_slots"
	FindingUnstaffedSlot            = "unstaffed_slot"
	FindingUnavailablePersona       = "unavailable_persona"
	FindingOrchestratedController   = "orchestrated_controller"
	FindingToolExecutionUnavailable = ToolExecutionUnavailableCode
)

// ErrRunAdmission marks a run start rejected by its admission report.
var ErrRunAdmission = errors.New("run_admission_failed")

// RunAdmissionError carries every blocking finding of a rejected run start.
type RunAdmissionError struct {
	Findings []BoardFinding
}

func (e *RunAdmissionError) Error() string {
	parts := make([]string, 0, len(e.Findings))
	for _, finding := range e.Findings {
		parts = append(parts, finding.Message)
	}
	return fmt.Sprintf("run admission found %d problem(s): %s", len(e.Findings), strings.Join(parts, "; "))
}

func (e *RunAdmissionError) Unwrap() error { return ErrRunAdmission }

// RunAdmissionScope selects the run being admitted. The zero scope checks the
// whole board, as board validation and the canvas draft markers do.
type RunAdmissionScope struct {
	MissionID   string
	FormationID string
}

// ValidateRunAdmission lists every problem that would stop a run, so authoring
// can accept drafts and admission reports what they still need. It extends
// ValidateBoard with supported formation types, staffing, readable personas,
// orchestrated controllers, runnable Tools and a wired mission.
//
// Findings are limited to the nodes the run reaches from its mission (or the
// selected formation). Formation types, slot counts and persona bindings are
// checked across the whole board, because the run snapshot binds every
// formation on it.
func ValidateRunAdmission(board *BoardDocument, personas *PersonaStore, scope RunAdmissionScope) BoardValidationReport {
	report := BoardValidationReport{Errors: []BoardFinding{}, Warnings: []BoardFinding{}}
	if board == nil {
		return report
	}
	var selected map[string]bool
	switch {
	case scope.MissionID != "":
		selected = reachableNodeIDs(board, scope.MissionID)
	case scope.FormationID != "":
		selected = map[string]bool{scope.FormationID: true}
	}
	inScope := func(nodeID string) bool { return selected == nil || selected[nodeID] }
	connectionInScope := make(map[string]bool, len(board.Connections))
	for _, connection := range board.Connections {
		from, _ := endpointParts(connection.From)
		to, _ := endpointParts(connection.To)
		connectionInScope[connection.ID] = connectionInScope[connection.ID] || inScope(from) || inScope(to)
	}
	findingInScope := func(finding BoardFinding) bool {
		switch {
		case selected == nil || finding.Code == FindingInvalidFormationType:
			return true
		case finding.NodeID == "":
			return false
		default:
			return selected[finding.NodeID] || connectionInScope[finding.NodeID]
		}
	}

	structural := ValidateBoard(board)
	for _, finding := range structural.Errors {
		if findingInScope(finding) {
			report.Errors = append(report.Errors, finding)
		}
	}
	for _, finding := range structural.Warnings {
		switch {
		case finding.Code == FindingMissionNotRunnable && finding.NodeID == scope.MissionID:
			report.Errors = append(report.Errors, finding)
		case findingInScope(finding):
			report.Warnings = append(report.Warnings, finding)
		}
	}

	for _, formation := range board.Formations {
		report.Errors = append(report.Errors, formationAdmissionFindings(formation, personas, inScope(formation.ID))...)
	}
	if selected != nil {
		for _, tool := range board.Tools {
			if selected[tool.ID] {
				report.Errors = append(report.Errors, BoardFinding{
					Code:    FindingToolExecutionUnavailable,
					NodeID:  tool.ID,
					Message: fmt.Sprintf("Tool %q cannot execute in this runtime; take it off the run path", tool.ID),
				})
			}
		}
	}

	sortFindings(report.Errors)
	sortFindings(report.Warnings)
	return report
}

// CheckRunAdmission returns a *RunAdmissionError listing every blocking finding,
// ErrNotFound for a missing mission or formation, or nil when the run may start.
func CheckRunAdmission(board *BoardDocument, personas *PersonaStore, scope RunAdmissionScope) error {
	if board == nil {
		return ErrNotFound
	}
	if _, ok := findMission(board, scope.MissionID); scope.MissionID != "" && !ok {
		return fmt.Errorf("%w: mission %q", ErrNotFound, scope.MissionID)
	}
	if _, ok := findFormation(board.Formations, scope.FormationID); scope.FormationID != "" && !ok {
		return fmt.Errorf("%w: formation %q", ErrNotFound, scope.FormationID)
	}
	if report := ValidateRunAdmission(board, personas, scope); len(report.Errors) > 0 {
		return &RunAdmissionError{Findings: report.Errors}
	}
	return nil
}

func formationAdmissionFindings(formation FormationNode, personas *PersonaStore, reached bool) []BoardFinding {
	var findings []BoardFinding
	add := func(code, format string, args ...any) {
		findings = append(findings, BoardFinding{Code: code, NodeID: formation.ID, Message: fmt.Sprintf(format, args...)})
	}
	// An invalid type is already reported by ValidateBoard.
	if validateFormationType(formation.Type) == nil && !runtimeSupportsFormationType(formation.Type) {
		add(FindingUnsupportedFormationType, "formation %q uses type %q, which the runtime cannot run; change it to solo, peer or orchestrated", formation.ID, formation.Type)
	}
	if len(formation.Slots) == 0 {
		add(FindingFormationWithoutSlots, "formation %q has no slots; add a slot and staff it", formation.ID)
	}
	controllers := 0
	for _, slot := range formation.Slots {
		if slot.Controller {
			controllers++
		}
		if slot.AgentID == "" {
			if reached {
				add(FindingUnstaffedSlot, "formation %q slot %s needs an agent", formation.ID, slotName(slot))
			}
			continue
		}
		if personas == nil {
			continue
		}
		card, err := personas.ReadPersona(slot.AgentID)
		if errors.Is(err, ErrNotFound) {
			add(FindingUnavailablePersona, "formation %q slot %s names unknown agent %q", formation.ID, slotName(slot), slot.AgentID)
			continue
		}
		if err != nil {
			add(FindingUnavailablePersona, "formation %q slot %s cannot read agent %q: %v", formation.ID, slotName(slot), slot.AgentID, err)
			continue
		}
		if _, err := card.SelectHarnessVariant(slot.Harness); err != nil {
			add(FindingUnavailablePersona, "formation %q slot %s cannot bind agent %q: %v", formation.ID, slotName(slot), slot.AgentID, err)
		}
	}
	if reached && formation.Type == FormationTypeOrchestrated && len(formation.Slots) > 0 {
		switch {
		case controllers != 1:
			add(FindingOrchestratedController, "orchestrated formation %q needs exactly one controller slot; it has %d", formation.ID, controllers)
		case len(formation.Slots) == 1:
			add(FindingOrchestratedController, "orchestrated formation %q needs a worker slot beside its controller", formation.ID)
		}
	}
	return findings
}

func runtimeSupportsFormationType(formationType string) bool {
	return formationType == FormationTypeSolo || formationType == FormationTypePeer || formationType == FormationTypeOrchestrated
}

func slotName(slot FormationSlot) string {
	if slot.Label != "" {
		return fmt.Sprintf("%q (%s)", slot.Label, slot.ID)
	}
	return fmt.Sprintf("%q", slot.ID)
}
