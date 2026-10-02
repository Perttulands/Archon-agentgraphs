package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Perttulands/Archon-agentgraphs/internal/buildinfo"
	"github.com/Perttulands/Archon-agentgraphs/internal/core"
	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

type tmuxRunner interface {
	LiveSessions() ([]formations.LiveAgentSession, error)
	Spawn(name, command string) error
	Attach(name string) error
}

type realTmuxRunner struct{}

type archonConfig struct {
	Workspace string
	Server    string
}

type archonBoardIdentity struct {
	ID    string `json:"id"`
	Slug  string `json:"slug"`
	Title string `json:"title"`
	Rev   int    `json:"rev"`
	ETag  string `json:"etag"`
}

type archonFormationListResponse struct {
	Board      archonBoardIdentity        `json:"mission"`
	Formations []formations.FormationNode `json:"formations"`
}

type archonFormationInspectResponse struct {
	Board       archonBoardIdentity          `json:"mission"`
	Formation   formations.FormationNode     `json:"formation"`
	Connections []formations.BoardConnection `json:"connections"`
}

type archonMissionInspectResponse struct {
	Board       archonBoardIdentity          `json:"mission"`
	Mission     formations.MissionNode       `json:"inputCard"`
	Chain       []archonMissionChainNode     `json:"chain"`
	Connections []formations.BoardConnection `json:"connections"`
}

type archonMissionChainNode struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Title string `json:"title"`
	Type  string `json:"type,omitempty"`
	Depth int    `json:"depth"`
}

type archonErrorResponse struct {
	Code     string                    `json:"code"`
	Message  string                    `json:"message"`
	Boundary string                    `json:"boundary"`
	Selector string                    `json:"selector"`
	Findings []formations.BoardFinding `json:"findings,omitempty"`
}

type archonRunAskResponse struct {
	RunID           string                          `json:"runId"`
	Question        string                          `json:"question,omitempty"`
	Status          *formations.RunStatusProjection `json:"status"`
	Answer          string                          `json:"answer"`
	CompletedNodes  []archonRunCompletedNode        `json:"completedNodes"`
	ProducedOutputs []archonRunProducedOutput       `json:"producedOutputs"`
	OpenEscalations []formations.OpenEscalation     `json:"openEscalations"`
	WaitingGates    []archonRunWaitingGate          `json:"waitingGates"`
	BlockedReasons  []archonRunBlockedReason        `json:"blockedReasons"`
	EvidenceSeqs    []int                           `json:"evidenceSeqs"`
	MissingEvidence []string                        `json:"missingEvidence,omitempty"`
}

type archonRunCompletedNode struct {
	NodeID    string `json:"nodeId"`
	Status    string `json:"status"`
	OutputSeq int    `json:"outputSeq"`
	ReportRef string `json:"reportRef,omitempty"`
	Text      string `json:"text,omitempty"`
}

type archonRunProducedOutput struct {
	NodeID    string `json:"nodeId"`
	OutputSeq int    `json:"outputSeq"`
	ReportRef string `json:"reportRef,omitempty"`
	Text      string `json:"text,omitempty"`
}

type archonRunWaitingGate struct {
	GateID       string   `json:"gateId"`
	NodeID       string   `json:"nodeId"`
	Prompt       string   `json:"prompt,omitempty"`
	Choices      []string `json:"choices,omitempty"`
	RequestedSeq int      `json:"requestedSeq"`
}

type archonRunBlockedReason struct {
	Seq           int    `json:"seq"`
	NodeID        string `json:"nodeId,omitempty"`
	GateID        string `json:"gateId,omitempty"`
	Reason        string `json:"reason"`
	Code          string `json:"code,omitempty"`
	Boundary      string `json:"boundary,omitempty"`
	ResumeAllowed bool   `json:"resumeAllowed"`
}

type archonStreamError struct {
	Type  string              `json:"type"`
	Error archonErrorResponse `json:"error"`
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, realTmuxRunner{}))
}

// archonNouns are the nouns the CLI knows, offline and with --server.
var archonNouns = map[string]bool{"mission": true, "formation": true, "gate": true, "end": true, "limit": true, "tool": true, "agent": true, "run": true, "peer": true}

func run(args []string, stdout, stderr io.Writer, runner tmuxRunner) int {
	if len(args) == 1 && (args[0] == "--version" || args[0] == "version") {
		fmt.Fprintln(stdout, buildinfo.String())
		return 0
	}
	config, args, ok := parseGlobalArgs(args, stderr)
	if !ok {
		return 2
	}
	if len(args) == 1 && (args[0] == "--version" || args[0] == "version") && config.Server != "" {
		return remoteVersion(config.Server, stdout, stderr)
	}
	// Help reads the same with or without a workspace or server.
	if len(args) == 0 || len(args) == 1 && isHelpArg(args[0]) {
		writeArchonHelp(stderr)
		return 2
	}
	if !archonNouns[args[0]] {
		if len(args) < 2 {
			writeArchonHelp(stderr)
			return 2
		}
		fmt.Fprintf(stderr, "unknown archon noun %q\n", args[0])
		return 2
	}
	if help, _ := helpForNoun(args[0]); len(args) == 1 || isHelpArg(args[1]) {
		writeNounHelp(stderr, help)
		return 2
	}
	if config.Server != "" {
		return runRemote(config.Server, args, stdout, stderr)
	}
	if config.Workspace == "" && needsWorkspace(args) {
		if args[0] == "peer" {
			// Peer commands work on the state directory itself, never through the daemon.
			fmt.Fprintf(stderr, "archon peer %s needs --workspace <state-dir>: there is no default workspace\n", args[1])
			return 2
		}
		fmt.Fprintf(stderr, "archon %s %s needs --workspace <state-dir> or --server <url>: there is no default workspace\n", args[0], args[1])
		return 2
	}
	switch args[0] {
	case "peer":
		return runPeerCommand(formations.NewStore(config.Workspace), args[1:], stdout, stderr)
	case "agent":
		dir := formations.AgentsDir(config.Workspace)
		if dir == "" {
			fmt.Fprintf(stderr, "archon agent %s needs --workspace <state-dir>, ARCHON_AGENTS_DIR or --server <url>: there is no default role directory\n", args[1])
			return 2
		}
		store := formations.NewPersonaStore(dir)
		switch args[1] {
		case "list":
			return runAgentList(store, args[2:], stdout, stderr, runner)
		case "inspect":
			return runAgentInspect(store, args[2:], stdout, stderr)
		case "new":
			return runAgentNew(store, args[2:], stdout, stderr)
		case "edit":
			return runAgentEdit(store, args[2:], stdout, stderr)
		case "spawn":
			return runAgentSpawn(store, args[2:], stdout, stderr, runner)
		case "attach":
			return runAgentAttach(store, args[2:], stdout, stderr, runner)
		case "retire":
			return runAgentRetire(store, formations.NewStore(config.Workspace), args[2:], stdout, stderr, false)
		case "restore":
			return runAgentRetire(store, formations.NewStore(config.Workspace), args[2:], stdout, stderr, true)
		case "delete":
			return runAgentDelete(store, formations.NewStore(config.Workspace), args[2:], stdout, stderr)
		default:
			return offlineUnavailable(stderr, "agent", args[1])
		}
	case "formation":
		store := formations.NewStore(config.Workspace)
		switch args[1] {
		case "create":
			return runFormationCreate(store, args[2:], stdout, stderr)
		case "list":
			return runFormationList(store, args[2:], stdout, stderr)
		case "inspect":
			return runFormationInspect(store, args[2:], stdout, stderr)
		case "assign":
			return runFormationAssign(store, args[2:], stdout, stderr)
		case "unassign":
			return runFormationUnassign(store, args[2:], stdout, stderr)
		case "set-brief":
			return runFormationSetBrief(store, args[2:], stdout, stderr)
		case "rename":
			return runFormationRename(store, args[2:], stdout, stderr)
		case "set-type":
			return runFormationSetType(store, args[2:], stdout, stderr)
		case "add-input":
			return runFormationAddPort(store, args[2:], stdout, stderr, formations.FormationPortInput)
		case "add-output":
			return runFormationAddPort(store, args[2:], stdout, stderr, formations.FormationPortOutput)
		case "wire":
			return runFormationWire(store, args[2:], stdout, stderr, false)
		case "unwire":
			return runFormationWire(store, args[2:], stdout, stderr, true)
		case "run":
			return runFormationRun(formations.NewStore(config.Workspace), args[2:], stdout, stderr)
		default:
			return offlineUnavailable(stderr, "formation", args[1])
		}
	case "gate":
		store := formations.NewStore(config.Workspace)
		switch args[1] {
		case "create":
			return runGateCreate(store, args[2:], stdout, stderr)
		case "update":
			return runGateUpdate(store, args[2:], stdout, stderr)
		case "judge":
			return runGateJudge(store, args[2:], stdout, stderr)
		case "approve":
			return runGateVerdict(formations.NewStore(config.Workspace), args[2:], stdout, stderr, "pass")
		case "reject":
			return runGateVerdict(formations.NewStore(config.Workspace), args[2:], stdout, stderr, "fail")
		default:
			return offlineUnavailable(stderr, "gate", args[1])
		}
	case "end":
		return runEndCommand(formations.NewStore(config.Workspace), args[1], args[2:], stdout, stderr)
	case "limit":
		return runLimitCommand(formations.NewStore(config.Workspace), args[1], args[2:], stdout, stderr)
	case "mission":
		store := formations.NewStore(config.Workspace)
		switch args[1] {
		case "new":
			return runBoardNew(store, args[2:], stdout, stderr)
		case "notes":
			return runBoardNotes(store, args[2:], stdout, stderr)
		case "note":
			return runBoardNote(store, args[2:], stdout, stderr)
		case "validate":
			return runBoardValidate(store, args[2:], stdout, stderr)
		case "arrange":
			return runBoardArrange(store, args[2:], stdout, stderr)
		case "create":
			return runMissionCreate(store, args[2:], stdout, stderr)
		case "list":
			return runMissionList(store, args[2:], stdout, stderr)
		case "inspect":
			return runMissionInspect(store, args[2:], stdout, stderr)
		case "wire":
			return runMissionWire(store, args[2:], stdout, stderr)
		case "update":
			return runMissionUpdate(store, args[2:], stdout, stderr)
		case "input":
			return runMissionInput(store, args[2:], stdout, stderr)
		case "run":
			return runMissionRun(formations.NewStore(config.Workspace), args[2:], stdout, stderr)
		default:
			return offlineUnavailable(stderr, "mission", args[1])
		}
	case "tool":
		store := formations.NewStore(config.Workspace)
		switch args[1] {
		case "create":
			return runToolCreate(store, args[2:], stdout, stderr)
		case "update":
			return runToolUpdate(store, args[2:], stdout, stderr)
		case "delete":
			return runToolDelete(store, args[2:], stdout, stderr)
		case "inspect":
			return runToolInspect(store, args[2:], stdout, stderr)
		default:
			return offlineUnavailable(stderr, "tool", args[1])
		}
	case "run":
		store := formations.NewStore(config.Workspace)
		switch args[1] {
		case "list":
			return runList(store, args[2:], stdout, stderr)
		case "status":
			return runStatus(store, args[2:], stdout, stderr)
		case "logs":
			return runLogs(store, args[2:], stdout, stderr)
		case "follow":
			return runFollow(store, args[2:], stdout, stderr)
		case "wait":
			return runWaitOffline(stderr)
		case "resume":
			return runResume(formations.NewStore(config.Workspace), args[2:], stdout, stderr)
		case "abort":
			return runAbort(formations.NewStore(config.Workspace), args[2:], stdout, stderr)
		case "ask":
			return runAsk(store, args[2:], stdout, stderr)
		default:
			return offlineUnavailable(stderr, "run", args[1])
		}
	default:
		fmt.Fprintf(stderr, "unknown archon noun %q\n", args[0])
		return 2
	}
}

func newArchonRunEngine(store *formations.Store, personas *formations.PersonaStore, boundary string) *formations.RunEngine {
	engine := formations.NewRunEngine(store, personas, formations.NewConfiguredFormationExecutorFromEnv(store, personas, boundary))
	engine.SetGateEvaluator(formations.NewCodeGateEvaluator())
	return engine
}

// warnUnreadable names each entry a listing skipped because it cannot be read.
func warnUnreadable(stderr io.Writer, noun string, unreadable []formations.Unreadable) {
	for _, entry := range unreadable {
		fmt.Fprintf(stderr, "warning: %s %s is not listed: %s\n", noun, entry.Name, entry.Reason)
	}
}

// warnFileRefs flags, once a write has saved them, the reference files that
// do not exist yet, as the cockpit flags their chips (archon-n7u.26).
func warnFileRefs(stderr io.Writer, files []string) {
	for _, ref := range files {
		if ref = strings.TrimSpace(ref); ref == "" {
			continue
		}
		if _, problem := formations.FileRefProblem(ref); problem != "" {
			fmt.Fprintf(stderr, "warning: file %s %s\n", ref, problem)
		}
	}
}

