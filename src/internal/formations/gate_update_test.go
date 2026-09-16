package formations

import (
	"errors"
	"strings"
	"testing"
)

func updateGateFixture(t *testing.T) (*Store, func() WriteOptions) {
	t.Helper()
	store := NewStore(t.TempDir())
	store.Now = fixedClock()
	writeFixture(t, store.BoardPath("session-search"), s4JudgeChainRunBoardFixture())
	writeFixture(t, store.LayoutPath("session-search"), `schema = 1
boardId = "brd_01J9_sesssearch"
boardRev = 7

[[node]]
id = "gate_review"
x = 640
y = 180
`)
	return store, func() WriteOptions {
		t.Helper()
		board, err := store.ReadBoard("session-search")
		if err != nil {
			t.Fatalf("read board: %v", err)
		}
		return WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev}
	}
}

func TestUpdateGateSetsAndClearsEachField(t *testing.T) {
	store, current := updateGateFixture(t)
	update := func(req GateUpdateRequest) GateNode {
		t.Helper()
		req.GateID = "gate_review"
		board, err := store.UpdateGate("session-search", req, current())
		if err != nil {
			t.Fatalf("update %+v: %v", req, err)
		}
		reloaded, err := store.ReadBoard("session-search")
		if err != nil || reloaded.ETag != board.ETag {
			t.Fatalf("reload after update = %v, want the written board", err)
		}
		gate, ok := findGate(reloaded.Gates, "gate_review")
		if !ok {
			t.Fatal("gate disappeared")
		}
		return gate
	}

	gate := update(GateUpdateRequest{Title: stringPtr("Framing review"), Criterion: stringPtr("The goal is framed")})
	if gate.Title != "Framing review" || gate.Criterion != "The goal is framed" || gate.Check != "output_contains" || !equalStrings(gate.Kinds, []string{"code", "formation"}) {
		t.Fatalf("title/criterion update = %+v, want other fields untouched", gate)
	}
	gate = update(GateUpdateRequest{Title: stringPtr(""), Criterion: stringPtr("")})
	if gate.Title != "" || gate.Criterion != "" {
		t.Fatalf("cleared title/criterion = %+v", gate)
	}
	gate = update(GateUpdateRequest{CheckValue: stringPtr("")})
	if gate.Check != "output_contains" || gate.CheckVersion != "1" || gate.CheckValue != "" {
		t.Fatalf("cleared check value = %+v, want profile kept", gate)
	}
	gate = update(GateUpdateRequest{Check: stringPtr("output_absent"), CheckValue: stringPtr("TODO")})
	if gate.Check != "output_absent" || gate.CheckVersion != "1" || gate.CheckValue != "TODO" {
		t.Fatalf("changed check = %+v", gate)
	}
	gate = update(GateUpdateRequest{Check: stringPtr(""), CheckVersion: stringPtr(""), CheckValue: stringPtr("")})
	if gate.Check != "" || gate.CheckVersion != "" || gate.CheckValue != "" {
		t.Fatalf("cleared check = %+v", gate)
	}
	raw := readFile(t, store.BoardPath("session-search"))
	if strings.Contains(raw, "check =") || strings.Contains(raw, "checkVersion") || strings.Contains(raw, "checkValue") {
		t.Fatalf("cleared check left keys behind:\n%s", raw)
	}
}

func TestUpdateGateConvertsCodeGateToHuman(t *testing.T) {
	store := NewStore(t.TempDir())
	store.Now = fixedClock()
	// The shape of Wayfinding's "Review gate": a code gate whose check value holds prose.
	writeFixture(t, store.BoardPath("session-search"), s4MissionOnlyBoardFixture()+`
[[gate]]
id = "gate_review"
title = "Review gate"
kinds = ["code"]
criterion = "This gate is about getting a human review on the framing of the goal"
check = "output_absent"
checkVersion = "1"
checkValue = "we have no forbidden text"

[[connection]]
id = "edge_start"
from = "mis_showcase:out"
to = "gate_review:in"
`)
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := store.UpdateGate("session-search", GateUpdateRequest{GateID: "gate_review", Kinds: []string{"human"}}, WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev})
	if err != nil {
		t.Fatalf("convert to human: %v", err)
	}
	gate, _ := findGate(updated.Gates, "gate_review")
	if !equalStrings(gate.Kinds, []string{"human"}) || gate.Check != "" || gate.CheckVersion != "" || gate.CheckValue != "" ||
		gate.Title != "Review gate" || !strings.HasPrefix(gate.Criterion, "This gate is about") || len(updated.Connections) != 1 {
		t.Fatalf("converted gate = %+v connections %+v, want human gate with check cleared and edges kept", gate, updated.Connections)
	}
	if report := ValidateBoard(updated); len(findBoardFindings(report.Errors, FindingGateNotRoutable)) != 0 {
		t.Fatalf("human gate still unroutable: %+v", report.Errors)
	}

	blank := ""
	explicit := GateUpdateRequest{GateID: "gate_review", Kinds: []string{"code"}, Check: &blank, CheckVersion: &blank, CheckValue: &blank}
	if _, err := store.UpdateGate("session-search", explicit, WriteOptions{ExpectedETag: updated.ETag, ExpectedRev: updated.Rev}); err != nil {
		t.Fatalf("explicit clears with kinds: %v", err)
	}
}

