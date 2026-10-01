package main

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// formation assign takes the same staffing flags offline and with --server.

func formationAssignUsage() string {
	efforts := []string{}
	for _, harness := range formations.LaunchableHarnesses() {
		efforts = append(efforts, harness.ID+" takes "+strings.Join(harness.Efforts, ", "))
	}
	return "usage: archon formation assign <mission> <formation> --slot <slot> --harness <claude-code|openai-codex> --effort <effort> [--model <model>] [--role <persona>] [--json]\n" +
		"A slot owns its harness, model and effort; --role adds optional role text, and a slot without one is a vanilla agent.\n" +
		"Choose the effort by the policy: " + formations.EffortPolicyText() + ". " + strings.Join(efforts, "; ") + ".\n" +
		"A blank --model means the harness default model."
}

type slotAssignFlags struct {
	slot, role, harness, model, effort, updatedBy *string
	jsonOut                                       *bool
}

func newSlotAssignFlags(fs *flag.FlagSet, stderr io.Writer) slotAssignFlags {
	flags := slotAssignFlags{
		slot:      fs.String("slot", "", "slot id"),
		role:      fs.String("role", "", "optional role (persona id); omit it for a vanilla agent"),
		harness:   fs.String("harness", "", "harness the seat runs: claude-code or openai-codex"),
		model:     fs.String("model", "", "model the seat runs; blank means the harness default model"),
		effort:    fs.String("effort", "", "reasoning effort; policy: "+formations.EffortPolicyText()),
		updatedBy: fs.String("updated-by", "agent:archon", "update actor"),
		jsonOut:   fs.Bool("json", false, "write JSON"),
	}
	fs.Usage = func() {
		fmt.Fprintln(stderr, formationAssignUsage())
		fs.PrintDefaults()
	}
	return flags
}

// resolve checks the parsed flags: a slot, a harness and an effort are
// required, so staffing always states its effort.
func (f slotAssignFlags) resolve(fs *flag.FlagSet, stderr io.Writer) (role string, ok bool) {
	role = *f.role
	if fs.NArg() != 2 || *f.slot == "" || *f.harness == "" || *f.effort == "" {
		fmt.Fprintln(stderr, formationAssignUsage())
		return "", false
	}
	return role, true
}

func (f slotAssignFlags) request(formationID, role string) formations.FormationSlotAssignmentRequest {
	return formations.FormationSlotAssignmentRequest{
		FormationID: formationID,
		SlotID:      *f.slot,
		AgentID:     role,
		Harness:     *f.harness,
		Model:       *f.model,
		Effort:      *f.effort,
		UpdatedBy:   *f.updatedBy,
	}
}

// assignedText reads the slot's staffing from the written board.
func assignedText(board *formations.BoardDocument, formationID, slotID string) string {
	for _, formation := range board.Formations {
		if formation.ID != formationID {
			continue
		}
		for _, slot := range formation.Slots {
			if slot.ID == slotID {
				return fmt.Sprintf("%s is %s", slotID, slot.StaffingSummary())
			}
		}
	}
	return "assigned " + slotID
}