// needsWorkspace reports whether an offline command reads or writes a state
// directory. Agent cards live in their own directory, run wait answers that it
// needs the daemon, and help needs nothing.
func needsWorkspace(args []string) bool {
	if args[0] == "agent" || (args[0] == "run" && args[1] == "wait") {
		return false
	}
	for _, arg := range args[2:] {
		if arg == "-h" || arg == "--help" || arg == "-help" {
			return false
		}
	}
	return true
}

func parseGlobalArgs(args []string, stderr io.Writer) (archonConfig, []string, bool) {
	var config archonConfig
	for len(args) > 0 {
		switch args[0] {
		case "--workspace":
			if len(args) < 2 {
				fmt.Fprintln(stderr, "--workspace requires a path")
				return config, args, false
			}
			config.Workspace = args[1]
			args = args[2:]
		case "--server":
			if len(args) < 2 {
				fmt.Fprintln(stderr, "--server requires a loopback URL")
				return config, args, false
			}
			config.Server = args[1]
			args = args[2:]
		default:
			return config, args, true
		}
	}
	return config, args, true
}

func runAgentList(store *formations.PersonaStore, args []string, stdout, stderr io.Writer, runner tmuxRunner) int {
	fs := commandFlags("agent list", stderr)
	jsonOut := fs.Bool("json", false, "write JSON")
	capable := fs.String("capable", "", "filter by bare capability")
	assignable := fs.Bool("assignable", false, "show assignable agents only")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true, "assignable": true})); err != nil {
		return 2
	}
	cards, unreadable, err := store.ListPersonasSkipping()
	if err != nil {
		return fail(stderr, err)
	}
	warnUnreadable(stderr, "role card", unreadable)
	live, err := liveFromRunner(runner)
	if err != nil {
		return fail(stderr, err)
	}
	roster, err := formations.ProjectAgentRoster(cards, live, formations.AgentRosterFilter{
		Capable:        *capable,
		AssignableOnly: *assignable,
	})
	if err != nil {
		return fail(stderr, err)
	}
	archonExposeTmuxTargetSessions(&roster)
	return writeAgentList(stdout, roster, *jsonOut)
}

// writeAgentList prints an agent roster for agent list, offline and remote.
func writeAgentList(stdout io.Writer, roster formations.AgentRoster, jsonOut bool) int {
	if jsonOut {
		return writeJSON(stdout, roster)
	}
	for _, agent := range roster.Agents {
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", agent.ID, agent.Kind, agent.Liveness, strings.Join(agent.Tags, ","))
	}
	return 0
}

func runAgentInspect(store *formations.PersonaStore, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("agent inspect", stderr)
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, commandUsage("agent inspect"))
		return 2
	}
	card, err := store.ReadPersona(fs.Arg(0))
	if err != nil {
		return fail(stderr, err)
	}
	return writeAgentInspect(stdout, card, *jsonOut)
}

// writeAgentInspect prints one agent card for agent inspect, offline and remote.
func writeAgentInspect(stdout io.Writer, card *formations.PersonaCard, jsonOut bool) int {
	card.TOML = ""
	if jsonOut {
		return writeJSON(stdout, card)
	}
	fmt.Fprintf(stdout, "%s\t%s\t%s\n", card.ID, card.Kind, strings.Join(card.Tags, ","))
	return 0
}

func runAgentNew(store *formations.PersonaStore, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("agent new", stderr)
	f := newAgentNewFlags(fs)
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, commandUsage("agent new"))
		return 2
	}
	card, err := store.CreatePersona(formations.CreatePersonaRequest{
		ID:           fs.Arg(0),
		Kind:         *f.kind,
		Capabilities: splitCSV(*f.capable),
		Personality:  *f.personality,
	})
	if err != nil {
		return fail(stderr, err)
	}
	card.TOML = ""
	if *f.jsonOut {
		return writeJSON(stdout, card)
	}
	fmt.Fprintf(stdout, "created %s\n", card.ID)
	return 0
}

func runAgentEdit(store *formations.PersonaStore, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("agent edit", stderr)
	f := newAgentEditFlags(fs)
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, commandUsage("agent edit"))
		return 2
	}
	before, err := store.ReadPersona(fs.Arg(0))
	if err != nil {
		return fail(stderr, err)
	}
	edit := formations.EditPersonaRequest{
		AddCapability:    *f.addCapability,
		RemoveCapability: *f.removeCapability,
		Note:             *f.note,
		ExpectedETag:     before.ETag,
	}
	setFlags := givenFlags(fs)
	if setFlags["display-name"] {
		edit.SetDisplayName = f.displayName
	}
	if setFlags["kind"] {
		edit.SetKind = f.kind
	}
	if setFlags["summary"] {
		edit.SetSummary = f.summary
	}
	if setFlags["capable"] {
		capabilities := splitCSV(*f.capable)
		edit.SetCapabilities = &capabilities
	}
	card, err := store.EditPersona(fs.Arg(0), edit)
	if err != nil {
		return fail(stderr, err)
	}
	card.TOML = ""
	if *f.jsonOut {
		return writeJSON(stdout, card)
	}
	fmt.Fprintf(stdout, "updated %s\n", card.ID)
	return 0
}

func runAgentSpawn(store *formations.PersonaStore, args []string, stdout, stderr io.Writer, runner tmuxRunner) int {
	fs := commandFlags("agent spawn", stderr)
	harness := fs.String("harness", "", "harness the session runs: "+strings.Join(formations.LaunchableHarnessIDs(), " or "))
	model := fs.String("model", "", "model the session runs; blank means the harness default model")
	effort := fs.String("effort", "", "effort the session runs at; the policy is "+formations.EffortPolicyText())
	if err := fs.Parse(reorderFlags(args, nil)); err != nil {
		return 2
	}
	if fs.NArg() != 1 || *harness == "" || *effort == "" {
		fmt.Fprintln(stderr, commandUsage("agent spawn"))
		return 2
	}
	card, err := store.ReadPersona(fs.Arg(0))
	if err != nil {
		return fail(stderr, err)
	}
	variant := formations.HarnessVariant{ID: strings.TrimSpace(*harness), SessionStem: card.ID, Model: strings.TrimSpace(*model), Effort: strings.TrimSpace(*effort)}
	if err := formations.ValidateSpawnSettings(card.ID, variant.ID, variant.Model, variant.Effort); err != nil {
		return fail(stderr, err)
	}
	live, err := liveFromRunner(runner)
	if err != nil {
		return fail(stderr, err)
	}
	if binding, err := formations.ResolveAgentSession(*card, live); err == nil {
		// A running session keeps what it started with, so a spawn changes none of it, whatever it states.
		stated := []string{"--harness " + variant.ID}
		if variant.Model != "" {
			stated = append(stated, "--model "+variant.Model)
		}
		stated = append(stated, "--effort "+variant.Effort)
		fmt.Fprintf(stderr, "%s is already running as %s with the harness, model and effort it started with; this spawn (%s) changes none of them. Stop that session to spawn %s again.\n",
			card.ID, archonTmuxTargetSessionName(binding.SessionStem), strings.Join(stated, " "), card.ID)
		return 1
	} else if !errors.Is(err, formations.ErrAgentSessionOffline) {
		return fail(stderr, err)
	}
	targetSession := archonTmuxTargetSessionName(variant.SessionStem)
	command, err := variant.SpawnCommand()
	if err != nil {
		return fail(stderr, err)
	}
	if err := runner.Spawn(targetSession, command); err != nil {
		return fail(stderr, err)
	}
	fmt.Fprintf(stdout, "spawned %s as %s\n", card.ID, targetSession)
	return 0
}

func runAgentAttach(store *formations.PersonaStore, args []string, stdout, stderr io.Writer, runner tmuxRunner) int {
	fs := commandFlags("agent attach", stderr)
	if err := fs.Parse(reorderFlags(args, nil)); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, commandUsage("agent attach"))
		return 2
	}
	card, err := store.ReadPersona(fs.Arg(0))
	if err != nil {
		return fail(stderr, err)
	}
	live, err := liveFromRunner(runner)
	if err != nil {
		return fail(stderr, err)
	}
	binding, err := formations.ResolveAgentSession(*card, live)
	if err != nil {
		return fail(stderr, err)
	}
	if err := runner.Attach(archonTmuxTargetSessionName(binding.SessionStem)); err != nil {
		return fail(stderr, err)
	}
	return 0
}

// runAgentRetire retires a role, or with restore brings it back, and names
// the slots that use it: a slot staffed with a retired role does not run.
func runAgentRetire(store *formations.PersonaStore, missions *formations.Store, args []string, stdout, stderr io.Writer, restore bool) int {
	verb := "retire"
	if restore {
		verb = "restore"
	}
	fs := commandFlags("agent "+verb, stderr)
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, commandUsage("agent "+verb))
		return 2
	}
	before, err := store.ReadPersona(fs.Arg(0))
	if err != nil {
		return fail(stderr, err)
	}
	uses, err := missions.RoleUsage(before.ID)
	if err != nil {
		return fail(stderr, err)
	}
	retired := !restore
	card, err := store.EditPersona(before.ID, formations.EditPersonaRequest{SetRetired: &retired, ExpectedETag: before.ETag})
	if err != nil {
		return fail(stderr, err)
	}
	return writeRoleRetired(stdout, card.ID, retired, uses, *jsonOut)
}

// writeRoleRetired says what retiring or restoring a role did, offline and
// remote.
func writeRoleRetired(stdout io.Writer, id string, retired bool, uses []formations.RoleUse, jsonOut bool) int {
	status := "active"
	if retired {
		status = "retired"
	}
	if jsonOut {
		return writeJSON(stdout, map[string]any{"id": id, "status": status, "usage": uses})
	}
	if !retired {
		fmt.Fprintf(stdout, "restored %s\n", id)
		return 0
	}
	fmt.Fprintf(stdout, "retired %s\n", id)
	if len(uses) > 0 {
		fmt.Fprintf(stdout, "these slots do not run until they are restaffed: %s\n", formations.RoleUsageWords(uses))
	}
	return 0
}

// runAgentDelete deletes a role's card once no slot names it.
func runAgentDelete(store *formations.PersonaStore, missions *formations.Store, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("agent delete", stderr)
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, commandUsage("agent delete"))
		return 2
	}
	id := fs.Arg(0)
	uses, err := missions.RoleUsage(id)
	if err != nil {
		return failJSON(stderr, err, *jsonOut, "agent", id)
	}
	if len(uses) > 0 {
		return failJSON(stderr, formations.RoleInUseError(id, uses), *jsonOut, "agent", id)
	}
	card, err := store.ReadPersona(id)
	if err != nil {
		return failJSON(stderr, err, *jsonOut, "agent", id)
	}
	builtin, err := store.DeletePersona(id, card.ETag)
	if err != nil {
		return failJSON(stderr, err, *jsonOut, "agent", id)
	}
	return writeRoleDeleted(stdout, id, builtin, *jsonOut)
}

func writeRoleDeleted(stdout io.Writer, id string, builtinRemains, jsonOut bool) int {
	if jsonOut {
		return writeJSON(stdout, map[string]any{"deleted": id, "builtinRemains": builtinRemains})
	}
	if builtinRemains {
		fmt.Fprintf(stdout, "deleted the card for %s; the built-in role %s remains\n", id, id)
		return 0
	}
	fmt.Fprintf(stdout, "deleted %s\n", id)
	return 0
}

