package formations

import (
	"fmt"
	"strings"
)

func (v HarnessVariant) effectiveEffort() string {
	if strings.TrimSpace(v.Effort) == "" {
		return "medium"
	}
	return strings.TrimSpace(v.Effort)
}

// RenderLaunch makes card settings authoritative over legacy launch strings.
// The executable is supplied by the adapter after resolving it on PATH.
func (v HarnessVariant) RenderLaunch(executable string) (string, error) {
	command := "exec " + shellQuote(executable)
	if v.Model != "" {
		command += " --model " + shellQuote(v.Model)
	}
	switch v.ID {
	case "openai-codex":
		command += " -c " + shellQuote("model_reasoning_effort=\""+v.effectiveEffort()+"\"")
		command += " -c check_for_update_on_startup=false --dangerously-bypass-approvals-and-sandbox"
	case "claude-code":
		command += " --effort " + shellQuote(v.effectiveEffort()) + " --dangerously-skip-permissions"
	default:
		return "", fmt.Errorf("unsupported seat harness %q", v.ID)
	}
	return command, nil
}

func (v HarnessVariant) verifyTurnSettings(turn codexTranscriptTurn) error {
	// Claude records model on assistant messages but may omit effort. Codex
	// records both in turn_context, so missing evidence there is a mismatch.
	if v.Model != "" && turn.Model != v.Model || (v.ID == "openai-codex" || turn.Effort != "") && turn.Effort != v.effectiveEffort() {
		return fmt.Errorf("seat model/effort mismatch: expected %s/%s, observed %s/%s", v.Model, v.effectiveEffort(), turn.Model, turn.Effort)
	}
	return nil
}
