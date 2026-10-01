package formations

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A role is role text: the card names no harness, session, model, effort or
// launch string, since each slot that uses it states what its seat runs.
func TestCreatePersonaWritesRoleTextOnly(t *testing.T) {
	store := NewPersonaStore(t.TempDir())
	store.Now = fixedClock()

	card, err := store.CreatePersona(CreatePersonaRequest{
		ID:           "researcher",
		Kind:         "specialist",
		Capabilities: []string{"research", "go"},
		Personality:  "direct",
	})
	if err != nil {
		t.Fatalf("create persona: %v", err)
	}

	if card.ID != "researcher" {
		t.Fatalf("card.ID = %q, want researcher", card.ID)
	}
	if got := filepath.Base(store.PersonaPath("researcher")); got != "researcher.toml" {
		t.Fatalf("persona path base = %q, want researcher.toml", got)
	}
	if !containsAll(card.Tags, []string{"research", "go", "personality:direct"}) {
		t.Fatalf("tags = %#v, want capabilities and personality facet", card.Tags)
	}

	raw := readFile(t, store.PersonaPath("researcher"))
	want := `schema = 1

[card]
id = "researcher"
display_name = "researcher"
kind = "specialist"
tags = ["research", "go", "personality:direct"]
status = "active"
`
	if raw != want {
		t.Fatalf("persona TOML:\n%s\nwant:\n%s", raw, want)
	}
}

func TestCreatePersonaRefusesExistingIDWithoutClobber(t *testing.T) {
	store := NewPersonaStore(t.TempDir())
	existing := `schema = 1

[card]
id = "scout"
kind = "specialist"
tags = ["research"]
reviewerNotes = "keep this exact line"
`
	writeFixture(t, store.PersonaPath("scout"), existing)

	_, err := store.CreatePersona(CreatePersonaRequest{ID: "scout", Kind: "specialist"})
	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("create existing error = %v, want ErrAlreadyExists", err)
	}
	if got := readFile(t, store.PersonaPath("scout")); got != existing {
		t.Fatalf("existing card changed:\n--- got ---\n%s\n--- want ---\n%s", got, existing)
	}
}

func TestEditPersonaCapabilitiesPreservesUnknownFieldsAndIsIdempotent(t *testing.T) {
	store := NewPersonaStore(t.TempDir())
	writeFixture(t, store.PersonaPath("susie"), `schema = 1

[card]
id = "susie"
display_name = "Susie"
kind = "specialist"
tags = ["design", "react", "taste:visual"]
reviewerNotes = "prefers tight grids"
`)

	editPersonaWithFreshETag(t, store, "susie", EditPersonaRequest{AddCapability: "tailwind"})
	editPersonaWithFreshETag(t, store, "susie", EditPersonaRequest{RemoveCapability: "tailwind"})
	card := editPersonaWithFreshETag(t, store, "susie", EditPersonaRequest{RemoveCapability: "tailwind"})

	if contains(card.Tags, "tailwind") {
		t.Fatalf("tags still contain removed capability: %#v", card.Tags)
	}
	raw := readFile(t, store.PersonaPath("susie"))
	if !strings.Contains(raw, `reviewerNotes = "prefers tight grids"`) {
		t.Fatalf("unknown field was not preserved:\n%s", raw)
	}
}

func TestEditPersonaAppendsNote(t *testing.T) {
	store := NewPersonaStore(t.TempDir())
	store.Now = func() time.Time { return time.Date(2026, 6, 3, 18, 0, 0, 0, time.UTC) }
	writeFixture(t, store.PersonaPath("susie"), minimalPersona("susie", "specialist", []string{"design"}))

	card := editPersonaWithFreshETag(t, store, "susie", EditPersonaRequest{Note: "react quality improved over sprint 3"})
	if len(card.Notes) != 1 || card.Notes[0].Text != "react quality improved over sprint 3" || card.Notes[0].Timestamp != "2026-06-03T18:00:00Z" {
		t.Fatalf("notes = %#v", card.Notes)
	}
}

func TestEditPersonaRequiresPreconditionBeforeWriting(t *testing.T) {
	store := NewPersonaStore(t.TempDir())
	writeFixture(t, store.PersonaPath("susie"), minimalPersona("susie", "specialist", []string{"design"}))
	before := readFile(t, store.PersonaPath("susie"))

	_, err := store.EditPersona("susie", EditPersonaRequest{AddCapability: "react"})
	if !errors.Is(err, ErrPreconditionRequired) {
		t.Fatalf("edit without expected ETag error = %v, want ErrPreconditionRequired", err)
	}
	if got := readFile(t, store.PersonaPath("susie")); got != before {
		t.Fatalf("card changed despite missing precondition:\n%s", got)
	}

	fresh, err := store.ReadPersona("susie")
	if err != nil {
		t.Fatalf("read persona: %v", err)
	}
	updated, err := store.EditPersona("susie", EditPersonaRequest{AddCapability: "react", ExpectedETag: fresh.ETag})
	if err != nil {
		t.Fatalf("edit with matching ETag: %v", err)
	}
	if !contains(updated.Tags, "react") || updated.ETag == fresh.ETag {
		t.Fatalf("updated card = %+v, want react and changed ETag", updated)
	}
	afterMatch := readFile(t, store.PersonaPath("susie"))

	_, err = store.EditPersona("susie", EditPersonaRequest{AddCapability: "go", ExpectedETag: fresh.ETag})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("edit with stale ETag error = %v, want ErrConflict", err)
	}
	if got := readFile(t, store.PersonaPath("susie")); got != afterMatch {
		t.Fatalf("stale edit changed card:\n%s", got)
	}
}

