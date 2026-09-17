package formations

import (
	"context"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// Peer collaboration has two phases, not a prescribed conversation schedule.
// Openings stay private until everyone has finished. The same seats then run
// concurrently, choosing their messages and waiting on the durable journal.
func (e *TmuxFormationExecutor) executePeerFormation(parent context.Context, req FormationExecution) (FormationExecutionResult, error) {
	peers, err := peerFormationSlots(req.Formation)
	if err != nil {
		return FormationExecutionResult{}, err
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	owned := newOwnedSessions()
	owned.ctx, owned.req = ctx, req
	defer e.teardownOwnedSessions(owned)
	bindings := make([]tmuxSlotBinding, 0, len(peers))
	participants := make([]string, 0, len(peers))
	for _, peer := range peers {
		binding, err := e.resolveSlotBinding(ctx, req, peer, e.allowedHarnesses(), owned)
		if err != nil {
			return FormationExecutionResult{}, withSlot(err, req.NodeID, peer.ID, "")
		}
		bindings = append(bindings, binding)
		participants = append(participants, peer.ID)
	}
	openingResults, err := e.executePeerPhase(req, bindings, "peer-opening", func(binding tmuxSlotBinding) []string {
		return []string{
			"Prepare your independent opening statement for this peer formation.",
			"Explain your position, supporting evidence, assumptions and questions. Do not inspect another peer's seat, transcript or work; all openings will be shared together after everyone finishes.",
			"This is the opening phase only. Return your statement with the normal completion sentinel; the same seat will receive the conversation next. Do not produce the formation's final output yet.",
			"The total preparation, discussion and finalization deadline is " + req.Deadline.UTC().Format(time.RFC3339Nano) + ". Keep your opening proportionate to the remaining time.",
		}
	}, owned, cancel)
	if err != nil {
		return FormationExecutionResult{}, err
	}
	openings := make(map[string]string, len(openingResults))
	for _, output := range openingResults {
		if strings.TrimSpace(output.Text) == "" {
			return FormationExecutionResult{}, runExecutionError("peer_opening_missing", "a peer returned no opening statement", "peer", nil)
		}
		openings[output.SlotID] = output.Text
	}
	path, err := e.store.CreatePeerConversation(req, participants, openings, req.Deadline)
	if err != nil {
		return FormationExecutionResult{}, err
	}
	id := PeerConversationID{RunID: req.RunID, NodeID: req.NodeID, Attempt: req.Attempt}
	defer e.store.ClosePeerConversation(id, "formation execution ended")
	if err := e.appendPeerPlaneEvent(req, path, bindings); err != nil {
		return FormationExecutionResult{}, err
	}
	_, err = e.executePeerPhase(req, bindings, "peer-conversation", func(binding tmuxSlotBinding) []string {
		return e.peerConversationInstructions(req, binding, path)
	}, owned, cancel)
	if err != nil {
		return FormationExecutionResult{}, err
	}
	conversation, err := e.store.ReadPeerConversation(id)
	if err != nil {
		return FormationExecutionResult{}, err
	}
	if conversation.Status != "agreed" || strings.TrimSpace(conversation.FinalText) == "" {
		return FormationExecutionResult{}, runExecutionError("peer_result_incomplete", "peers ended without an acknowledged result; inspect the retained conversation", "peer", nil)
	}
	return owned.finish(e.formationResultFromText(req, path, conversation.FinalText))
}

// Every seat may work concurrently, but no seat receives overlapping turns.
// On failure, cancel siblings and drain all outcomes before seat cleanup.
func (e *TmuxFormationExecutor) executePeerPhase(req FormationExecution, bindings []tmuxSlotBinding, phase string, instructions func(tmuxSlotBinding) []string, owned *ownedSessions, cancel context.CancelFunc) ([]tmuxSlotOutput, error) {
	type outcome struct {
		index  int
		output tmuxSlotOutput
		err    error
	}
	outcomes := make(chan outcome, len(bindings))
	dispatcher := NewSlotDispatcher(e.store, nil)
	for index, binding := range bindings {
		extra := instructions(binding)
		go func(index int, binding tmuxSlotBinding, extra []string) {
			if err := e.seatClient.Ready(owned.ctx, e.config.Socket, owned.seats[binding.Slot.ID], binding.Variant.ID); err != nil {
				outcomes <- outcome{index: index, err: withSlot(err, req.NodeID, binding.Slot.ID, "")}
				return
			}
			output, err := e.executeBoundSlot(req, binding, dispatcher, phase, extra, owned)
			if err == nil && phase == "peer-opening" {
				err = e.preservePeerOpening(req, output)
			}
			outcomes <- outcome{index: index, output: output, err: err}
		}(index, binding, extra)
	}
	results := make([]tmuxSlotOutput, len(bindings))
	var firstErr error
	for range bindings {
		result := <-outcomes
		results[result.index] = result.output
		if result.err != nil && firstErr == nil {
			firstErr = result.err
			cancel()
		}
	}
	return results, firstErr
}

// Preserve each finished opening even if another seat never finishes. These
// private artifacts are not advertised to peers before the opening barrier.
func (e *TmuxFormationExecutor) preservePeerOpening(req FormationExecution, output tmuxSlotOutput) error {
	ledger, err := e.store.openRunLedger(req.RunID, false)
	if err != nil {
		return err
	}
	defer ledger.close()
	identity := sha256.Sum256([]byte(req.NodeID + "\x00" + output.SlotID))
	suffix := fmt.Sprintf(".peer-%x.attempt-%d.opening.md", identity, req.Attempt)
	if err := writeRunArtifactExclusiveAt(ledger.directory, req.RunID+suffix, []byte(output.Text)); err != nil {
		return err
	}
	return e.store.AppendRunEvent(req.RunID, RunEvent{Type: "peer_opening", NodeID: req.NodeID, SlotID: output.SlotID, Attempt: req.Attempt,
		Data: map[string]any{"path": runArtifactPath(ledger.directory.slug, req.RunID, suffix)}})
}

func (e *TmuxFormationExecutor) peerConversationInstructions(req FormationExecution, binding tmuxSlotBinding, path string) []string {
	cli := e.config.PeerCLI
	if cli == "" {
		cli = "archon"
	}
	base := shellQuote(cli) + " --workspace " + shellQuote(e.store.Workspace) + " peer "
	identity := fmt.Sprintf(" --run %s --node %s --attempt %d --slot %s", shellQuote(req.RunID), shellQuote(req.NodeID), req.Attempt, shellQuote(binding.Slot.ID))
	lines := []string{
		"All independent openings are now published. Work with the other peers through the shared conversation file: " + filepath.Join(e.store.Workspace, ".formations", "artifacts", req.RunID, path),
		"The conversation's artifact reference, relative to this run's artifact directory, is " + path + ". Use that reference in your completion sentinel.",
		"You are equal peers: there is no controller or prescribed turn order. Read, respond, investigate and propose as useful. Continue this turn while the conversation is open; a first message does not finish your participation.",
		"Use the commands below for all writes. They append attributed, ordered records safely. Do not edit, overwrite or append to the journal directly and do not drive another peer's terminal.",
		"Read: " + base + "read" + identity,
		"Post a message: " + base + "post" + identity + " --text-file <your-message-file>",
		"Wait for new messages when there is nothing useful to do: " + base + "wait" + identity + " --after <lastSeq-from-read>",
		"Propose a complete result: " + base + "propose" + identity + " --text-file <your-result-file>",
		"Acknowledge a proposal after reading it: " + base + "ack" + identity + " --proposal <proposal-sequence>",
		"Dissent with a proposal: " + base + "post" + identity + " --dissent --proposal <proposal-sequence> --text-file <your-reasons-file>",
		"Any peer may propose. The proposer must also acknowledge. Every peer must acknowledge the same proposal for status agreed. If you disagree, explain why and work toward a revision. Agreement can be on an accurate account of unresolved tensions, alternatives and the decision the operator must make; do not manufacture consensus.",
		"The proposed text must contain the ordinary declared output payloads described below, including remaining tensions. Once status is agreed, return a brief receipt and the normal completion sentinel using the conversation path as its artifact. The runtime routes the acknowledged proposal as the formation output.",
		"The entire formation deadline is " + req.Deadline.UTC().Format(time.RFC3339Nano) + ". Preparation has already used part of this budget. Leave time to write and acknowledge the result and finish your turn. Near the deadline, stop opening new debate and preserve the result and tensions. The budget will not be extended; expiry without a valid result blocks visibly.",
	}
	return append(lines, outputContractExtraLines(req.Formation)...)
}
