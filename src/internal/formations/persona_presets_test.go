package formations

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
)

// Every Archon has the generic roles Perttu confirmed (archon-o7p.12.1), each
// role text only, with no mission-specific role among them.
func TestBuiltinRolesAreTheGenericRoles(t *testing.T) {
	store := NewPersonaStore(t.TempDir())
	cards, err := store.ListPersonas()
	if err != nil {
		t.Fatalf("list presets: %v", err)
	}
	gotIDs := make([]string, 0, len(cards))
	for _, card := range cards {
		gotIDs = append(gotIDs, card.ID)
		if !card.Preset || card.Customized || card.Summary == "" || card.DisplayName == "" || card.Status != "active" {
			t.Fatalf("preset projection = %+v", card)
		}
	}
	wantIDs := []string{"builder", "debugger", "judge", "orchestrator", "planner", "reviewer", "scout"}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("preset ids = %v, want %v", gotIDs, wantIDs)
	}
	for _, id := range []string{"codex-builder", "delivery-worker", "delivery-final-reviewer"} {
		if _, err := store.ReadPersona(id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("ReadPersona(%q) = %v, want ErrNotFound", id, err)
		}
	}
}

// Each built-in role's kind is one the effort policy names, so staffing it
// suggests an effort.
func TestEveryBuiltinRoleKindIsOnTheEffortPolicy(t *testing.T) {
	named := map[string]string{}
	for _, entry := range EffortPolicy() {
		for _, kind := range entry.Kinds {
			named[kind] = entry.Effort
		}
	}
	want := map[string]string{"scout": "low", "planner": "xhigh", "builder": "medium", "judge": "xhigh", "orchestrator": "xhigh", "debugger": "medium", "reviewer": "xhigh"}
	for _, preset := range personaPresetCatalog {
		if named[preset.Kind] != want[preset.ID] {
			t.Fatalf("%s (%s) suggests %q, want %q", preset.ID, preset.Kind, named[preset.Kind], want[preset.ID])
		}
	}
}

func TestEditingABuiltinRoleMaterializesLocalOverride(t *testing.T) {
	dir := t.TempDir()
	store := NewPersonaStore(dir)
	builtin, err := store.ReadPersona("builder")
	if err != nil {
		t.Fatalf("read builtin: %v", err)
	}
	name := "Repository Builder"
	summary := "Builds in the selected workspace"
	capabilities := []string{"implement", "test", "refactor"}
	updated, err := store.EditPersona("builder", EditPersonaRequest{
		SetDisplayName:  &name,
		SetSummary:      &summary,
		SetCapabilities: &capabilities,
		ExpectedETag:    builtin.ETag,
	})
	if err != nil {
		t.Fatalf("edit builtin: %v", err)
	}
	if !updated.Preset || !updated.Customized || updated.DisplayName != name || updated.Summary != summary {
		t.Fatalf("updated preset = %+v", updated)
	}
	if !reflect.DeepEqual(bareCapabilities(updated.Tags), capabilities) {
		t.Fatalf("capabilities = %v, want %v", updated.Tags, capabilities)
	}
	if _, err := os.Stat(store.PersonaPath("builder")); err != nil {
		t.Fatalf("materialized override: %v", err)
	}
	reread, err := NewPersonaStore(dir).ReadPersona("builder")
	if err != nil || !reread.Customized || reread.ETag != updated.ETag {
		t.Fatalf("reread override = %+v, err %v", reread, err)
	}
}

func TestStaleBuiltinRoleEditDoesNotMaterializeOverride(t *testing.T) {
	store := NewPersonaStore(t.TempDir())
	name := "Stale"
	_, err := store.EditPersona("scout", EditPersonaRequest{SetDisplayName: &name, ExpectedETag: "stale"})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("stale edit error = %v, want ErrConflict", err)
	}
	if _, err := os.Stat(store.PersonaPath("scout")); !os.IsNotExist(err) {
		t.Fatalf("stale edit created override: %v", err)
	}
}

