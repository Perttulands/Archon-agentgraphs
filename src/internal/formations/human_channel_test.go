package formations

import (
	"errors"
	"strings"
	"testing"
)

func TestMissionHumanChannelRoundTripsAndStoresNotifyAsAbsent(t *testing.T) {
	store := NewStore(t.TempDir())
	store.Now = fixedClock()
	if _, err := store.CreateBoard(BoardCreateRequest{Slug: "channel", Title: "Channel"}); err != nil {
		t.Fatal(err)
	}
	current := func() (*BoardDocument, WriteOptions) {
		t.Helper()
		board, err := store.ReadBoard("channel")
		if err != nil {
			t.Fatal(err)
		}
		return board, WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev}
	}
	channelOf := func(missionID string) string {
		t.Helper()
		board, _ := current()
		mission, ok := findMission(board, missionID)
		if !ok {
			t.Fatalf("mission %s missing", missionID)
		}
		return mission.HumanChannel
	}

	_, opts := current()
	created, err := store.CreateMission("channel", MissionCreateRequest{Title: "Talk", HumanChannel: " session "}, opts)
	if err != nil || created.Mission.HumanChannel != HumanChannelSession {
		t.Fatalf("create with session: %+v, %v", created, err)
	}
	_, opts = current()
	plain, err := store.CreateMission("channel", MissionCreateRequest{Title: "Mail", HumanChannel: HumanChannelNotify}, opts)
	if err != nil {
		t.Fatal(err)
	}
	if raw := readFile(t, store.BoardPath("channel")); strings.Count(raw, "humanChannel") != 1 || !strings.Contains(raw, `humanChannel = "session"`) {
		t.Fatalf("only the session mission stores a channel:\n%s", raw)
	}
	if channelOf(created.Mission.ID) != HumanChannelSession || channelOf(plain.Mission.ID) != "" {
		t.Fatalf("read back: talk %q, mail %q", channelOf(created.Mission.ID), channelOf(plain.Mission.ID))
	}

	update := func(missionID, channel string) error {
		t.Helper()
		_, opts := current()
		_, err := store.UpdateMission("channel", MissionUpdateRequest{MissionID: missionID, HumanChannel: &channel}, opts)
		return err
	}
	for _, clear := range []string{HumanChannelNotify, ""} {
		if err := update(created.Mission.ID, clear); err != nil {
			t.Fatalf("set %q: %v", clear, err)
		}
		if raw := readFile(t, store.BoardPath("channel")); strings.Contains(raw, "humanChannel") || channelOf(created.Mission.ID) != "" {
			t.Fatalf("%q leaves the key:\n%s", clear, raw)
		}
		if err := update(created.Mission.ID, HumanChannelSession); err != nil || channelOf(created.Mission.ID) != HumanChannelSession {
			t.Fatalf("set session again: %q, %v", channelOf(created.Mission.ID), err)
		}
	}

	before, opts := current()
	if err := update(created.Mission.ID, "email"); !errors.Is(err, ErrInvalidHumanChannel) || !strings.Contains(err.Error(), "must be notify or session") {
		t.Fatalf("unknown channel on update: %v", err)
	}
	if _, err := store.CreateMission("channel", MissionCreateRequest{Title: "Nope", HumanChannel: "email"}, opts); !errors.Is(err, ErrInvalidHumanChannel) {
		t.Fatalf("unknown channel on create: %v", err)
	}
	if after, _ := current(); after.Rev != before.Rev || after.TOML != before.TOML {
		t.Fatalf("a rejected channel saved the board: rev %d -> %d", before.Rev, after.Rev)
	}
}

func TestMissionHumanChannelWrittenByHandDecodesAndValidates(t *testing.T) {
	raw := s4MissionOnlyBoardFixture() + `humanChannel = "notify"

[[mission]]
id = "mis_other"
title = "Other"
goal = ""
beadId = ""
humanChannel = "email"
`
	board, err := parseBoard([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	compat := parseMissionNodes([]byte(raw))
	for label, missions := range map[string][]MissionNode{"decoder": board.Missions, "compatibility parser": compat} {
		if len(missions) != 2 || missions[0].HumanChannel != "" || missions[1].HumanChannel != "email" {
			t.Fatalf("%s: channels %+v, want notify stored as empty and email kept for validation", label, missions)
		}
	}
	var found []BoardFinding
	for _, finding := range ValidateBoard(board).Errors {
		if finding.Code == FindingInvalidHumanChannel {
			found = append(found, finding)
		}
	}
	if len(found) != 1 || found[0].NodeID != "mis_other" || !strings.Contains(found[0].Message, "notify or session") {
		t.Fatalf("human channel findings = %+v", found)
	}
}

func TestRunFreezesTheMissionHumanChannel(t *testing.T) {
	store, personas := s4RunFixture(t)
	store.Now = fixedClock()
	personas.Now = fixedClock()
	createS4Persona(t, personas, "scout")
	fixture := strings.Replace(s5HumanGateBoardFixture(), `beadId = "home-7kc4.7"`+"\n", `beadId = "home-7kc4.7"`+"\nhumanChannel = \"session\"\n", 1)
	writeFixture(t, store.BoardPath("session-search"), fixture)
	board, err := store.ReadBoard("session-search")
	if err != nil || board.Missions[0].HumanChannel != HumanChannelSession {
		t.Fatalf("board mission %+v, %v", board.Missions, err)
	}
	engine := NewRunEngine(store, personas, &fakeRunExecutor{})
	status, err := engine.RunMission("session-search", RunStartRequest{
		MissionID:         "mis_showcase",
		Actor:             "agent:test",
		ExpectedBoardETag: board.ETag,
		ExpectedBoardRev:  board.Rev,
		Limits:            RunLimits{MaxDispatch: 5, MaxAttempts: 2},
	})
	if err != nil {
		t.Fatalf("run mission: %v", err)
	}
	notify := HumanChannelNotify
	if _, err := store.UpdateMission("session-search", MissionUpdateRequest{MissionID: "mis_showcase", HumanChannel: &notify}, WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev}); err != nil {
		t.Fatalf("change the draft's channel: %v", err)
	}
	frozen, err := store.ReadRunBoard(status.RunID)
	if err != nil {
		t.Fatal(err)
	}
	draft, _ := store.ReadBoard("session-search")
	if frozen.Missions[0].HumanChannel != HumanChannelSession || draft.Missions[0].HumanChannel != "" {
		t.Fatalf("frozen channel %q, draft channel %q; want the run to keep session", frozen.Missions[0].HumanChannel, draft.Missions[0].HumanChannel)
	}
}
