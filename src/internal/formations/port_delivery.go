package formations

// A formation's input port holds the latest work delivered to it. Two
// deliveries can reach one port before the formation runs: a step's output and
// a gate's send-back routed while another seat worked, say (archon-o7p.11).
// The port then keeps both, so no verdict and no output is lost: the latest
// work stays the input, and every gate feedback and human response the
// formation has not run on travels with it. A port's deliveries count as read
// once the formation records its output. Live runs and replay deliver in
// ledger order and apply the same rule, so they agree.

// deliverToPort puts input in a formation's port, keeping what an unread
// delivery there carried.
func deliverToPort(ready map[string]map[string]RunInputRef, nodeID, portID string, input RunInputRef) {
	if ready[nodeID] == nil {
		ready[nodeID] = map[string]RunInputRef{}
	}
	if older, ok := ready[nodeID][portID]; ok && older.unread {
		input = mergePortInputs(older, input)
	}
	input.unread = true
	ready[nodeID][portID] = input
}

// markPortsRead records that a formation ran on everything in its ports.
func markPortsRead(ready map[string]map[string]RunInputRef, nodeID string) {
	for portID, input := range ready[nodeID] {
		input.unread = false
		ready[nodeID][portID] = input
	}
}

// mergePortInputs combines two unread deliveries to one port. A send-back
// reaching unread work leaves that work the input, with the feedback; newer
// work takes the place of older work and keeps its feedback; feedback and
// responses that meet chain, newest first.
func mergePortInputs(older, newer RunInputRef) RunInputRef {
	merged := newer
	if newer.Feedback != nil && older.Feedback == nil {
		merged = older
		merged.Feedback = newer.Feedback
		merged.Response = newer.Response
	}
	merged.Feedback = chainFeedback(merged.Feedback, other(merged.Feedback, older.Feedback, newer.Feedback))
	merged.Response = chainResponse(merged.Response, otherResponse(merged.Response, older.Response, newer.Response))
	return merged
}

// other is whichever of two feedbacks the merge did not keep, or nil.
func other(kept, a, b *GateFeedback) *GateFeedback {
	switch kept {
	case a:
		return b
	case b:
		return a
	}
	return nil
}

func otherResponse(kept, a, b *GateResponse) *GateResponse {
	switch kept {
	case a:
		return b
	case b:
		return a
	}
	return nil
}

// chainFeedback puts earlier behind newest, keeping newest's own chain.
func chainFeedback(newest, earlier *GateFeedback) *GateFeedback {
	if newest == nil {
		return earlier
	}
	if earlier == nil {
		return newest
	}
	chained := *newest
	chained.Earlier = chainFeedback(newest.Earlier, earlier)
	return &chained
}

func chainResponse(newest, earlier *GateResponse) *GateResponse {
	if newest == nil {
		return earlier
	}
	if earlier == nil {
		return newest
	}
	chained := *newest
	chained.Earlier = chainResponse(newest.Earlier, earlier)
	return &chained
}

func gateFeedbackFromAny(value any) *GateFeedback {
	fields, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return &GateFeedback{
		GateID: stringFromAny(fields["gateId"]), GateAttempt: intFromRunEventData(fields["gateAttempt"]),
		Verdict: stringFromAny(fields["verdict"]), Reason: stringFromAny(fields["reason"]),
		Evidence:    gateEvidenceRefsFromRunEventData(fields["evidence"]),
		OriginalRef: stringFromAny(fields["originalRef"]), OriginalText: stringFromAny(fields["originalText"]),
		Earlier: gateFeedbackFromAny(fields["earlier"]),
	}
}

func gateResponseFromAny(value any) *GateResponse {
	fields, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return &GateResponse{
		GateID: stringFromAny(fields["gateId"]), GateAttempt: intFromRunEventData(fields["gateAttempt"]),
		RequestedSeq: intFromRunEventData(fields["requestedSeq"]),
		DecidedBy:    stringFromAny(fields["decidedBy"]), Text: stringFromAny(fields["text"]),
		Earlier: gateResponseFromAny(fields["earlier"]),
	}
}