func runFormationCreate(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("formation create", stderr)
	title := fs.String("title", "", "formation title")
	x := fs.Int("x", 120, "layout x")
	y := fs.Int("y", 120, "layout y")
	updatedBy := fs.String("updated-by", "agent:archon", "update actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() < 1 || fs.NArg() > 2 {
		fmt.Fprintln(stderr, commandUsage("formation create"))
		return 2
	}
	slug, err := store.ResolveBoardSelector(fs.Arg(0))
	if err != nil {
		return failSelector(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	board, err := store.ReadBoard(slug)
	if err != nil {
		return failJSON(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	createX, createY, err := resolveCreateCoordinates(store, slug, fs, *x, *y)
	if err != nil {
		return failDefinitionWrite(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	result, err := store.CreateFormation(slug, formations.FormationCreateRequest{
		Type:      fs.Arg(1), // blank creates a solo formation
		Title:     *title,
		X:         createX,
		Y:         createY,
		UpdatedBy: *updatedBy,
	}, formations.WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev})
	if err != nil {
		return failDefinitionWrite(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	return writeCreated(stdout, *jsonOut, result, result.Board, result.Layout, result.Formation.ID)
}

// writeCreated prints a create result: {board, layout, <node>} without TOML, or
// the created node's ID.
func writeCreated(stdout io.Writer, jsonOut bool, result any, board *formations.BoardDocument, layout *formations.LayoutDocument, id string) int {
	board.TOML = ""
	layout.TOML = ""
	if jsonOut {
		return writeJSON(stdout, result)
	}
	fmt.Fprintf(stdout, "created %s\n", id)
	return 0
}

func resolveCreateCoordinates(store *formations.Store, slug string, fs *flag.FlagSet, x, y int) (int, int, error) {
	explicit := false
	fs.Visit(func(current *flag.Flag) {
		if current.Name == "x" || current.Name == "y" {
			explicit = true
		}
	})
	if explicit {
		return x, y, nil
	}
	position, err := store.FindFreeLayoutPosition(slug, x, y)
	if err != nil {
		return 0, 0, err
	}
	return position.X, position.Y, nil
}

func runFormationAssign(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("formation assign", stderr)
	f := newSlotAssignFlags(fs, stderr)
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	role, ok := f.resolve(fs, stderr)
	if !ok {
		return 2
	}
	slug, board, formationID, err := resolveFormationCommandTarget(store, fs.Arg(0), fs.Arg(1))
	if err != nil {
		return failSelector(stderr, err, *f.jsonOut, "formation", fs.Arg(1))
	}
	result, err := store.AssignFormationSlot(slug, f.request(formationID, role), formations.WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev})
	if err != nil {
		return failDefinitionWrite(stderr, err, *f.jsonOut, "formation", fs.Arg(1))
	}
	result.TOML = ""
	printWarnings(stderr, formations.SlotWarnings(result, formationID, *f.slot))
	if *f.jsonOut {
		return writeJSON(stdout, result)
	}
	fmt.Fprintln(stdout, assignedText(result, formationID, *f.slot))
	return 0
}

func runFormationUnassign(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("formation unassign", stderr)
	slotID := fs.String("slot", "", "slot id")
	updatedBy := fs.String("updated-by", "agent:archon", "update actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 2 || *slotID == "" {
		fmt.Fprintln(stderr, commandUsage("formation unassign"))
		return 2
	}
	slug, board, formationID, err := resolveFormationCommandTarget(store, fs.Arg(0), fs.Arg(1))
	if err != nil {
		return failSelector(stderr, err, *jsonOut, "formation", fs.Arg(1))
	}
	result, err := store.AssignFormationSlot(slug, formations.FormationSlotAssignmentRequest{
		FormationID: formationID,
		SlotID:      *slotID,
		UpdatedBy:   *updatedBy,
	}, formations.WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev})
	if err != nil {
		return failDefinitionWrite(stderr, err, *jsonOut, "formation", fs.Arg(1))
	}
	result.TOML = ""
	if *jsonOut {
		return writeJSON(stdout, result)
	}
	fmt.Fprintf(stdout, "unassigned %s from %s\n", *slotID, formationID)
	return 0
}

func runFormationRename(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("formation rename", stderr)
	updatedBy := fs.String("updated-by", "agent:archon", "update actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 3 {
		fmt.Fprintln(stderr, commandUsage("formation rename"))
		fmt.Fprintln(stderr, "An empty title clears it. The ID, ports, slots, brief, edges, layout and notes stay unchanged.")
		return 2
	}
	slug, board, formationID, err := resolveFormationCommandTarget(store, fs.Arg(0), fs.Arg(1))
	if err != nil {
		return failSelector(stderr, err, *jsonOut, "formation", fs.Arg(1))
	}
	title := fs.Arg(2)
	result, err := store.UpdateFormation(slug, formations.FormationUpdateRequest{
		FormationID: formationID,
		Title:       &title,
		UpdatedBy:   *updatedBy,
	}, formations.WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev})
	if err != nil {
		return failDefinitionWrite(stderr, err, *jsonOut, "formation", fs.Arg(1))
	}
	result.TOML = ""
	if *jsonOut {
		return writeJSON(stdout, result)
	}
	fmt.Fprintf(stdout, "renamed %s\n", formationID)
	return 0
}

func runFormationSetType(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("formation set-type", stderr)
	keepSlot := fs.String("keep-slot", "", "slot id or label to keep when changing to solo")
	updatedBy := fs.String("updated-by", "agent:archon", "update actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 3 {
		fmt.Fprintln(stderr, commandUsage("formation set-type"))
		fmt.Fprintln(stderr, "solo keeps one slot, peer has at least two, orchestrated has one controller and a worker; added slots are empty. Changing to solo with several staffed slots needs --keep-slot.")
		return 2
	}
	slug, board, formationID, err := resolveFormationCommandTarget(store, fs.Arg(0), fs.Arg(1))
	if err != nil {
		return failSelector(stderr, err, *jsonOut, "formation", fs.Arg(1))
	}
	keepSlotID := *keepSlot
	if keepSlotID != "" {
		var candidates []graphSelectorCandidate
		for _, formation := range board.Formations {
			if formation.ID != formationID {
				continue
			}
			for _, slot := range formation.Slots {
				candidates = append(candidates, graphSelectorCandidate{ID: slot.ID, Title: slot.Label})
			}
		}
		if keepSlotID, err = resolveGraphSelector("slot", keepSlotID, candidates); err != nil {
			return failSelector(stderr, err, *jsonOut, "slot", *keepSlot)
		}
	}
	result, err := store.SetFormationType(slug, formations.FormationTypeRequest{
		FormationID: formationID,
		Type:        fs.Arg(2),
		KeepSlotID:  keepSlotID,
		UpdatedBy:   *updatedBy,
	}, formations.WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev})
	if err != nil {
		return failJSON(stderr, err, *jsonOut, "formation", fs.Arg(1))
	}
	result.TOML = ""
	if *jsonOut {
		return writeJSON(stdout, result)
	}
	fmt.Fprintf(stdout, "%s is now %s\n", formationID, fs.Arg(2))
	return 0
}

func runFormationSetBrief(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("formation set-brief", stderr)
	goal := fs.String("goal", "", "brief goal")
	beadID := fs.String("bead", "", "project Beads id")
	updatedBy := fs.String("updated-by", "agent:archon", "update actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	var files stringList
	var links stringList
	fs.Var(&files, "file", "file reference")
	fs.Var(&links, "link", "link reference")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(stderr, commandUsage("formation set-brief"))
		return 2
	}
	slug, board, formationID, err := resolveFormationCommandTarget(store, fs.Arg(0), fs.Arg(1))
	if err != nil {
		return failSelector(stderr, err, *jsonOut, "formation", fs.Arg(1))
	}
	result, err := store.SetFormationBrief(slug, formations.FormationBriefRequest{
		FormationID: formationID,
		Goal:        *goal,
		BeadID:      *beadID,
		Files:       files,
		Links:       links,
		UpdatedBy:   *updatedBy,
	}, formations.WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev})
	if err != nil {
		return failDefinitionWrite(stderr, err, *jsonOut, "formation", fs.Arg(1))
	}
	warnFileRefs(stderr, files)
	result.TOML = ""
	if *jsonOut {
		return writeJSON(stdout, result)
	}
	fmt.Fprintf(stdout, "updated brief for %s\n", formationID)
	return 0
}

func runFormationAddPort(store *formations.Store, args []string, stdout, stderr io.Writer, direction string) int {
	name := "formation add-input"
	if direction == formations.FormationPortOutput {
		name = "formation add-output"
	}
	fs := commandFlags(name, stderr)
	label := fs.String("label", "", "port label")
	updatedBy := fs.String("updated-by", "agent:archon", "update actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(stderr, commandUsage(name))
		return 2
	}
	slug, board, formationID, err := resolveFormationCommandTarget(store, fs.Arg(0), fs.Arg(1))
	if err != nil {
		return failSelector(stderr, err, *jsonOut, "formation", fs.Arg(1))
	}
	result, err := store.AddFormationPort(slug, formations.FormationPortRequest{
		FormationID: formationID,
		Direction:   direction,
		Label:       *label,
		UpdatedBy:   *updatedBy,
	}, formations.WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev})
	if err != nil {
		return failDefinitionWrite(stderr, err, *jsonOut, "formation", fs.Arg(1))
	}
	result.TOML = ""
	if *jsonOut {
		return writeJSON(stdout, result)
	}
	fmt.Fprintf(stdout, "added %s to %s\n", direction, formationID)
	return 0
}

func runFormationWire(store *formations.Store, args []string, stdout, stderr io.Writer, remove bool) int {
	name := "formation wire"
	if remove {
		name = "formation unwire"
	}
	fs := commandFlags(name, stderr)
	updatedBy := fs.String("updated-by", "agent:archon", "update actor")
	join := new(bool)
	if !remove {
		join = fs.Bool("join", false, "join an occupied formation input by adding a new input port atomically")
	}
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true, "join": true})); err != nil {
		return 2
	}
	if fs.NArg() != 3 {
		fmt.Fprintln(stderr, commandUsage(name))
		return 2
	}
	slug, err := store.ResolveBoardSelector(fs.Arg(0))
	if err != nil {
		return failSelector(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	board, err := store.ReadBoard(slug)
	if err != nil {
		return failDefinitionWrite(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	request := formations.FormationWireRequest{
		JoinIfOccupied: *join,
		From:           fs.Arg(1),
		To:             fs.Arg(2),
		UpdatedBy:      *updatedBy,
	}
	var result *formations.BoardDocument
	if remove {
		result, err = store.UnwireFormationPorts(slug, request, formations.WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev})
	} else {
		result, err = store.WireFormationPorts(slug, request, formations.WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev})
	}
	if err != nil {
		return failDefinitionWrite(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	result.TOML = ""
	if *jsonOut {
		return writeJSON(stdout, result)
	}
	if remove {
		fmt.Fprintf(stdout, "removed connection %s -> %s\n", request.From, request.To)
	} else {
		fmt.Fprintf(stdout, "wired %s -> %s\n", request.From, request.To)
	}
	return 0
}

func runGateCreate(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("gate create", stderr)
	title := fs.String("title", "Review gate", "gate title")
	kinds := fs.String("kinds", "", "comma-separated gate kinds: code, formation, human (default human)")
	criterion := fs.String("criterion", "", "gate criterion")
	check := fs.String("check", "", "registered code Gate profile id")
	checkVersion := fs.String("check-version", "", "exact code Gate profile version")
	checkValue := fs.String("check-value", "", "code Gate profile value parameter")
	var files stringList
	fs.Var(&files, "file", "reference file path; repeat for more")
	x := fs.Int("x", 0, "layout x coordinate")
	y := fs.Int("y", 0, "layout y coordinate")
	updatedBy := fs.String("updated-by", "agent:archon", "update actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, commandUsage("gate create"))
		return 2
	}
	slug, err := store.ResolveBoardSelector(fs.Arg(0))
	if err != nil {
		return failSelector(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	board, err := store.ReadBoard(slug)
	if err != nil {
		return failJSON(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	createX, createY, err := resolveCreateCoordinates(store, slug, fs, *x, *y)
	if err != nil {
		return failDefinitionWrite(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	result, err := store.CreateGate(slug, formations.GateCreateRequest{
		Title:        *title,
		Kinds:        splitCSV(*kinds),
		Criterion:    *criterion,
		Check:        *check,
		CheckVersion: *checkVersion,
		CheckValue:   *checkValue,
		Files:        files,
		X:            createX,
		Y:            createY,
		UpdatedBy:    *updatedBy,
	}, formations.WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev})
	if err != nil {
		return failDefinitionWrite(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	warnFileRefs(stderr, files)
	return writeCreated(stdout, *jsonOut, result, result.Board, result.Layout, result.Gate.ID)
}

func runGateUpdate(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("gate update", stderr)
	title := fs.String("title", "", "gate title")
	kinds := fs.String("kinds", "", "comma-separated gate kinds")
	criterion := fs.String("criterion", "", "gate criterion")
	check := fs.String("check", "", "registered code Gate profile id")
	checkVersion := fs.String("check-version", "", "exact code Gate profile version")
	checkValue := fs.String("check-value", "", "code Gate profile value parameter")
	clearCheck := fs.Bool("clear-check", false, "clear the code check profile, version and value")
	var files stringList
	fs.Var(&files, "file", "reference file path, replacing the current ones; repeat for more, or give an empty value to clear")
	updatedBy := fs.String("updated-by", "agent:archon", "update actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true, "clear-check": true})); err != nil {
		return 2
	}
	// Only flags given on the command line change the gate; an empty value clears.
	given := map[string]bool{}
	fs.Visit(func(current *flag.Flag) { given[current.Name] = true })
	if fs.NArg() != 2 || *clearCheck && (given["check"] || given["check-version"] || given["check-value"]) {
		fmt.Fprintln(stderr, commandUsage("gate update"))
		fmt.Fprintln(stderr, "Only the flags you give change the gate; an empty value clears that field. Dropping formation detaches the judge chain and dropping code clears the check.")
		return 2
	}
	slug, err := store.ResolveBoardSelector(fs.Arg(0))
	if err != nil {
		return failSelector(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	board, err := store.ReadBoard(slug)
	if err != nil {
		return failJSON(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	gateID, err := resolveGateSelector(board, fs.Arg(1))
	if err != nil {
		return failSelector(stderr, err, *jsonOut, "gate", fs.Arg(1))
	}
	update := formations.GateUpdateRequest{
		GateID:    gateID,
		UpdatedBy: *updatedBy,
	}
	if given["kinds"] {
		update.Kinds = append([]string{}, splitCSV(*kinds)...)
	}
	for name, field := range map[string]struct {
		value  *string
		target **string
	}{
		"title":         {title, &update.Title},
		"criterion":     {criterion, &update.Criterion},
		"check":         {check, &update.Check},
		"check-version": {checkVersion, &update.CheckVersion},
		"check-value":   {checkValue, &update.CheckValue},
	} {
		if given[name] {
			*field.target = field.value
		}
	}
	if *clearCheck {
		blank := ""
		update.Check, update.CheckVersion, update.CheckValue = &blank, &blank, &blank
	}
	if given["file"] {
		refs := []string(files)
		update.Files = &refs
	}
	result, err := store.UpdateGate(slug, update, formations.WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev})
	if err != nil {
		return failDefinitionWrite(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	warnFileRefs(stderr, files)
	result.TOML = ""
	if *jsonOut {
		return writeJSON(stdout, result)
	}
	fmt.Fprintln(stdout, "updated gate")
	return 0
}

func runGateJudge(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("gate judge", stderr)
	chain := fs.String("chain", "", "comma-separated formation chain")
	detach := fs.Bool("detach", false, "detach judge")
	updatedBy := fs.String("updated-by", "agent:archon", "update actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true, "detach": true})); err != nil {
		return 2
	}
	if fs.NArg() != 2 || (!*detach && *chain == "") {
		fmt.Fprintln(stderr, commandUsage("gate judge"))
		return 2
	}
	slug, err := store.ResolveBoardSelector(fs.Arg(0))
	if err != nil {
		return failSelector(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	board, err := store.ReadBoard(slug)
	if err != nil {
		return failDefinitionWrite(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	gateID, err := resolveGateSelector(board, fs.Arg(1))
	if err != nil {
		return failSelector(stderr, err, *jsonOut, "gate", fs.Arg(1))
	}
	request := formations.GateJudgeRequest{
		GateID:    gateID,
		Chain:     splitCSV(*chain),
		UpdatedBy: *updatedBy,
	}
	var result *formations.BoardDocument
	if *detach {
		result, err = store.DetachGateJudge(slug, request, formations.WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev})
	} else {
		result, err = store.SetGateJudgeChain(slug, request, formations.WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev})
	}
	if err != nil {
		return failDefinitionWrite(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	result.TOML = ""
	if *jsonOut {
		return writeJSON(stdout, result)
	}
	if *detach {
		fmt.Fprintf(stdout, "detached judge from %s\n", gateID)
	} else {
		fmt.Fprintf(stdout, "updated judge for %s\n", gateID)
	}
	return 0
}

// grantUsage describes run resume --grant, offline and remote (archon-o7p.8).
const grantUsage = "give the step a spent Limit card stopped one more allowance (one more round, or the card's time or tokens again); the ledger records the grant and who gave it"

// relayedByUsage describes gate approve|reject --relayed-by, offline and remote.
const relayedByUsage = "slot ID of the seat that typed the operator's confirmed decision; the decider stays human:operator"

func runGateVerdict(store *formations.Store, args []string, stdout, stderr io.Writer, verdict string) int {
	name := "gate approve"
	if verdict == "fail" {
		name = "gate reject"
	}
	fs := commandFlags(name, stderr)
	reason := fs.String("response", "", "the response: approve delivers it downstream with the gate input, reject sends it back as feedback")
	actor := fs.String("actor", "human:operator", "deciding actor")
	relayedBy := fs.String("relayed-by", "", relayedByUsage)
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(stderr, commandUsage("gate approve"))
		return 2
	}
	// Offline, this command is the run's worker, so no daemon may own the
	// state meanwhile: its worker would route the same verdict.
	release, err := holdStateLock(store.Workspace)
	if err != nil {
		return failJSON(stderr, err, *jsonOut, "run", fs.Arg(0))
	}
	defer release()
	personas := formations.NewPersonaStore(formations.AgentsDir(store.Workspace))
	engine := newArchonRunEngine(store, personas, "archon")
	if _, err := engine.RecordHumanGateVerdict(fs.Arg(0), formations.HumanGateVerdictRequest{
		GateID:    fs.Arg(1),
		Verdict:   verdict,
		Reason:    *reason,
		Actor:     *actor,
		RelayedBy: *relayedBy,
	}); err != nil {
		return failJSON(stderr, err, *jsonOut, "run", fs.Arg(0))
	}
	// With no daemon this command is the run's worker: it routes the verdict
	// and runs what follows, as the daemon does after a verdict.
	status, err := engine.ContinueRun(fs.Arg(0))
	if err != nil {
		return failJSON(stderr, err, *jsonOut, "run", fs.Arg(0))
	}
	if *jsonOut {
		return writeJSON(stdout, status)
	}
	fmt.Fprintf(stdout, "%s\t%s\n", status.RunID, status.Status)
	return 0
}

// errDaemonOwnsState refuses an offline command a running daemon would race.
var errDaemonOwnsState = errors.New("a daemon owns this state directory: run this through it with --server")

// holdStateLock takes the state directory's coordinator lock, as archond does,
// for an offline command that starts, continues or stops a run: mission run,
// formation run, run resume, run abort and gate approve or reject. It fails
// while a daemon holds the lock.
func holdStateLock(workspace string) (func(), error) {
	lock, err := os.OpenFile(filepath.Join(workspace, "coordinator.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return nil, errDaemonOwnsState
	}
	return func() {
		_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		lock.Close()
	}, nil
}

const humanChannelUsage = "how human gates reach the operator: notify (the default) or session"

// missionUpdateGiven reports whether a mission update names any field to change.
func missionUpdateGiven(given map[string]bool) bool {
	for _, name := range []string{"title", "goal", "file", "input-hint", "human-channel"} {
		if given[name] {
			return true
		}
	}
	return false
}

func runMissionCreate(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("mission create", stderr)
	title := fs.String("title", "", "mission title")
	goal := fs.String("goal", "", "mission goal")
	var files stringList
	fs.Var(&files, "file", "reference file path; repeat for more")
	humanChannel := fs.String("human-channel", "", humanChannelUsage)
	x := fs.Int("x", 0, "layout x coordinate")
	y := fs.Int("y", 0, "layout y coordinate")
	updatedBy := fs.String("updated-by", "agent:archon", "update actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, commandUsage("mission create"))
		return 2
	}
	slug, err := store.ResolveBoardSelector(fs.Arg(0))
	if err != nil {
		return failSelector(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	board, err := store.ReadBoard(slug)
	if err != nil {
		return failJSON(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	if err := secondInputCard(board, fs.Arg(0)); err != nil {
		return failJSON(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	createX, createY, err := resolveCreateCoordinates(store, slug, fs, *x, *y)
	if err != nil {
		return failDefinitionWrite(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	result, err := store.CreateMission(slug, formations.MissionCreateRequest{
		Title:        *title,
		Goal:         *goal,
		Files:        files,
		HumanChannel: *humanChannel,
		X:            createX,
		Y:            createY,
		UpdatedBy:    *updatedBy,
	}, formations.WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev})
	if err != nil {
		return failDefinitionWrite(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	warnFileRefs(stderr, files)
	return writeCreated(stdout, *jsonOut, result, result.Board, result.Layout, result.Mission.ID)
}

func runMissionList(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("mission list", stderr)
	fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() == 0 {
		return runBoardList(store, args, stdout, stderr)
	}
	fmt.Fprintln(stderr, commandUsage("mission list"))
	return 2
}

func runMissionInspect(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("mission inspect", stderr)
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() == 1 {
		return runBoardInspect(store, args, stdout, stderr)
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(stderr, commandUsage("mission inspect"))
		return 2
	}
	slug, err := store.ResolveBoardSelector(fs.Arg(0))
	if err != nil {
		return failSelector(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	board, err := store.ReadBoard(slug)
	if err != nil {
		return failJSON(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	return writeMissionInspect(stdout, stderr, board, fs.Arg(1), *jsonOut)
}

// writeMissionInspect resolves a mission on a read board and prints it with its
// reachable chain, for mission inspect offline and remote.
func writeMissionInspect(stdout, stderr io.Writer, board *formations.BoardDocument, selector string, jsonOut bool) int {
	missionID, err := resolveMissionSelector(board, selector)
	if err != nil {
		return failSelector(stderr, err, jsonOut, "inputCard", selector)
	}
	mission, ok := missionByID(board, missionID)
	if !ok {
		return failSelector(stderr, fmt.Errorf("%w: mission %q", formations.ErrNotFound, missionID), jsonOut, "inputCard", missionID)
	}
	chain, connections, err := missionReachableChain(board, missionID)
	if err != nil {
		return fail(stderr, err)
	}
	response := archonMissionInspectResponse{
		Board:       identityFromBoard(board),
		Mission:     mission,
		Chain:       chain,
		Connections: connections,
	}
	if jsonOut {
		return writeJSON(stdout, response)
	}
	fmt.Fprintf(stdout, "%s\t%s\t%d reachable nodes\n", mission.ID, mission.Title, len(chain))
	for _, input := range formations.MissionRunInputs(board) {
		fmt.Fprintln(stdout, "input\t"+missionInputLine(input))
	}
	return 0
}

func runMissionWire(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("mission wire", stderr)
	updatedBy := fs.String("updated-by", "agent:archon", "update actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 2 && fs.NArg() != 3 {
		fmt.Fprintln(stderr, commandUsage("mission wire"))
		return 2
	}
	slug, err := store.ResolveBoardSelector(fs.Arg(0))
	if err != nil {
		return failSelector(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	board, err := store.ReadBoard(slug)
	if err != nil {
		return failJSON(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	missionID, rest, err := inputCardArgs(board, fs.Arg(0), fs.Args()[1:], 2)
	if err != nil {
		return failSelector(stderr, err, *jsonOut, "inputCard", fs.Arg(1))
	}
	target := rest[0]
	result, err := store.WireFormationPorts(slug, formations.FormationWireRequest{
		From:      missionID + ":out",
		To:        target,
		UpdatedBy: *updatedBy,
	}, formations.WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev})
	if err != nil {
		return failDefinitionWrite(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	result.TOML = ""
	if *jsonOut {
		return writeJSON(stdout, result)
	}
	fmt.Fprintf(stdout, "wired Input card %s -> %s\n", missionID, target)
	return 0
}

func runMissionUpdate(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("mission update", stderr)
	title := fs.String("title", "", "mission title")
	goal := fs.String("goal", "", "mission goal")
	var files stringList
	fs.Var(&files, "file", "reference file path, replacing the current ones; repeat for more, or give an empty value to clear")
	inputHint := fs.String("input-hint", "", "what a run brief for this mission should contain")
	humanChannel := fs.String("human-channel", "", humanChannelUsage)
	updatedBy := fs.String("updated-by", "agent:archon", "update actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	given := map[string]bool{}
	fs.Visit(func(current *flag.Flag) { given[current.Name] = true })
	if (fs.NArg() != 1 && fs.NArg() != 2) || !missionUpdateGiven(given) {
		fmt.Fprintln(stderr, commandUsage("mission update"))
		return 2
	}
	slug, err := store.ResolveBoardSelector(fs.Arg(0))
	if err != nil {
		return failSelector(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	board, err := store.ReadBoard(slug)
	if err != nil {
		return failJSON(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	missionID, _, err := inputCardArgs(board, fs.Arg(0), fs.Args()[1:], 1)
	if err != nil {
		return failSelector(stderr, err, *jsonOut, "inputCard", fs.Arg(1))
	}
	update := formations.MissionUpdateRequest{MissionID: missionID, UpdatedBy: *updatedBy}
	if given["title"] {
		update.Title = title
	}
	if given["goal"] {
		update.Goal = goal
	}
	if given["file"] {
		refs := []string(files)
		update.Files = &refs
	}
	if given["input-hint"] {
		update.InputHint = inputHint
	}
	if given["human-channel"] {
		update.HumanChannel = humanChannel
	}
	result, err := store.UpdateMission(slug, update, formations.WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev})
	if err != nil {
		return failDefinitionWrite(stderr, err, *jsonOut, "inputCard", fs.Arg(1))
	}
	warnFileRefs(stderr, files)
	result.TOML = ""
	if *jsonOut {
		return writeJSON(stdout, result)
	}
	fmt.Fprintf(stdout, "updated Input card %s\n", missionID)
	return 0
}

// runStartFlags are the run fields a mission run and a single step's run
// both take (archon-o7p.3).
type runStartFlags struct {
	cwd          *string
	bead         *string
	contextPaths stringList
	inputs       runInputFlags
}

func registerRunStartFlags(fs *flag.FlagSet) *runStartFlags {
	flags := &runStartFlags{}
	flags.cwd = fs.String("cwd", "", "absolute existing directory the agents work in; omit to create a workspace for the run")
	flags.bead = fs.String("bead", "", "the Beads issue the run belongs to")
	fs.Var(&flags.contextPaths, "context-path", "absolute file or directory the agents inspect as context; repeat for more")
	flags.inputs.register(fs)
	return flags
}

// checkCwd refuses a cwd that is not an absolute existing directory, as the
// daemon does.
func (f *runStartFlags) checkCwd() error {
	if *f.cwd == "" {
		return nil
	}
	info, err := os.Stat(*f.cwd)
	if !filepath.IsAbs(*f.cwd) || err != nil || !info.IsDir() {
		return fmt.Errorf("--cwd must be omitted or an absolute existing directory")
	}
	return nil
}

func runMissionRun(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("mission run", stderr)
	actor := fs.String("actor", "agent:archon", "run actor")
	run := registerRunStartFlags(fs)
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, commandUsage("mission run"))
		return 2
	}
	inputs, err := run.inputs.collect()
	if err == nil {
		err = run.checkCwd()
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	slug, err := store.ResolveBoardSelector(fs.Arg(0))
	if err != nil {
		return failSelector(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	board, err := store.ReadBoard(slug)
	if err != nil {
		return failJSON(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	missionID, err := runInputCard(board, slug)
	if err != nil {
		return failJSON(stderr, err, *jsonOut, "run", "")
	}
	personas := formations.NewPersonaStore(formations.AgentsDir(store.Workspace))
	if err := formations.CheckRunAdmission(board, personas, formations.RunAdmissionScope{MissionID: missionID, Inputs: inputs}); err != nil {
		return failJSON(stderr, err, *jsonOut, "run", missionID)
	}
	release, err := holdStateLock(store.Workspace)
	if err != nil {
		return failJSON(stderr, err, *jsonOut, "run", missionID)
	}
	defer release()
	engine := newArchonRunEngine(store, personas, "archon")
	status, err := engine.RunMission(slug, formations.RunStartRequest{
		MissionID:         missionID,
		Cwd:               *run.cwd,
		ContextPaths:      run.contextPaths,
		BeadID:            *run.bead,
		Inputs:            inputs,
		Actor:             *actor,
		ExpectedBoardETag: board.ETag,
		ExpectedBoardRev:  board.Rev,
		Personas:          personas,
	})
	if err != nil {
		return failJSON(stderr, err, *jsonOut, "run", missionID)
	}
	return writeRunCommandResponse(stdout, status, *jsonOut)
}

func runFormationRun(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("formation run", stderr)
	actor := fs.String("actor", "agent:archon", "run actor")
	run := registerRunStartFlags(fs)
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(stderr, commandUsage("formation run"))
		return 2
	}
	inputs, err := run.inputs.collect()
	if err == nil {
		err = run.checkCwd()
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	slug, board, formationID, err := resolveFormationCommandTarget(store, fs.Arg(0), fs.Arg(1))
	if err != nil {
		return failSelector(stderr, err, *jsonOut, "formation", fs.Arg(1))
	}
	personas := formations.NewPersonaStore(formations.AgentsDir(store.Workspace))
	if err := formations.CheckRunAdmission(board, personas, formations.RunAdmissionScope{FormationID: formationID, Inputs: inputs}); err != nil {
		return failJSON(stderr, err, *jsonOut, "run", formationID)
	}
	release, err := holdStateLock(store.Workspace)
	if err != nil {
		return failJSON(stderr, err, *jsonOut, "run", formationID)
	}
	defer release()
	engine := newArchonRunEngine(store, personas, "archon")
	status, err := engine.RunFormation(slug, formationID, formations.FormationRunRequest{
		Actor:             *actor,
		Personas:          personas,
		Cwd:               *run.cwd,
		ContextPaths:      run.contextPaths,
		BeadID:            *run.bead,
		Inputs:            inputs,
		ExpectedBoardETag: board.ETag,
		ExpectedBoardRev:  board.Rev,
	})
	if err != nil {
		return failJSON(stderr, err, *jsonOut, "run", formationID)
	}
	return writeRunCommandResponse(stdout, status, *jsonOut)
}

func runList(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("run list", stderr)
	boardSelector := fs.String("mission", "", "list only this mission's runs")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stderr, commandUsage("run list"))
		return 2
	}
	filter := formations.RunListFilter{}
	if *boardSelector != "" {
		resolved, err := store.ResolveBoardSelector(*boardSelector)
		if err != nil {
			return failSelector(stderr, err, *jsonOut, "mission", *boardSelector)
		}
		board, err := store.ReadBoard(resolved)
		if err != nil {
			return failSelector(stderr, err, *jsonOut, "mission", *boardSelector)
		}
		filter.MissionID = board.ID
	}
	runs, unreadable, err := store.ListRunsSkipping(filter)
	if err != nil {
		return failJSON(stderr, err, *jsonOut, "run", "")
	}
	warnUnreadable(stderr, "run", unreadable)
	if *jsonOut {
		return writeJSON(stdout, map[string]any{"runs": runs})
	}
	writeRunList(stdout, runListLines(runs))
	return 0
}

func runStatus(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("run status", stderr)
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, commandUsage("run status"))
		return 2
	}
	status, err := store.ProjectRun(fs.Arg(0))
	if err != nil {
		return failJSON(stderr, err, *jsonOut, "run", fs.Arg(0))
	}
	if *jsonOut {
		return writeJSON(stdout, status)
	}
	fmt.Fprintf(stdout, "%s\t%s\t%s\t%d events\n", status.RunID, status.Status, status.BoardSlug, status.EventCount)
	return 0
}

func runLogs(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("run logs", stderr)
	nodeID := fs.String("node", "", "node id filter")
	follow := fs.Bool("follow", false, "follow the ledger")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true, "follow": true})); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, commandUsage("run logs"))
		return 2
	}
	runID := fs.Arg(0)
	events, err := store.ReadRunEvents(runID)
	if err != nil {
		return failJSON(stderr, err, *jsonOut, "run", runID)
	}
	filtered := filterRunEvents(events, *nodeID)
	if *follow && *jsonOut {
		for {
			status, err := store.ProjectRun(runID)
			if err != nil {
				return failJSON(stderr, err, *jsonOut, "run", runID)
			}
			if status.Final || isBlockedResumable(status) {
				break
			}
			time.Sleep(25 * time.Millisecond)
		}
		events, err = store.ReadRunEvents(runID)
		if err != nil {
			return failJSON(stderr, err, *jsonOut, "run", runID)
		}
		filtered = filterRunEvents(events, *nodeID)
	}
	if *jsonOut {
		return writeJSON(stdout, filtered)
	}
	writeRunEventsText(stdout, filtered)
	if *follow {
		lastSeq := lastRunSeq(events)
		for {
			time.Sleep(250 * time.Millisecond)
			nextEvents, err := store.ReadRunEvents(runID)
			if err != nil {
				return failJSON(stderr, err, *jsonOut, "run", runID)
			}
			var newEvents []formations.RunEvent
			for _, event := range nextEvents {
				if event.Seq > lastSeq {
					newEvents = append(newEvents, event)
				}
			}
			writeRunEventsText(stdout, filterRunEvents(newEvents, *nodeID))
			lastSeq = lastRunSeq(nextEvents)
			status, err := store.ProjectRun(runID)
			if err != nil {
				return failJSON(stderr, err, *jsonOut, "run", runID)
			}
			if status.Final {
				return 0
			}
		}
	}
	return 0
}

func runFollow(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("run follow", stderr)
	nodeID := fs.String("node", "", "node id filter")
	since := fs.Int("since", 0, "only emit events with seq greater than this value")
	jsonOut := fs.Bool("json", false, "write NDJSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, commandUsage("run follow"))
		return 2
	}
	if *since < 0 {
		return failJSON(stderr, fmt.Errorf("%w: --since must be non-negative", formations.ErrInvalidSlug), *jsonOut, "run", fs.Arg(0))
	}
	runID := fs.Arg(0)
	lastSeq := *since
	for {
		events, err := store.ReadRunEvents(runID)
		if err != nil {
			return failRunStreamError(stdout, stderr, err, *jsonOut, "run", runID)
		}
		for _, event := range events {
			if event.Seq <= lastSeq || !runEventReferencesNode(event, *nodeID) {
				continue
			}
			if *jsonOut {
				if err := writeNDJSON(stdout, event); err != nil {
					return 1
				}
			} else {
				writeRunEventsText(stdout, []formations.RunEvent{event})
			}
		}
		if ledgerLast := lastRunSeq(events); ledgerLast > lastSeq {
			lastSeq = ledgerLast
		}
		status, err := store.ProjectRun(runID)
		if err != nil {
			return failRunStreamError(stdout, stderr, err, *jsonOut, "run", runID)
		}
		if status.Final {
			return 0
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func isBlockedResumable(status *formations.RunStatusProjection) bool {
	return status != nil && status.Status == formations.RunStatusBlocked && status.ResumeAllowed
}

func runResume(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("run resume", stderr)
	actor := fs.String("actor", "agent:archon", "resume actor")
	mode := fs.String("mode", "reattach", "resume mode")
	reason := fs.String("reason", "", "resume reason")
	grant := fs.Bool("grant", false, grantUsage)
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true, "grant": true})); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, commandUsage("run resume"))
		return 2
	}
	release, err := holdStateLock(store.Workspace)
	if err != nil {
		return failJSON(stderr, err, *jsonOut, "run", fs.Arg(0))
	}
	defer release()
	personas := formations.NewPersonaStore(formations.AgentsDir(store.Workspace))
	engine := newArchonRunEngine(store, personas, "archon")
	status, err := engine.ResumeRun(fs.Arg(0), formations.RunResumeRequest{
		Actor:  *actor,
		Mode:   *mode,
		Reason: *reason,
		Grant:  *grant,
	})
	if err != nil {
		return failJSON(stderr, err, *jsonOut, "run", fs.Arg(0))
	}
	if *jsonOut {
		return writeJSON(stdout, status)
	}
	fmt.Fprintf(stdout, "%s\t%s\t%d\n", status.RunID, status.Status, status.Epoch)
	return 0
}

func runAbort(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("run abort", stderr)
	reason := fs.String("reason", "operator abort", "abort reason")
	requestedBy := fs.String("requested-by", "agent:archon", "requesting actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, commandUsage("run abort"))
		return 2
	}
	runID := fs.Arg(0)
	release, err := holdStateLock(store.Workspace)
	if err != nil {
		return failJSON(stderr, err, *jsonOut, "run", runID)
	}
	defer release()
	if err := store.AppendRunEvent(runID, formations.RunEvent{
		Type:  formations.RunEventCanceled,
		Actor: *requestedBy,
		Data: map[string]any{
			"reason":      *reason,
			"requestedBy": *requestedBy,
			"final":       true,
		},
	}); err != nil {
		return failJSON(stderr, err, *jsonOut, "run", runID)
	}
	status, err := store.ProjectRun(runID)
	if err != nil {
		return failJSON(stderr, err, *jsonOut, "run", runID)
	}
	if *jsonOut {
		return writeJSON(stdout, status)
	}
	fmt.Fprintf(stdout, "%s\t%s\n", status.RunID, status.Status)
	return 0
}

func runAsk(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("run ask", stderr)
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(stderr, commandUsage("run ask"))
		return 2
	}
	runID := fs.Arg(0)
	question := strings.Join(fs.Args()[1:], " ")
	status, err := store.ProjectRun(runID)
	if err != nil {
		return failJSON(stderr, err, *jsonOut, "run", runID)
	}
	events, err := store.ReadRunEvents(runID)
	if err != nil {
		return failJSON(stderr, err, *jsonOut, "run", runID)
	}
	escalations, err := store.ProjectOpenEscalations(runID)
	if err != nil {
		return failJSON(stderr, err, *jsonOut, "run", runID)
	}
	response := buildRunAskResponse(runID, question, status, events, escalations)
	if *jsonOut {
		return writeJSON(stdout, response)
	}
	fmt.Fprintln(stdout, response.Answer)
	return 0
}

func writeRunCommandResponse(stdout io.Writer, status *formations.RunStatusProjection, jsonOut bool) int {
	if jsonOut {
		return writeJSON(stdout, struct {
			RunID  string                          `json:"runId"`
			Status *formations.RunStatusProjection `json:"status"`
		}{
			RunID:  status.RunID,
			Status: status,
		})
	}
	fmt.Fprintf(stdout, "%s	%s	%s\n", status.RunID, status.Status, status.BoardSlug)
	return 0
}

func buildRunAskResponse(runID, question string, status *formations.RunStatusProjection, events []formations.RunEvent, escalations []formations.OpenEscalation) archonRunAskResponse {
	response := archonRunAskResponse{
		RunID:           runID,
		Question:        question,
		Status:          status,
		OpenEscalations: escalations,
	}
	waitingBySeq := map[int]archonRunWaitingGate{}
	waitingOrder := []int{}
	seenEvidence := map[int]bool{}
	addEvidence := func(seq int) {
		if seq <= 0 || seenEvidence[seq] {
			return
		}
		seenEvidence[seq] = true
		response.EvidenceSeqs = append(response.EvidenceSeqs, seq)
	}
	for _, escalation := range escalations {
		addEvidence(escalation.Seq)
	}
	for _, event := range events {
		switch event.Type {
		case formations.RunEventNodeOutput:
			statusText := stringFromMap(event.Data, "status")
			if statusText == "" {
				statusText = "done"
			}
			completed := archonRunCompletedNode{
				NodeID:    event.NodeID,
				Status:    statusText,
				OutputSeq: event.Seq,
				ReportRef: stringFromMap(event.Data, "reportRef"),
				Text:      stringFromMap(event.Data, "text"),
			}
			response.CompletedNodes = append(response.CompletedNodes, completed)
			response.ProducedOutputs = append(response.ProducedOutputs, archonRunProducedOutput{
				NodeID:    completed.NodeID,
				OutputSeq: completed.OutputSeq,
				ReportRef: completed.ReportRef,
				Text:      completed.Text,
			})
			addEvidence(event.Seq)
		case formations.RunEventHumanInputRequested:
			waiting := archonRunWaitingGate{
				GateID:       event.GateID,
				NodeID:       event.NodeID,
				Prompt:       stringFromMap(event.Data, "prompt"),
				Choices:      stringSliceFromMap(event.Data, "choices"),
				RequestedSeq: event.Seq,
			}
			waitingBySeq[event.Seq] = waiting
			waitingOrder = append(waitingOrder, event.Seq)
			addEvidence(event.Seq)
		case formations.RunEventHumanVerdictRecorded:
			requestedSeq := intFromMap(event.Data, "requestedSeq")
			if requestedSeq > 0 {
				delete(waitingBySeq, requestedSeq)
				continue
			}
			for seq, waiting := range waitingBySeq {
				if waiting.GateID == event.GateID {
					delete(waitingBySeq, seq)
				}
			}
		case formations.RunEventBlocked:
			response.BlockedReasons = append(response.BlockedReasons, archonRunBlockedReason{
				Seq:           event.Seq,
				NodeID:        firstNonEmpty(event.NodeID, stringFromMap(event.Data, "blockedNodeId")),
				GateID:        firstNonEmpty(event.GateID, stringFromMap(event.Data, "blockedGateId")),
				Reason:        stringFromMap(event.Data, "reason"),
				Code:          stringFromMap(event.Data, "code"),
				Boundary:      stringFromMap(event.Data, "boundary"),
				ResumeAllowed: boolFromMap(event.Data, "resumeAllowed"),
			})
			addEvidence(event.Seq)
		}
	}
	for _, seq := range waitingOrder {
		if waiting, ok := waitingBySeq[seq]; ok {
			response.WaitingGates = append(response.WaitingGates, waiting)
		}
	}
	if len(response.CompletedNodes) == 0 {
		response.MissingEvidence = append(response.MissingEvidence, "no node_output events are present in the durable ledger")
	}
	if len(response.ProducedOutputs) == 0 {
		response.MissingEvidence = append(response.MissingEvidence, "no produced output text or report references are present in the durable ledger")
	}
	response.Answer = buildRunAskAnswer(response)
	return response
}

func buildRunAskAnswer(response archonRunAskResponse) string {
	parts := []string{}
	if response.Status != nil {
		parts = append(parts, fmt.Sprintf("Run %s is %s with %d ledger events", response.RunID, response.Status.Status, response.Status.EventCount))
	} else {
		parts = append(parts, fmt.Sprintf("Run %s has no status projection", response.RunID))
	}
	if len(response.CompletedNodes) > 0 {
		nodes := make([]string, 0, len(response.CompletedNodes))
		for _, node := range response.CompletedNodes {
			nodes = append(nodes, node.NodeID)
		}
		parts = append(parts, fmt.Sprintf("completed nodes: %s", strings.Join(nodes, ", ")))
	} else {
		parts = append(parts, "no completed node_output evidence yet")
	}
	if len(response.ProducedOutputs) > 0 {
		latest := response.ProducedOutputs[len(response.ProducedOutputs)-1]
		detail := latest.ReportRef
		if latest.Text != "" {
			detail = clipText(latest.Text, 160)
		}
		if detail != "" {
			parts = append(parts, fmt.Sprintf("latest output from %s: %s", latest.NodeID, detail))
		}
	}
	if len(response.WaitingGates) > 0 {
		gates := make([]string, 0, len(response.WaitingGates))
		for _, gate := range response.WaitingGates {
			gates = append(gates, gate.GateID)
		}
		parts = append(parts, fmt.Sprintf("waiting gates: %s", strings.Join(gates, ", ")))
	}
	if len(response.BlockedReasons) > 0 {
		latest := response.BlockedReasons[len(response.BlockedReasons)-1]
		parts = append(parts, fmt.Sprintf("latest block: %s", latest.Reason))
	}
	if len(response.OpenEscalations) > 0 {
		escalations := make([]string, 0, len(response.OpenEscalations))
		for _, escalation := range response.OpenEscalations {
			where := firstNonEmpty(escalation.NodeID, escalation.GateID)
			if where == "" {
				where = "run"
			}
			escalations = append(escalations, where+": "+escalation.Reason)
		}
		parts = append(parts, fmt.Sprintf("open escalations: %s", strings.Join(escalations, "; ")))
	}
	if len(response.MissingEvidence) > 0 {
		parts = append(parts, "missing evidence: "+strings.Join(response.MissingEvidence, "; "))
	}
	return strings.Join(parts, ". ") + "."
}

func filterRunEvents(events []formations.RunEvent, nodeID string) []formations.RunEvent {
	filtered := make([]formations.RunEvent, 0, len(events))
	for _, event := range events {
		if !runEventReferencesNode(event, nodeID) {
			continue
		}
		filtered = append(filtered, event)
	}
	return filtered
}

func runEventReferencesNode(event formations.RunEvent, nodeID string) bool {
	if nodeID == "" || event.NodeID == nodeID {
		return true
	}
	if event.Data == nil {
		return false
	}
	for _, key := range []string{"nodeId", "blockedNodeId", "fromNodeId", "toNodeId"} {
		if stringFromMap(event.Data, key) == nodeID {
			return true
		}
	}
	return false
}

func writeRunEventsText(stdout io.Writer, events []formations.RunEvent) {
	for _, event := range events {
		fmt.Fprintf(stdout, "%d\t%s\t%s\n", event.Seq, event.Type, event.NodeID)
	}
}

func lastRunSeq(events []formations.RunEvent) int {
	if len(events) == 0 {
		return 0
	}
	return events[len(events)-1].Seq
}

func identityFromBoard(board *formations.BoardDocument) archonBoardIdentity {
	return archonBoardIdentity{
		ID:    board.ID,
		Slug:  board.Slug,
		Title: board.Title,
		Rev:   board.Rev,
		ETag:  board.ETag,
	}
}

func runBoardNew(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("mission new", stderr)
	title := fs.String("title", "", "mission title")
	updatedBy := fs.String("updated-by", "agent:archon", "update actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, commandUsage("mission new"))
		return 2
	}
	info, err := os.Stat(store.Workspace)
	if errors.Is(err, os.ErrNotExist) {
		return failJSON(stderr, fmt.Errorf("workspace directory %q does not exist; mission new does not create it: create the directory first", store.Workspace), *jsonOut, "mission", fs.Arg(0))
	}
	if err != nil {
		return failJSON(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	if !info.IsDir() {
		return failJSON(stderr, fmt.Errorf("workspace path %q is not a directory", store.Workspace), *jsonOut, "mission", fs.Arg(0))
	}
	board, err := store.CreateBoard(formations.BoardCreateRequest{
		Slug:      fs.Arg(0),
		Title:     *title,
		UpdatedBy: *updatedBy,
	})
	if err != nil {
		return failJSON(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	board.TOML = ""
	if *jsonOut {
		return writeJSON(stdout, board)
	}
	fmt.Fprintf(stdout, "created %s\n", board.Slug)
	return 0
}

func runBoardList(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("mission list", stderr)
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	boards, err := store.ListBoards()
	if err != nil {
		return fail(stderr, err)
	}
	return writeBoardList(stdout, boards, *jsonOut)
}

// writeBoardList prints board summaries for board list, offline and remote.
func writeBoardList(stdout io.Writer, boards []formations.BoardSummary, jsonOut bool) int {
	if jsonOut {
		return writeJSON(stdout, map[string]interface{}{"missions": boards})
	}
	for _, board := range boards {
		if board.Broken != "" {
			fmt.Fprintf(stdout, "%s\tbroken: %s\n", board.Slug, board.Broken)
			continue
		}
		fmt.Fprintf(stdout, "%s\t%s\t%d\n", board.Slug, board.Title, board.Rev)
	}
	return 0
}

func runBoardInspect(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("mission inspect", stderr)
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, commandUsage("mission inspect"))
		return 2
	}
	slug, err := store.ResolveBoardSelector(fs.Arg(0))
	if err != nil {
		return failSelector(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	board, err := store.ReadBoard(slug)
	if err != nil {
		return failJSON(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	return writeBoardInspect(stdout, board, *jsonOut)
}

// writeBoardInspect prints one board for board inspect, offline and remote.
func writeBoardInspect(stdout io.Writer, board *formations.BoardDocument, jsonOut bool) int {
	board.TOML = ""
	if jsonOut {
		return writeJSON(stdout, board)
	}
	fmt.Fprintf(stdout, "%s\t%s\t%d\t%d formations\n", board.Slug, board.Title, board.Rev, len(board.Formations))
	return 0
}

func runBoardNotes(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("mission notes", stderr)
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, commandUsage("mission notes"))
		return 2
	}
	slug, err := store.ResolveBoardSelector(fs.Arg(0))
	if err != nil {
		return failSelector(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	notes, err := store.ReadBoardNotes(slug)
	if err != nil {
		return fail(stderr, err)
	}
	if *jsonOut {
		return writeJSON(stdout, notes)
	}
	writeBoardNotesText(stdout, slug, notes)
	return 0
}

// writeBoardNotesText prints each thread with one header line per entry; the
// offline and --server commands share it so their output matches.
func writeBoardNotesText(stdout io.Writer, slug string, notes *formations.BoardNotesDocument) {
	if len(notes.Board) == 0 && len(notes.Elements) == 0 {
		fmt.Fprintf(stdout, "%s\tno notes\n", slug)
		return
	}
	writeThread := func(target string, entries []formations.NoteEntry) {
		fmt.Fprintf(stdout, "[%s]\n", target)
		for _, entry := range entries {
			edited := ""
			if entry.EditedAt != nil {
				edited = "\tedited"
			}
			fmt.Fprintf(stdout, "%s\t%s\t%s%s\n%s\n\n", entry.ID, entry.Author, entry.CreatedAt.Format(time.RFC3339), edited, entry.Text)
		}
	}
	if len(notes.Board) > 0 {
		writeThread(noteTargetLabel(formations.BoardNoteTarget), notes.Board)
	}
	for _, element := range notes.Elements {
		writeThread(element.NodeID, element.Entries)
	}
}

func runBoardNote(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("mission note", stderr)
	selector, patch, jsonOut, code := parseBoardNote(fs, args, stderr)
	if code != 0 {
		return code
	}
	slug, err := store.ResolveBoardSelector(selector)
	if err != nil {
		return failSelector(stderr, err, jsonOut, "mission", selector)
	}
	current, err := store.ReadBoardNotes(slug)
	if err != nil {
		return fail(stderr, err)
	}
	updated, err := store.UpdateBoardNote(slug, patch, formations.NoteWriteOptions{ExpectedETag: current.ETag})
	if err != nil {
		return failJSON(stderr, err, jsonOut, "mission", selector)
	}
	return writeBoardNoteResult(stdout, slug, patch, updated, jsonOut)
}

// parseBoardNote reads the board note flags shared by the offline and --server
// commands. A note appends; --entry edits or, with --clear, deletes one of the
// author's own entries.
func parseBoardNote(fs *flag.FlagSet, args []string, stderr io.Writer) (string, formations.BoardNotePatch, bool, int) {
	text := fs.String("text", "", "note text")
	file := fs.String("file", "", "read note text from file")
	node := fs.String("node", "", "element id; omit for the mission thread")
	entry := fs.String("entry", "", "your own entry to edit (with --text or --file) or delete (with --clear)")
	clear := fs.Bool("clear", false, "delete the entry named by --entry")
	author := fs.String("author", "agent:archon", "note author, human:<name> or agent:<name>")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"clear": true, "json": true})); err != nil {
		return "", formations.BoardNotePatch{}, false, 2
	}
	given := givenFlags(fs)
	if fs.NArg() != 1 || boolCount(given["text"], *file != "", *clear) != 1 || *clear && *entry == "" {
		fmt.Fprintln(stderr, commandUsage("mission note"))
		fmt.Fprintln(stderr, "       archon mission note <mission> [--node <element-id>] --clear --entry <id>")
		fmt.Fprintln(stderr, "A note appends to the thread. --entry edits or, with --clear, deletes one of your own entries; others' entries cannot be changed.")
		return "", formations.BoardNotePatch{}, *jsonOut, 2
	}
	patch := formations.BoardNotePatch{Target: strings.TrimSpace(*node), Text: *text, Author: *author}
	if patch.Target == "" {
		patch.Target = formations.BoardNoteTarget
	}
	if *file != "" {
		raw, err := os.ReadFile(*file)
		if err != nil {
			return "", formations.BoardNotePatch{}, *jsonOut, fail(stderr, err)
		}
		patch.Text = string(raw)
	}
	switch {
	case *clear:
		patch.Action, patch.EntryID, patch.Text = formations.NoteActionDelete, *entry, ""
	case *entry != "":
		patch.Action, patch.EntryID = formations.NoteActionEdit, *entry
	default:
		patch.Action = formations.NoteActionAppend
	}
	return fs.Arg(0), patch, *jsonOut, 0
}

func writeBoardNoteResult(stdout io.Writer, slug string, patch formations.BoardNotePatch, updated *formations.BoardNotesDocument, jsonOut bool) int {
	if jsonOut {
		return writeJSON(stdout, updated)
	}
	switch patch.Action {
	case formations.NoteActionAppend:
		thread := updated.Board
		for _, element := range updated.Elements {
			if element.NodeID == patch.Target {
				thread = element.Entries
			}
		}
		fmt.Fprintf(stdout, "added note %s on %s (notes rev %d)\n", thread[len(thread)-1].ID, noteTargetLabel(patch.Target), updated.Rev)
	case formations.NoteActionEdit:
		fmt.Fprintf(stdout, "edited note %s on %s (notes rev %d)\n", patch.EntryID, noteTargetLabel(patch.Target), updated.Rev)
	default:
		fmt.Fprintf(stdout, "deleted note %s on %s (notes rev %d)\n", patch.EntryID, noteTargetLabel(patch.Target), updated.Rev)
	}
	return 0
}

// noteTargetLabel names a note thread in text output.
func noteTargetLabel(target string) string {
	if target == formations.BoardNoteTarget {
		return "mission"
	}
	return target
}

func boolCount(values ...bool) int {
	count := 0
	for _, value := range values {
		if value {
			count++
		}
	}
	return count
}

func runBoardValidate(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("mission validate", stderr)
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, commandUsage("mission validate"))
		return 2
	}
	slug, err := store.ResolveBoardSelector(fs.Arg(0))
	if err != nil {
		return failSelector(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	board, err := store.ReadBoard(slug)
	if err != nil {
		return failJSON(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	report := formations.ValidateRunAdmission(board, formations.NewPersonaStore(formations.AgentsDir(store.Workspace)), formations.RunAdmissionScope{})
	if *jsonOut {
		code := writeJSON(stdout, map[string]interface{}{
			"mission":  identityFromBoard(board),
			"errors":   report.Errors,
			"warnings": report.Warnings,
		})
		if code != 0 {
			return code
		}
	} else {
		fmt.Fprintf(stdout, "%s	%d errors	%d warnings\n", board.Slug, len(report.Errors), len(report.Warnings))
		writeFindingsText(stdout, "ERROR", report.Errors, board)
		writeFindingsText(stdout, "WARN", report.Warnings, board)
	}
	if len(report.Errors) > 0 {
		return 1
	}
	return 0
}

func runBoardArrange(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("mission arrange", stderr)
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, commandUsage("mission arrange"))
		return 2
	}
	slug, err := store.ResolveBoardSelector(fs.Arg(0))
	if err != nil {
		return failSelector(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	expectedETag := "*"
	if current, err := store.ReadLayout(slug); err == nil {
		expectedETag = current.ETag
	} else if !errors.Is(err, formations.ErrNotFound) {
		return fail(stderr, err)
	}
	layout, err := store.ArrangeLayout(slug, formations.WriteOptions{ExpectedETag: expectedETag})
	if err != nil {
		return failDefinitionWrite(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	layout.TOML = ""
	if *jsonOut {
		return writeJSON(stdout, layout)
	}
	fmt.Fprintf(stdout, "arranged %s\n", slug)
	return 0
}

func runFormationList(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("formation list", stderr)
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, commandUsage("formation list"))
		return 2
	}
	slug, err := store.ResolveBoardSelector(fs.Arg(0))
	if err != nil {
		return failSelector(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	board, err := store.ReadBoard(slug)
	if err != nil {
		return failJSON(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	return writeFormationList(stdout, board, *jsonOut)
}

// writeFormationList prints a board's formations with their slot staffing, for
// formation list offline and remote.
func writeFormationList(stdout io.Writer, board *formations.BoardDocument, jsonOut bool) int {
	response := archonFormationListResponse{
		Board:      identityFromBoard(board),
		Formations: append([]formations.FormationNode{}, board.Formations...),
	}
	if jsonOut {
		return writeJSON(stdout, response)
	}
	for _, formation := range response.Formations {
		fmt.Fprintln(stdout, formationSummary(formation))
	}
	return 0
}

// formationSummary is a formation's text line: id, type, title and staffing.
func formationSummary(formation formations.FormationNode) string {
	staffed := 0
	for _, slot := range formation.Slots {
		if slot.Staffed() {
			staffed++
		}
	}
	return fmt.Sprintf("%s\t%s\t%s\t%d/%d staffed", formation.ID, formation.Type, formation.Title, staffed, len(formation.Slots))
}

func runFormationInspect(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("formation inspect", stderr)
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(stderr, commandUsage("formation inspect"))
		return 2
	}
	slug, err := store.ResolveBoardSelector(fs.Arg(0))
	if err != nil {
		return failSelector(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	board, err := store.ReadBoard(slug)
	if err != nil {
		return failJSON(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	return writeFormationInspect(stdout, stderr, board, fs.Arg(1), *jsonOut)
}

// writeFormationInspect resolves a formation on a read board and prints it with
// the connections at its ports, for formation inspect offline and remote.
func writeFormationInspect(stdout, stderr io.Writer, board *formations.BoardDocument, selector string, jsonOut bool) int {
	formationID, err := resolveFormationSelector(board, selector)
	if err != nil {
		return failSelector(stderr, err, jsonOut, "formation", selector)
	}
	response := archonFormationInspectResponse{Board: identityFromBoard(board), Connections: []formations.BoardConnection{}}
	for _, formation := range board.Formations {
		if formation.ID == formationID {
			response.Formation = formation
		}
	}
	for _, connection := range board.Connections {
		if strings.SplitN(connection.From, ":", 2)[0] == formationID || strings.SplitN(connection.To, ":", 2)[0] == formationID {
			response.Connections = append(response.Connections, connection)
		}
	}
	if jsonOut {
		return writeJSON(stdout, response)
	}
	fmt.Fprintf(stdout, "%s\t%d connections\n", formationSummary(response.Formation), len(response.Connections))
	for _, slot := range response.Formation.Slots {
		controller := ""
		if slot.Controller {
			controller = " (controller)"
		}
		fmt.Fprintf(stdout, "slot %s\t%s%s\t%s\n", slot.ID, slot.Label, controller, slot.StaffingSummary())
	}
	return 0
}

func liveFromRunner(runner tmuxRunner) ([]formations.LiveAgentSession, error) {
	if runner == nil {
		return nil, nil
	}
	live, err := runner.LiveSessions()
	if err != nil {
		return nil, err
	}
	return archonLogicalTmuxSessions(live), nil
}

func archonTmuxSessionPrefix() string {
	return strings.TrimSpace(os.Getenv("ARCHON_TMUX_SESSION_PREFIX"))
}

func archonTmuxTargetSessionName(stem string) string {
	return archonTmuxSessionPrefix() + stem
}

func archonLogicalTmuxSessions(live []formations.LiveAgentSession) []formations.LiveAgentSession {
	prefix := archonTmuxSessionPrefix()
	if prefix == "" {
		return live
	}
	logical := make([]formations.LiveAgentSession, 0, len(live))
	for _, session := range live {
		stem, ok := strings.CutPrefix(session.Name, prefix)
		if !ok || stem == "" {
			continue
		}
		session.Name = stem
		logical = append(logical, session)
	}
	return logical
}

func archonExposeTmuxTargetSessions(roster *formations.AgentRoster) {
	if roster == nil || archonTmuxSessionPrefix() == "" {
		return
	}
	for i := range roster.Agents {
		if roster.Agents[i].SessionID != "" {
			roster.Agents[i].SessionID = archonTmuxTargetSessionName(roster.Agents[i].SessionID)
		}
	}
}

func (realTmuxRunner) LiveSessions() ([]formations.LiveAgentSession, error) {
	cmd := exec.Command(core.TmuxBin(), archonTmuxArgs("list-sessions", "-F", "#{session_name}:#{session_attached}")...)
	cmd.Env = archonTmuxEnv()
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr := string(exitErr.Stderr)
			if formations.TmuxHasNoServer(stderr) {
				return nil, nil
			}
			return nil, fmt.Errorf("%s: %s", err.Error(), stderr)
		}
		return nil, err
	}
	return formations.ParseTmuxSessionList(string(output)), nil
}

func (realTmuxRunner) Spawn(name, command string) error {
	args := []string{"new-session", "-d", "-s", name}
	if command != "" {
		args = append(args, command)
	}
	cmd := exec.Command(core.TmuxBin(), archonTmuxArgs(args...)...)
	cmd.Env = archonTmuxEnv()
	return cmd.Run()
}

func (realTmuxRunner) Attach(name string) error {
	cmd := exec.Command(core.TmuxBin(), archonTmuxArgs("attach-session", "-t", name)...)
	cmd.Env = archonTmuxEnv()
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func archonTmuxArgs(args ...string) []string {
	socket := strings.TrimSpace(os.Getenv("ARCHON_TMUX_SOCKET"))
	if socket == "" {
		return append([]string(nil), args...)
	}
	allArgs := []string{"-S", socket}
	allArgs = append(allArgs, args...)
	return allArgs
}

func archonTmuxEnv() []string {
	base := core.GetTmuxEnv()
	env := make([]string, 0, len(base))
	for _, item := range base {
		if strings.HasPrefix(item, "TMUX=") {
			continue
		}
		env = append(env, item)
	}
	return env
}

func writeJSON(w io.Writer, value interface{}) int {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return 1
	}
	return 0
}

func writeNDJSON(w io.Writer, value interface{}) error {
	return json.NewEncoder(w).Encode(value)
}

func fail(stderr io.Writer, err error) int {
	var admission *formations.RunAdmissionError
	if errors.As(err, &admission) {
		fmt.Fprintf(stderr, "run admission found %d problem(s)\n", len(admission.Findings))
		writeFindingsText(stderr, "ERROR", admission.Findings, nil)
		return 1
	}
	fmt.Fprintln(stderr, archonErrorMessage(err))
	return 1
}

// writeFindingsText prints one finding per line. Given the mission, it names
// each node by title and ID (archon-n7u.33).
func writeFindingsText(w io.Writer, level string, findings []formations.BoardFinding, board *formations.BoardDocument) {
	for _, finding := range findings {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", level, finding.Code, findingWhere(board, finding.NodeID), finding.Message)
	}
}

// findingWhere is the node a finding is about: its title and ID, a
// connection's ends, or the mission itself.
func findingWhere(board *formations.BoardDocument, nodeID string) string {
	if nodeID == "" {
		return "mission"
	}
	if board == nil {
		return nodeID
	}
	if title := strings.TrimSpace(board.NodeTitle(nodeID)); title != "" {
		return title + " (" + nodeID + ")"
	}
	for _, connection := range board.Connections {
		if connection.ID == nodeID {
			return "connection " + nodeID + " (" + connection.From + " -> " + connection.To + ")"
		}
	}
	return nodeID
}

func failJSON(stderr io.Writer, err error, jsonOut bool, boundary, selector string) int {
	if !jsonOut {
		return fail(stderr, err)
	}
	if code := writeJSON(stderr, archonErrorFromError(err, boundary, selector)); code != 0 {
		return code
	}
	return 1
}

func failDefinitionWrite(stderr io.Writer, err error, jsonOut bool, boundary, selector string) int {
	if errors.Is(err, formations.ErrInvalidDefinitionSource) || errors.Is(err, formations.ErrInvalidSlotSettings) || errors.Is(err, formations.ErrInvalidMissionInput) || errors.Is(err, formations.ErrRelativeFileRef) || errors.Is(err, formations.ErrInputOccupied) || errors.Is(err, formations.ErrSelfWire) || errors.Is(err, formations.ErrDuplicateConnection) || errors.Is(err, formations.ErrIncompatibleToolConnection) {
		return failJSON(stderr, err, jsonOut, boundary, selector)
	}
	return fail(stderr, err)
}

func failRunStreamError(stdout, stderr io.Writer, err error, jsonOut bool, boundary, selector string) int {
	if !jsonOut {
		return fail(stderr, err)
	}
	if err := writeNDJSON(stdout, archonStreamError{Type: "stream_error", Error: archonErrorFromError(err, boundary, selector)}); err != nil {
		return 1
	}
	return 1
}

func archonErrorFromError(err error, boundary, selector string) archonErrorResponse {
	response := archonErrorResponse{
		Code:     archonErrorCode(err),
		Message:  archonErrorMessage(err),
		Boundary: boundary,
		Selector: selector,
	}
	var admission *formations.RunAdmissionError
	if errors.As(err, &admission) {
		response.Findings = admission.Findings
	}
	return response
}

func archonErrorMessage(err error) string {
	if errors.Is(err, formations.ErrDefinitionPublicationUncertain) {
		return "Reload both the mission and its layout before any explicit retry"
	}
	return err.Error()
}

func archonErrorCode(err error) string {
	switch {
	case errors.Is(err, formations.ErrRunAdmission):
		return "run_admission_failed"
	case errors.Is(err, formations.ErrInvalidGateKind):
		return "invalid_gate_kind"
	case errors.Is(err, formations.ErrUnsupportedFormationType):
		return "unsupported_formation_type"
	case errors.Is(err, formations.ErrInvalidNotePatch):
		return "invalid_note_patch"
	case errors.Is(err, formations.ErrNoteEntryNotFound):
		return "note_entry_not_found"
	case errors.Is(err, formations.ErrNoteAuthorMismatch):
		return "note_author_mismatch"
	case errors.Is(err, formations.ErrInvalidTypeChange):
		return "invalid_type_change"
	case errors.Is(err, formations.ErrSlotChoiceRequired):
		return "slot_choice_required"
	case errors.Is(err, formations.ErrInvalidSlotSettings):
		return "invalid_slot_settings"
	case errors.Is(err, formations.ErrDefinitionPublicationUncertain):
		return "definition_publication_uncertain"
	case errors.Is(err, formations.ErrInvalidToolMutation):
		return "invalid_tool_mutation"
	case errors.Is(err, formations.ErrInvalidDefinitionSource):
		return formations.InvalidDefinitionSourceCode
	case errors.Is(err, formations.ErrToolExecutionUnavailable):
		return formations.ToolExecutionUnavailableCode
	case errors.Is(err, formations.ErrAmbiguousSelector):
		return "ambiguous_selector"
	case errors.Is(err, formations.ErrNotFound):
		return "not_found"
	case errors.Is(err, formations.ErrAlreadyExists):
		return "conflict"
	case errors.Is(err, formations.ErrInputOccupied):
		return "input_occupied"
	case errors.Is(err, formations.ErrSelfWire):
		return "self_wire"
	case errors.Is(err, formations.ErrDuplicateConnection):
		return "duplicate_connection"
	case errors.Is(err, formations.ErrIncompatibleToolConnection):
		return "incompatible_tool_connection"
	case errors.Is(err, formations.ErrConflict):
		return "conflict"
	case errors.Is(err, formations.ErrInvalidBeadID):
		return "invalid_bead_id"
	case errors.Is(err, formations.ErrInvalidHumanChannel):
		return "invalid_human_channel"
	case errors.Is(err, formations.ErrInvalidMissionInput):
		return "invalid_mission_input"
	case errors.Is(err, formations.ErrRelativeFileRef):
		return "relative_file_reference"
	case errors.Is(err, formations.ErrBrokenLink):
		return "broken_link"
	case errors.Is(err, formations.ErrInvalidRelayedBy):
		return "invalid_relayed_by"
	case errors.Is(err, formations.ErrInvalidControllerRole):
		return "invalid_controller_role"
	case errors.Is(err, formations.ErrInvalidPortDirection):
		return "invalid_port_direction"
	case errors.Is(err, formations.ErrInvalidAgentCard):
		return "invalid_agent_card"
	case errors.Is(err, formations.ErrRoleInUse):
		return "role_in_use"
	case errors.Is(err, formations.ErrBuiltinRole):
		return "builtin_role"
	case errors.Is(err, formations.ErrInvalidSlug):
		return "invalid_selector"
	case errors.Is(err, formations.ErrPreconditionRequired):
		return "precondition_required"
	case errors.Is(err, formations.ErrUnsupportedSchema):
		return "unsupported_schema"
	case errors.Is(err, formations.ErrRunFinal):
		return "run_final"
	case errors.Is(err, formations.ErrRunLedgerInvalid):
		return "run_ledger_invalid"
	case errors.Is(err, formations.ErrRunResumeNotAllowed):
		return "run_resume_not_allowed"
	case errors.Is(err, formations.ErrRunEpochBlocked):
		return "run_epoch_blocked"
	default:
		return "error"
	}
}

func failSelector(stderr io.Writer, err error, jsonOut bool, boundary, selector string) int {
	return failJSON(stderr, err, jsonOut, boundary, selector)
}

func splitCSV(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			values = append(values, part)
		}
	}
	return values
}

func stringFromMap(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return ""
	}
}

func boolFromMap(values map[string]any, key string) bool {
	if values == nil {
		return false
	}
	value, ok := values[key].(bool)
	return ok && value
}

func intFromMap(values map[string]any, key string) int {
	if values == nil {
		return 0
	}
	switch value := values[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		parsed, err := value.Int64()
		if err != nil {
			return 0
		}
		return int(parsed)
	default:
		return 0
	}
}

func stringSliceFromMap(values map[string]any, key string) []string {
	if values == nil {
		return nil
	}
	switch raw := values[key].(type) {
	case []string:
		return append([]string(nil), raw...)
	case []any:
		items := make([]string, 0, len(raw))
		for _, item := range raw {
			if text, ok := item.(string); ok && text != "" {
				items = append(items, text)
			}
		}
		return items
	default:
		return nil
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func clipText(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}

type stringList []string

func (l *stringList) String() string {
	return strings.Join(*l, ",")
}

func (l *stringList) Set(value string) error {
	*l = append(*l, value)
	return nil
}

func resolveFormationCommandTarget(store *formations.Store, boardSelector, formationSelector string) (string, *formations.BoardDocument, string, error) {
	slug, err := store.ResolveBoardSelector(boardSelector)
	if err != nil {
		return "", nil, "", err
	}
	board, err := store.ReadBoard(slug)
	if err != nil {
		return "", nil, "", err
	}
	formationID, err := resolveFormationSelector(board, formationSelector)
	if err != nil {
		return "", nil, "", err
	}
	return slug, board, formationID, nil
}

func resolveFormationSelector(board *formations.BoardDocument, selector string) (string, error) {
	candidates := make([]graphSelectorCandidate, 0, len(board.Formations))
	for _, formation := range board.Formations {
		candidates = append(candidates, graphSelectorCandidate{
			ID:    formation.ID,
			Title: formation.Title,
		})
	}
	return resolveGraphSelector("formation", selector, candidates)
}

func resolveGateSelector(board *formations.BoardDocument, selector string) (string, error) {
	candidates := make([]graphSelectorCandidate, 0, len(board.Gates))
	for _, gate := range board.Gates {
		candidates = append(candidates, graphSelectorCandidate{
			ID:    gate.ID,
			Title: gate.Title,
		})
	}
	return resolveGraphSelector("gate", selector, candidates)
}

func resolveMissionSelector(board *formations.BoardDocument, selector string) (string, error) {
	candidates := make([]graphSelectorCandidate, 0, len(board.Missions))
	for _, mission := range board.Missions {
		candidates = append(candidates, graphSelectorCandidate{
			ID:    mission.ID,
			Title: mission.Title,
		})
	}
	return resolveGraphSelector("Input card", selector, candidates)
}

type graphSelectorCandidate struct {
	ID    string
	Title string
}

func resolveGraphSelector(kind, selector string, candidates []graphSelectorCandidate) (string, error) {
	matches := map[string]graphSelectorCandidate{}
	for _, candidate := range candidates {
		if candidate.ID == selector || candidate.Title == selector || slugKey(candidate.Title) == selector {
			matches[candidate.ID] = candidate
		}
	}
	if len(matches) == 1 {
		for id := range matches {
			return id, nil
		}
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("%w: %s %q matched %d objects", formations.ErrAmbiguousSelector, kind, selector, len(matches))
	}
	return "", fmt.Errorf("%w: %s %q", formations.ErrNotFound, kind, selector)
}

func missionByID(board *formations.BoardDocument, missionID string) (formations.MissionNode, bool) {
	for _, mission := range board.Missions {
		if mission.ID == missionID {
			return mission, true
		}
	}
	return formations.MissionNode{}, false
}

func missionReachableChain(board *formations.BoardDocument, missionID string) ([]archonMissionChainNode, []formations.BoardConnection, error) {
	type queueItem struct {
		nodeID string
		depth  int
	}
	seen := map[string]bool{missionID: true}
	queue := []queueItem{{nodeID: missionID}}
	chain := []archonMissionChainNode{}
	connections := []formations.BoardConnection{}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, connection := range board.Connections {
			fromNode, ok := endpointNodeID(connection.From)
			if !ok || fromNode != current.nodeID {
				continue
			}
			toNode, ok := endpointNodeID(connection.To)
			if !ok {
				return nil, nil, fmt.Errorf("%w: malformed connection target %q", formations.ErrNotFound, connection.To)
			}
			connections = append(connections, connection)
			if seen[toNode] {
				continue
			}
			node, ok := chainNodeByID(board, toNode, current.depth+1)
			if !ok {
				return nil, nil, fmt.Errorf("%w: reachable node %q", formations.ErrNotFound, toNode)
			}
			seen[toNode] = true
			chain = append(chain, node)
			queue = append(queue, queueItem{nodeID: toNode, depth: current.depth + 1})
		}
	}
	return chain, connections, nil
}

func chainNodeByID(board *formations.BoardDocument, nodeID string, depth int) (archonMissionChainNode, bool) {
	for _, formation := range board.Formations {
		if formation.ID == nodeID {
			return archonMissionChainNode{
				ID:    formation.ID,
				Kind:  "formation",
				Title: formation.Title,
				Type:  formation.Type,
				Depth: depth,
			}, true
		}
	}
	for _, gate := range board.Gates {
		if gate.ID == nodeID {
			return archonMissionChainNode{
				ID:    gate.ID,
				Kind:  "gate",
				Title: gate.Title,
				Depth: depth,
			}, true
		}
	}
	for _, mission := range board.Missions {
		if mission.ID == nodeID {
			return archonMissionChainNode{
				ID:    mission.ID,
				Kind:  "inputCard",
				Title: mission.Title,
				Depth: depth,
			}, true
		}
	}
	for _, tool := range board.Tools {
		if tool.ID == nodeID {
			return archonMissionChainNode{
				ID:    tool.ID,
				Kind:  "tool",
				Title: tool.Title,
				Depth: depth,
			}, true
		}
	}
	for _, end := range board.Ends {
		if end.ID == nodeID {
			return archonMissionChainNode{
				ID:    end.ID,
				Kind:  "end",
				Title: end.Title,
				Depth: depth,
			}, true
		}
	}
	return archonMissionChainNode{}, false
}

func endpointNodeID(endpoint string) (string, bool) {
	node, _, ok := strings.Cut(endpoint, ":")
	if !ok || node == "" {
		return "", false
	}
	return node, true
}

func slugKey(value string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(value) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case !lastDash:
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func reorderFlags(args []string, boolFlags map[string]bool) []string {
	flags := make([]string, 0, len(args))
	positionals := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positionals = append(positionals, arg)
			continue
		}
		flags = append(flags, arg)
		name := strings.TrimLeft(arg, "-")
		if eq := strings.Index(name, "="); eq >= 0 {
			continue
		}
		if boolFlags[name] {
			continue
		}
		if i+1 < len(args) {
			flags = append(flags, args[i+1])
			i++
		}
	}
	return append(flags, positionals...)
}
