package coordinator

import (
	"strings"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

func TestProjectionKeepsFullGateVerdictReason(t *testing.T) {
	reason := strings.Repeat("The judge checked the evidence. ", 100)
	p := project(&formations.RunStatusProjection{RunID: "run_reason"}, []formations.RunEvent{
		{Seq: 1, Type: formations.RunEventGateVerdict, GateID: "gate_review", Data: map[string]any{"verdict": "pass", "reason": reason}},
	})
	if got := p.Events[0].Reason; got != reason {
		t.Fatalf("gate verdict reason = %q, want full recorded reason", got)
	}
}
