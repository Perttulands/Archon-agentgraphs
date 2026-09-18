package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/Perttulands/Archon-agentgraphs/internal/coordinator"
)

// The reads use only public projections; sessionName can be matched to tmux
// list-panes by an operator on the daemon host.
func printRemoteRunRead(command string, raw []byte, jsonOut bool, stdout, stderr io.Writer) int {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fail(stderr, err)
	}
	data := envelope["data"]
	if len(data) == 0 || string(data) == "null" {
		return fail(stderr, fmt.Errorf("coordinator response has no data"))
	}
	switch command {
	case "run gates":
		var projection struct {
			RunID        string                    `json:"runId"`
			WaitingGates []coordinator.GateRequest `json:"waitingGates"`
		}
		if err := json.Unmarshal(data, &projection); err != nil {
			return fail(stderr, err)
		}
		if projection.WaitingGates == nil {
			projection.WaitingGates = []coordinator.GateRequest{}
		}
		if jsonOut {
			var err error
			envelope["data"], err = json.Marshal(projection)
			if err != nil {
				return fail(stderr, err)
			}
			if err := json.NewEncoder(stdout).Encode(envelope); err != nil {
				return fail(stderr, err)
			}
			return 0
		}
		if len(projection.WaitingGates) == 0 {
			fmt.Fprintln(stdout, "No pending human gates.")
		}
		for _, gate := range projection.WaitingGates {
			fmt.Fprintf(stdout, "gate %s requested-seq %d\n", gate.GateID, gate.RequestedSeq)
			for _, seat := range gate.AskedSeats {
				fmt.Fprintf(stdout, "  asked node %s slot %s created-seq %d delivered-seq %d\n", seat.NodeID, seat.SlotID, seat.CreatedSeq, seat.DeliveredSeq)
			}
			if gate.FallbackReason != "" {
				fmt.Fprintf(stdout, "  fallback: %s\n", gate.FallbackReason)
			}
		}
	case "run seats":
		var seats coordinator.SeatList
		if err := json.Unmarshal(data, &seats); err != nil {
			return fail(stderr, err)
		}
		if jsonOut {
			fmt.Fprint(stdout, string(raw))
			return 0
		}
		if !seats.Available {
			fmt.Fprintf(stdout, "Seat terminals unavailable: %s\n", seats.Reason)
		}
		if len(seats.Seats) == 0 {
			fmt.Fprintln(stdout, "No seats.")
		}
		for _, seat := range seats.Seats {
			fmt.Fprintf(stdout, "seat %d node %s slot %s state %s session %s\n", seat.CreatedSeq, seat.NodeID, seat.SlotID, seat.State, seat.SessionName)
			if seat.Reason != "" {
				fmt.Fprintf(stdout, "  reason: %s\n", seat.Reason)
			}
			if seat.OnCall != nil {
				fmt.Fprintln(stdout, "  on call")
				for _, ask := range seat.OnCall.WaitingOn {
					fmt.Fprintf(stdout, "  waiting gate %s requested-seq %d\n", ask.GateID, ask.RequestedSeq)
				}
			}
		}
	case "gate request":
		var payload struct {
			Request coordinator.PendingGateRequest `json:"request"`
		}
		if err := json.Unmarshal(data, &payload); err != nil {
			return fail(stderr, err)
		}
		request := payload.Request
		if jsonOut {
			fmt.Fprint(stdout, string(raw))
			return 0
		}
		fmt.Fprintf(stdout, "gate %s requested-seq %d\ncriterion: %s\ninput from %s:%s\n%s\n", request.GateID, request.RequestedSeq, request.Criterion, request.Input.FromNodeID, request.Input.FromPortID, request.Input.Text)
		if request.Input.Truncated {
			fmt.Fprintln(stdout, "[input truncated]")
		}
	}
	return 0
}