// A role card or the agents directory may be a symlink; a FIFO card is refused.
func TestPersonaStoreFollowsSymlinkedCardsAndRefusesFIFOCards(t *testing.T) {
	dir := t.TempDir()
	external := filepath.Join(t.TempDir(), "external.toml")
	externalRaw := renderPersona(CreatePersonaRequest{
		ID:          "builder",
		DisplayName: "Substituted Builder",
		Kind:        "builder",
		Summary:     "external secret",
	}, []string{"implement"})
	if err := os.WriteFile(external, []byte(externalRaw), 0o600); err != nil {
		t.Fatalf("write external file: %v", err)
	}
	if err := os.Symlink(external, filepath.Join(dir, "builder.toml")); err != nil {
		t.Fatalf("symlink card: %v", err)
	}
	store := NewPersonaStore(dir)
	if card, err := store.ReadPersona("builder"); err != nil || card.Summary != "external secret" {
		t.Fatalf("ReadPersona through a symlinked card = %+v, %v", card, err)
	}
	linkedDir := filepath.Join(t.TempDir(), "agents")
	if err := os.Symlink(dir, linkedDir); err != nil {
		t.Fatal(err)
	}
	if card, err := NewPersonaStore(linkedDir).ReadPersona("builder"); err != nil || card.Summary != "external secret" {
		t.Fatalf("ReadPersona through a symlinked agents directory = %+v, %v", card, err)
	}

	if err := os.Remove(filepath.Join(dir, "builder.toml")); err != nil {
		t.Fatalf("remove card symlink: %v", err)
	}
	fifo := filepath.Join(dir, "builder.toml")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}
	fifoHandle, err := os.OpenFile(fifo, os.O_RDWR|syscall.O_NONBLOCK, 0)
	if err != nil {
		t.Fatalf("open FIFO keeper: %v", err)
	}
	fifoReader, err := os.OpenFile(fifo, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		_ = fifoHandle.Close()
		t.Fatalf("open FIFO reader keeper: %v", err)
	}
	defer fifoReader.Close()
	if _, err := fifoHandle.Write([]byte(externalRaw)); err != nil {
		_ = fifoHandle.Close()
		t.Fatalf("prime FIFO with valid persona: %v", err)
	}
	if err := fifoHandle.Close(); err != nil {
		t.Fatalf("close FIFO writer keeper: %v", err)
	}
	if _, err := store.ReadPersona("builder"); err == nil {
		t.Fatal("ReadPersona accepted FIFO card")
	}
}

// Editing a role card symlinked from elsewhere, such as a dotfiles
// repository, writes through the link and keeps it (archon-4m4j).
func TestPersonaStoreWritesASymlinkedCardThroughItsLink(t *testing.T) {
	dir := t.TempDir()
	repository := t.TempDir()
	external := filepath.Join(repository, "builder.toml")
	if err := os.WriteFile(external, []byte(renderPersona(CreatePersonaRequest{
		ID: "linked-builder", DisplayName: "Builder", Kind: "builder",
	}, []string{"implement"})), 0o644); err != nil {
		t.Fatalf("write external card: %v", err)
	}
	link := filepath.Join(dir, "linked-builder.toml")
	if err := os.Symlink(external, link); err != nil {
		t.Fatalf("symlink card: %v", err)
	}
	store := NewPersonaStore(dir)
	card, err := store.ReadPersona("linked-builder")
	if err != nil {
		t.Fatalf("read linked card: %v", err)
	}
	name := "Linked Builder"
	if _, err := store.EditPersona("linked-builder", EditPersonaRequest{SetDisplayName: &name, ExpectedETag: card.ETag}); err != nil {
		t.Fatalf("edit linked card: %v", err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("card after edit: %v %v, want the link kept", info, err)
	}
	raw, err := os.ReadFile(external)
	if err != nil || !strings.Contains(string(raw), `display_name = "Linked Builder"`) {
		t.Fatalf("external card = %q (%v), want the edit written through the link", raw, err)
	}
	if entries, err := os.ReadDir(repository); err != nil || len(entries) != 1 {
		t.Fatalf("repository holds %v (%v), want only the card", entries, err)
	}
}

func TestPersonaStoreRefusesAFIFOLock(t *testing.T) {
	dir := t.TempDir()
	store := NewPersonaStore(dir)
	builtin, err := store.ReadPersona("scout")
	if err != nil {
		t.Fatalf("read builtin: %v", err)
	}
	lockPath := filepath.Join(dir, "scout.toml.lock")
	if err := syscall.Mkfifo(lockPath, 0o600); err != nil {
		t.Fatalf("mkfifo lock: %v", err)
	}
	name := "Override"
	if _, err := store.EditPersona("scout", EditPersonaRequest{SetDisplayName: &name, ExpectedETag: builtin.ETag}); err == nil {
		t.Fatal("EditPersona accepted FIFO lock")
	}
	if _, err := os.Stat(store.PersonaPath("scout")); !os.IsNotExist(err) {
		t.Fatalf("FIFO lock edit materialized card: %v", err)
	}
}

func bareCapabilities(tags []string) []string {
	result := make([]string, 0, len(tags))
	for _, tag := range tags {
		if isBareCapability(tag) {
			result = append(result, tag)
		}
	}
	return result
}