func TestAgentRosterLeftJoinsCardsWithLiveSessions(t *testing.T) {
	cards := []PersonaCard{
		mustParsePersonaFixture(t, "susie", minimalPersona("susie", "specialist", []string{"design", "react"})),
		mustParsePersonaFixture(t, "codex", minimalPersona("codex", "specialist", []string{"go", "fast"})),
	}
	live := []LiveAgentSession{
		{Name: "susie", Status: "idle", Attached: true},
		{Name: "scratch", Status: "working"},
	}

	roster, err := ProjectAgentRoster(cards, live, AgentRosterFilter{})
	if err != nil {
		t.Fatalf("project roster: %v", err)
	}
	if got := roster.ByID("susie").Liveness; got != AgentLivenessLive {
		t.Fatalf("susie liveness = %q, want live", got)
	}
	if got := roster.ByID("codex").Liveness; got != AgentLivenessOffline {
		t.Fatalf("codex liveness = %q, want offline", got)
	}
	scratch := roster.ByID("scratch")
	if scratch == nil || !scratch.Unbound || scratch.Assignable {
		t.Fatalf("scratch projection = %#v, want visible unbound and not assignable", scratch)
	}

	assignable, err := ProjectAgentRoster(cards, live, AgentRosterFilter{AssignableOnly: true})
	if err != nil {
		t.Fatalf("project assignable roster: %v", err)
	}
	if assignable.ByID("scratch") != nil {
		t.Fatalf("unbound scratch should be excluded from assignable roster: %#v", assignable)
	}
}

func TestAgentRosterFiltersBareCapabilitiesOnly(t *testing.T) {
	cards := []PersonaCard{
		mustParsePersonaFixture(t, "susie", minimalPersona("susie", "specialist", []string{"react", "taste:visual"})),
		mustParsePersonaFixture(t, "codex", minimalPersona("codex", "specialist", []string{"typescript"})),
	}

	roster, err := ProjectAgentRoster(cards, nil, AgentRosterFilter{Capable: "react"})
	if err != nil {
		t.Fatalf("project roster: %v", err)
	}
	if roster.ByID("susie") == nil {
		t.Fatal("susie missing from react-capable roster")
	}
	if roster.ByID("codex") != nil {
		t.Fatal("codex included in react-capable roster")
	}

	roster, err = ProjectAgentRoster(cards, nil, AgentRosterFilter{Capable: "taste:visual"})
	if err != nil {
		t.Fatalf("project roster by facet: %v", err)
	}
	if len(roster.Agents) != 0 {
		t.Fatalf("facet tag matched as capability: %#v", roster.Agents)
	}
}

// A role's own session is the one named after it.
func TestResolveAgentSessionFindsTheSessionNamedAfterTheRole(t *testing.T) {
	card := mustParsePersonaFixture(t, "susie", minimalPersona("susie", "specialist", []string{"design"}))
	binding, err := ResolveAgentSession(card, []LiveAgentSession{{Name: "claude-susie"}, {Name: "susie", Status: "working"}})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if binding.AgentID != "susie" || binding.SessionStem != "susie" || binding.Session.Name != "susie" || binding.Session.Status != "working" {
		t.Fatalf("binding = %#v", binding)
	}
}

func TestResolveAgentSessionFailsLoudForOfflineOrDuplicateLiveMatches(t *testing.T) {
	card := mustParsePersonaFixture(t, "scout", minimalPersona("scout", "specialist", []string{"research"}))

	_, err := ResolveAgentSession(card, nil)
	if !errors.Is(err, ErrAgentSessionOffline) {
		t.Fatalf("offline resolve error = %v, want ErrAgentSessionOffline", err)
	}

	_, err = ResolveAgentSession(card, []LiveAgentSession{{Name: "scout"}, {Name: "scout"}})
	if !errors.Is(err, ErrAmbiguousAgentBinding) {
		t.Fatalf("duplicate live resolve error = %v, want ErrAmbiguousAgentBinding", err)
	}
}

func mustParsePersonaFixture(t *testing.T, id, raw string) PersonaCard {
	t.Helper()
	card, err := parsePersonaCard(id, []byte(raw))
	if err != nil {
		t.Fatalf("parse persona fixture %s: %v", id, err)
	}
	return *card
}

func editPersonaWithFreshETag(t *testing.T, store *PersonaStore, id string, req EditPersonaRequest) *PersonaCard {
	t.Helper()
	before, err := store.ReadPersona(id)
	if err != nil {
		t.Fatalf("read persona %s: %v", id, err)
	}
	req.ExpectedETag = before.ETag
	card, err := store.EditPersona(id, req)
	if err != nil {
		t.Fatalf("edit persona %s: %v", id, err)
	}
	return card
}

func minimalPersona(id, kind string, tags []string) string {
	return `schema = 1

[card]
id = "` + id + `"
display_name = "` + strings.Title(id) + `"
kind = "` + kind + `"
tags = [` + renderStringList(tags) + `]
`
}

func containsAll(values, wants []string) bool {
	for _, want := range wants {
		if !contains(values, want) {
			return false
		}
	}
	return true
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestPersonaPathValidationRejectsNonSlugIDs(t *testing.T) {
	store := NewPersonaStore(t.TempDir())
	for _, id := range []string{"", "../scout", "Scout", "scout.toml", "scout/slash", "bad id", "agent:one", "_hidden", "-bad", "bad-"} {
		if _, err := store.CreatePersona(CreatePersonaRequest{ID: id, Kind: "specialist"}); !errors.Is(err, ErrInvalidSlug) {
			t.Fatalf("CreatePersona(%q) error = %v, want ErrInvalidSlug", id, err)
		}
	}
	entries, err := os.ReadDir(store.AgentsDir)
	if err != nil {
		t.Fatalf("read temp agents dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("invalid create unexpectedly wrote files: %#v", entries)
	}
}