func TestUpdateGateKindChangesKeepJudgeChainConsistent(t *testing.T) {
	store, current := updateGateFixture(t)
	before, _ := store.ReadBoard("session-search")
	layoutBefore := readFile(t, store.LayoutPath("session-search"))

	dropped, err := store.UpdateGate("session-search", GateUpdateRequest{GateID: "gate_review", Kinds: []string{"code", "human"}}, current())
	if err != nil {
		t.Fatalf("drop formation kind: %v", err)
	}
	for _, connection := range dropped.Connections {
		if strings.HasSuffix(connection.From, ":judge") || strings.HasSuffix(connection.To, ":judge") {
			t.Fatalf("judge connection %+v survived dropping the formation kind", connection)
		}
	}
	if len(dropped.Connections) != len(before.Connections)-2 {
		t.Fatalf("connections after detaching = %+v, want only the two judge edges removed", dropped.Connections)
	}
	gate, _ := findGate(dropped.Gates, "gate_review")
	if !equalStrings(gate.Kinds, []string{"code", "human"}) || gate.CheckValue != "output from" {
		t.Fatalf("gate after dropping formation = %+v, want code check kept", gate)
	}
	if got := readFile(t, store.LayoutPath("session-search")); got != layoutBefore {
		t.Fatalf("gate update changed layout:\n%s", got)
	}

	added, err := store.UpdateGate("session-search", GateUpdateRequest{GateID: "gate_review", Kinds: []string{"human", "formation"}}, current())
	if err != nil {
		t.Fatalf("add formation kind: %v", err)
	}
	gate, _ = findGate(added.Gates, "gate_review")
	if gate.Check != "" || gate.CheckValue != "" {
		t.Fatalf("dropping code kept its check: %+v", gate)
	}
	if !hasBoardFinding(ValidateBoard(added).Errors, "gate_review", "a judge chain") {
		t.Fatalf("formation kind without a chain is not reported: %+v", ValidateBoard(added).Errors)
	}
}

func TestGateAuthoringRejectsMalformedKindsAndChecksWithoutMutation(t *testing.T) {
	store, current := updateGateFixture(t)
	before := readFile(t, store.BoardPath("session-search"))
	check := "output_contains"
	unknownVersion := "999"
	for name, test := range map[string]struct {
		write func() error
		want  error
	}{
		"empty kinds": {func() error {
			_, err := store.UpdateGate("session-search", GateUpdateRequest{GateID: "gate_review", Kinds: []string{}}, current())
			return err
		}, ErrInvalidGateKind},
		"unknown kind on update": {func() error {
			_, err := store.UpdateGate("session-search", GateUpdateRequest{GateID: "gate_review", Kinds: []string{"human", "robot"}}, current())
			return err
		}, ErrInvalidGateKind},
		"unknown kind on create": {func() error {
			_, err := store.CreateGate("session-search", GateCreateRequest{Kinds: []string{"robot"}}, current())
			return err
		}, ErrInvalidGateKind},
		"check without code kind": {func() error {
			_, err := store.UpdateGate("session-search", GateUpdateRequest{GateID: "gate_review", Kinds: []string{"human"}, Check: &check}, current())
			return err
		}, ErrInvalidCodeGateProfile},
		"unknown check version": {func() error {
			_, err := store.UpdateGate("session-search", GateUpdateRequest{GateID: "gate_review", CheckVersion: &unknownVersion}, current())
			return err
		}, ErrInvalidCodeGateProfile},
		"missing gate": {func() error {
			_, err := store.UpdateGate("session-search", GateUpdateRequest{GateID: "gate_missing", Title: &check}, current())
			return err
		}, ErrNotFound},
	} {
		if err := test.write(); !errors.Is(err, test.want) {
			t.Errorf("%s error = %v, want %v", name, err, test.want)
		}
		if after := readFile(t, store.BoardPath("session-search")); after != before {
			t.Fatalf("%s changed the board:\n%s", name, after)
		}
	}
}
