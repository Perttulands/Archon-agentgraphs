package formations

import (
	"errors"
	"fmt"
	"strconv"
	"time"
)

var ErrInvalidExecutionPolicy = errors.New("invalid formation execution policy")

// FormationExecutionPolicy is authored workload policy, independent of the
// harness and of the run's total budget. A missing policy inherits the default
// captured when the run is admitted.
type FormationExecutionPolicy struct {
	TimeoutSeconds int `json:"timeoutSeconds"`
}

type FormationExecutionPolicyRequest struct {
	FormationID    string
	TimeoutSeconds int
	UpdatedBy      string
}

func validExecutionSeconds(seconds int) bool {
	return seconds > 0 && int64(seconds) <= int64((1<<63-1)/time.Second)
}

// SetFormationExecutionPolicy uses zero to remove the override. An authored
// table, when present, always contains a positive duration.
func (s *Store) SetFormationExecutionPolicy(slug string, req FormationExecutionPolicyRequest, opts WriteOptions) (*BoardDocument, error) {
	if req.TimeoutSeconds != 0 && !validExecutionSeconds(req.TimeoutSeconds) {
		return nil, fmt.Errorf("%w: timeoutSeconds must be a positive whole number of seconds, or zero to inherit", ErrInvalidExecutionPolicy)
	}
	return s.updateBoardDefinition(slug, req.UpdatedBy, opts, func(raw []byte, _ *BoardDocument) ([]byte, error) {
		lines := splitLines(raw)
		start, end, ok := findFormationBlockByID(lines, req.FormationID)
		if !ok {
			return nil, ErrNotFound
		}
		first, last, exists := findFormationSection(lines, start, end, "formation.execution")
		if req.TimeoutSeconds == 0 {
			if exists {
				lines = append(lines[:first], lines[last:]...)
			}
			return renderTOMLLines(lines), nil
		}
		if exists {
			lines = setScalarInLineRange(lines, first+1, last, "timeoutSeconds", strconv.Itoa(req.TimeoutSeconds))
		} else {
			lines = insertTomLLines(lines, formationHeaderEnd(lines, start, end), []tomlLine{
				{body: "[formation.execution]", newline: "\n"},
				{body: "timeoutSeconds = " + strconv.Itoa(req.TimeoutSeconds), newline: "\n"},
			})
		}
		return renderTOMLLines(lines), nil
	})
}

func executionPolicyFindings(formations []FormationNode) []BoardFinding {
	var findings []BoardFinding
	for _, node := range formations {
		if node.Execution != nil && !validExecutionSeconds(node.Execution.TimeoutSeconds) {
			findings = append(findings, BoardFinding{Code: "invalid_execution_policy", NodeID: node.ID,
				Message: "formation execution.timeoutSeconds must be a positive whole number of seconds; remove the policy to inherit the run default"})
		}
	}
	return findings
}
