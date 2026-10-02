package formations

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// DefaultHarnessEffort is the effort a harness variant with no effort runs at.
const DefaultHarnessEffort = "medium"

// LaunchableHarness is a harness whose seats Archon starts from slot settings.
// Efforts lists the values its CLI accepts: `claude --effort` for Claude Code,
// and the union of every Codex model's supported reasoning levels (Codex
// models_cache.json) for Codex, whose `-c model_reasoning_effort` takes them.
// Models lists the models it is known to run (HarnessModels).
type LaunchableHarness struct {
	ID            string         `json:"id"`
	Executable    string         `json:"executable"`
	Efforts       []string       `json:"efforts"`
	DefaultEffort string         `json:"defaultEffort"`
	Models        []HarnessModel `json:"models"`
}

var launchableHarnesses = []LaunchableHarness{
	{ID: "claude-code", Executable: "claude", Efforts: []string{"low", "medium", "high", "xhigh", "max"}, DefaultEffort: DefaultHarnessEffort},
	{ID: "openai-codex", Executable: "codex", Efforts: []string{"low", "medium", "high", "xhigh", "max", "ultra"}, DefaultEffort: DefaultHarnessEffort},
}

// LaunchableHarnesses returns the harnesses Archon can start seats for, each
// with the models this host knows it to run.
func LaunchableHarnesses() []LaunchableHarness {
	out := make([]LaunchableHarness, len(launchableHarnesses))
	for i, harness := range launchableHarnesses {
		harness.Efforts = slices.Clone(harness.Efforts)
		harness.Models = HarnessModels(harness.ID)
		out[i] = harness
	}
	return out
}

func launchableHarness(id string) (LaunchableHarness, bool) {
	for _, harness := range launchableHarnesses {
		if harness.ID == id {
			return harness, true
		}
	}
	return LaunchableHarness{}, false
}

func (v HarnessVariant) effectiveEffort() string {
	if strings.TrimSpace(v.Effort) == "" {
		return DefaultHarnessEffort
	}
	return strings.TrimSpace(v.Effort)
}

// RenderLaunch renders the seat command from the variant's model and effort.
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

// LaunchCommand is the command a seat for this variant runs: the harness CLI
// resolved on this process's PATH, rendered by RenderLaunch. When the CLI is
// not on PATH the command names it bare and the error says so.
func (v HarnessVariant) LaunchCommand() (string, error) {
	harness, ok := launchableHarness(v.ID)
	if !ok {
		return "", fmt.Errorf("unsupported seat harness %q", v.ID)
	}
	bin, err := exec.LookPath(harness.Executable)
	if err == nil {
		bin, err = filepath.Abs(bin)
	}
	if err != nil {
		command, renderErr := v.RenderLaunch(harness.Executable)
		if renderErr != nil {
			return "", renderErr
		}
		return command, fmt.Errorf("%s is not on PATH: %w", harness.Executable, err)
	}
	return v.RenderLaunch(bin)
}

// SeatLaunchCommand is the command a seat working in cwd runs: LaunchCommand,
// and for Codex that folder trusted for this launch only. Codex 0.159 asks
// "Trust this folder?" in every new folder and saves the answer in the
// operator's ~/.codex/config.toml; the override starts the seat without the
// dialog and writes nothing there (archon-m1yg).
func (v HarnessVariant) SeatLaunchCommand(cwd string) (string, error) {
	command, err := v.LaunchCommand()
	if err != nil || v.ID != "openai-codex" {
		return command, err
	}
	return command + " -c " + shellQuote("projects={"+renderString(cwd)+"={trust_level=\"trusted\"}}"), nil
}

// SpawnCommand is what `archon agent spawn` runs: the session command for the
// spawn's harness, model and effort, which Archon renders only for the
// harnesses it starts.
func (v HarnessVariant) SpawnCommand() (string, error) {
	if _, ok := launchableHarness(v.ID); !ok {
		return "", fmt.Errorf("Archon cannot start harness %q; use claude-code or openai-codex", v.ID)
	}
	return v.LaunchCommand()
}

func (v HarnessVariant) verifyTurnSettings(turn codexTranscriptTurn) error {
	// Claude records model on assistant messages but may omit effort. Codex
	// records both in turn_context, so missing evidence there is a mismatch.
	if v.Model != "" && turn.Model != v.Model || (v.ID == "openai-codex" || turn.Effort != "") && turn.Effort != v.effectiveEffort() {
		return fmt.Errorf("seat model/effort mismatch: expected %s/%s, observed %s/%s", v.Model, v.effectiveEffort(), turn.Model, turn.Effort)
	}
	return nil
}
