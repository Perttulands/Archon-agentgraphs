package formations

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	ErrAgentSessionOffline   = errors.New("agent session offline")
	ErrAlreadyExists         = errors.New("archon file already exists")
	ErrAmbiguousAgentBinding = errors.New("ambiguous agent binding")
)

// DefaultPersonaKind fills a blank role so a sketched persona still saves.
const DefaultPersonaKind = "specialist"

const (
	AgentLivenessLive      = "live"
	AgentLivenessOffline   = "offline"
	AgentLivenessAmbiguous = "ambiguous"
)

type PersonaStore struct {
	AgentsDir string
	Now       func() time.Time
}

// PersonaCard is a role: role text a slot may carry. It names no harness,
// model or effort; each slot that uses it states those, and agent spawn states
// them for the role's own session, which is named after the role.
type PersonaCard struct {
	Schema      int           `json:"schema"`
	ID          string        `json:"id"`
	DisplayName string        `json:"displayName,omitempty"`
	Kind        string        `json:"kind"`
	Summary     string        `json:"summary,omitempty"`
	Tags        []string      `json:"tags"`
	Status      string        `json:"status,omitempty"`
	Notes       []PersonaNote `json:"notes,omitempty"`
	ETag        string        `json:"etag"`
	TOML        string        `json:"toml,omitempty"`
	Preset      bool          `json:"preset,omitempty"`
	Customized  bool          `json:"customized,omitempty"`
}

// HarnessVariant is what a session starts on: the harness, the session stem
// and the model and effort it runs with, from the slot a seat staffs
// (SlotSettings.Variant) or from agent spawn's flags.
type HarnessVariant struct {
	ID          string `json:"id"`
	SessionStem string `json:"sessionStem,omitempty"`
	Model       string `json:"model,omitempty"`
	Effort      string `json:"effort,omitempty"`
}

type PersonaNote struct {
	Timestamp string `json:"ts"`
	Actor     string `json:"actor"`
	Text      string `json:"text"`
}

type CreatePersonaRequest struct {
	ID           string
	DisplayName  string
	Kind         string
	Summary      string
	Capabilities []string
	Personality  string
}

type EditPersonaRequest struct {
	AddCapability    string
	RemoveCapability string
	Note             string
	// SetRetired retires the role (true) or brings it back (false).
	SetRetired      *bool
	ExpectedETag    string
	SetDisplayName  *string
	SetKind         *string
	SetSummary      *string
	SetCapabilities *[]string
}

type AgentRosterFilter struct {
	Capable        string
	AssignableOnly bool
}

type LiveAgentSession struct {
	Name       string `json:"name"`
	Status     string `json:"status,omitempty"`
	ContextPct int    `json:"contextPct,omitempty"`
	BeadID     string `json:"beadId,omitempty"`
	Attached   bool   `json:"attached"`
}

// ParseTmuxSessionList reads `list-sessions -F "#{session_name}:#{session_attached}"`
// output as live agent sessions.
func ParseTmuxSessionList(output string) []LiveAgentSession {
	live := []LiveAgentSession{}
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		parts := strings.SplitN(line, ":", 2)
		if parts[0] == "" {
			continue
		}
		live = append(live, LiveAgentSession{Name: parts[0], Status: "live", Attached: len(parts) == 2 && parts[1] == "1"})
	}
	return live
}

// TmuxHasNoServer reports tmux diagnostics that mean no sessions exist.
func TmuxHasNoServer(diagnostic string) bool {
	return strings.Contains(diagnostic, "no server running") ||
		strings.Contains(diagnostic, "No such file or directory") ||
		strings.Contains(diagnostic, "server exited unexpectedly")
}

type AgentRoster struct {
	Agents []AgentProjection `json:"agents"`
}

type AgentProjection struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"displayName,omitempty"`
	Kind        string   `json:"kind,omitempty"`
	Summary     string   `json:"summary,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Liveness    string   `json:"liveness"`
	SessionID   string   `json:"sessionId,omitempty"`
	Status      string   `json:"status,omitempty"`
	ContextPct  int      `json:"contextPct,omitempty"`
	BeadID      string   `json:"beadId,omitempty"`
	Attached    bool     `json:"attached"`
	Assignable  bool     `json:"assignable"`
	Unbound     bool     `json:"unbound,omitempty"`
	Preset      bool     `json:"preset,omitempty"`
	Customized  bool     `json:"customized,omitempty"`
}

// AgentSessionBinding is a role's own live session, the one named after it.
type AgentSessionBinding struct {
	AgentID     string           `json:"agentId"`
	SessionStem string           `json:"sessionStem"`
	Session     LiveAgentSession `json:"session"`
}

func NewPersonaStore(agentsDir string) *PersonaStore {
	return &PersonaStore{
		AgentsDir: agentsDir,
		Now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func DefaultAgentsDir() string {
	if dir := strings.TrimSpace(os.Getenv("ARCHON_AGENTS_DIR")); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "agents"
	}
	return filepath.Join(home, "agents")
}

func (s *PersonaStore) PersonaPath(id string) string {
	return filepath.Join(s.AgentsDir, id+".toml")
}

func (s *PersonaStore) ListPersonas() ([]PersonaCard, error) {
	cards, _, err := s.ListPersonasSkipping()
	return cards, err
}

// ListPersonasSkipping lists every card it can read and names the ones it
// could not, such as a card symlinked from a repository whose file has moved,
// so one bad card never hides the roster (archon-4m4j).
func (s *PersonaStore) ListPersonasSkipping() ([]PersonaCard, []Unreadable, error) {
	cardsByID := make(map[string]PersonaCard, len(personaPresetCatalog))
	for _, card := range builtinPresetPersonas() {
		cardsByID[card.ID] = card
	}
	entries, err := s.listPersonaEntries()
	if err != nil {
		return nil, nil, err
	}
	var unreadable []Unreadable
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".toml") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".toml")
		if _, linked, err := followLink(filepath.Join(s.AgentsDir, entry.Name())); linked && err != nil {
			unreadable = append(unreadable, Unreadable{Name: id, Reason: err.Error()})
			continue
		}
		card, err := s.ReadPersona(id)
		if err != nil {
			unreadable = append(unreadable, Unreadable{Name: id, Reason: err.Error()})
			continue
		}
		cardsByID[id] = *card
	}
	cards := make([]PersonaCard, 0, len(cardsByID))
	for _, card := range cardsByID {
		cards = append(cards, card)
	}
	sort.Slice(cards, func(i, j int) bool {
		return cards[i].ID < cards[j].ID
	})
	sort.Slice(unreadable, func(i, j int) bool {
		return unreadable[i].Name < unreadable[j].Name
	})
	return cards, unreadable, nil
}

func (s *PersonaStore) ReadPersona(id string) (*PersonaCard, error) {
	if err := validatePersonaID(id); err != nil {
		return nil, err
	}
	raw, err := s.readPersonaRaw(id)
	if err != nil {
		if os.IsNotExist(err) {
			if preset, ok := builtinPresetPersona(id); ok {
				return preset, nil
			}
			return nil, ErrNotFound
		}
		return nil, err
	}
	card, err := parsePersonaCard(id, raw)
	if err != nil {
		return nil, err
	}
	if _, ok := builtinPresetPersona(id); ok {
		card.Preset = true
		card.Customized = true
	}
	return card, nil
}

func (s *PersonaStore) CreatePersona(req CreatePersonaRequest) (*PersonaCard, error) {
	if err := validatePersonaID(req.ID); err != nil {
		return nil, err
	}
	if _, ok := builtinPresetPersona(req.ID); ok {
		return nil, ErrAlreadyExists
	}
	var created *PersonaCard
	err := s.withPersonaLock(req.ID, func() error {
		exists, err := s.personaExists(req.ID)
		if err != nil {
			return err
		}
		if exists {
			return ErrAlreadyExists
		}
		if strings.TrimSpace(req.Kind) == "" {
			req.Kind = DefaultPersonaKind
		}
		tags := normalizeTags(req.Capabilities)
		if req.Personality != "" {
			tags = appendUnique(tags, "personality:"+req.Personality)
		}
		raw := renderPersona(req, tags)
		if err := s.writePersonaAtomic(req.ID, []byte(raw)); err != nil {
			return err
		}
		card, err := parsePersonaCard(req.ID, []byte(raw))
		if err != nil {
			return err
		}
		created = card
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

func (s *PersonaStore) EditPersona(id string, req EditPersonaRequest) (*PersonaCard, error) {
	if err := validatePersonaID(id); err != nil {
		return nil, err
	}
	var updated *PersonaCard
	err := s.withPersonaLock(id, func() error {
		preset := false
		raw, err := s.readPersonaRaw(id)
		if err != nil {
			if os.IsNotExist(err) {
				builtin, ok := builtinPresetPersona(id)
				if !ok {
					return ErrNotFound
				}
				raw = []byte(builtin.TOML)
				preset = true
			} else {
				return err
			}
		}
		card, err := parsePersonaCard(id, raw)
		if err != nil {
			return err
		}
		if req.ExpectedETag == "" {
			return ErrPreconditionRequired
		}
		if req.ExpectedETag != card.ETag {
			return ErrConflict
		}
		next := string(raw)
		if req.SetDisplayName != nil {
			next = setSectionScalar(next, "card", "display_name", renderString(strings.TrimSpace(*req.SetDisplayName)))
		}
		if req.SetKind != nil {
			kind := strings.TrimSpace(*req.SetKind)
			if kind == "" {
				kind = DefaultPersonaKind
			}
			next = setSectionScalar(next, "card", "kind", renderString(kind))
		}
		if req.SetSummary != nil {
			next = setSectionScalar(next, "card", "summary", renderString(strings.TrimSpace(*req.SetSummary)))
		}
		if req.SetCapabilities != nil {
			tags := make([]string, 0, len(card.Tags)+len(*req.SetCapabilities))
			for _, tag := range card.Tags {
				if !isBareCapability(tag) {
					tags = appendUnique(tags, tag)
				}
			}
			for _, capability := range normalizeTags(*req.SetCapabilities) {
				if isBareCapability(capability) {
					tags = appendUnique(tags, capability)
				}
			}
			next = setSectionScalar(next, "card", "tags", "["+renderStringList(tags)+"]")
		}
		if req.AddCapability != "" || req.RemoveCapability != "" {
			tags := append([]string{}, card.Tags...)
			if req.AddCapability != "" && isBareCapability(req.AddCapability) {
				tags = appendUnique(tags, req.AddCapability)
			}
			if req.RemoveCapability != "" && isBareCapability(req.RemoveCapability) {
				tags = removeValue(tags, req.RemoveCapability)
			}
			next = setSectionScalar(next, "card", "tags", "["+renderStringList(tags)+"]")
		}
		if req.SetRetired != nil {
			status := "active"
			if *req.SetRetired {
				status = "retired"
			}
			next = setSectionScalar(next, "card", "status", renderString(status))
		}
		if req.Note != "" {
			next = appendPersonaNote(next, PersonaNote{
				Timestamp: s.now().Format(time.RFC3339),
				Actor:     "agent:archon",
				Text:      req.Note,
			})
		}
		// An edit that leaves a built-in role as it ships, such as bringing back
		// one that was retired, keeps no card of its own.
		if builtin, ok := builtinPresetPersona(id); ok && next == builtin.TOML {
			if !preset {
				if err := s.removePersonaFile(id); err != nil {
					return err
				}
			}
			updated = builtin
			return nil
		}
		if err := s.writePersonaAtomic(id, []byte(next)); err != nil {
			return err
		}
		updated, err = parsePersonaCard(id, []byte(next))
		if err == nil {
			if _, ok := builtinPresetPersona(id); ok || preset {
				updated.Preset = true
				updated.Customized = true
			}
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func (s *PersonaStore) now() time.Time {
	if s.Now == nil {
		return time.Now().UTC()
	}
	return s.Now().UTC()
}

// ResolveAgentSession finds a role's own live session: the one named after
// the role.
func ResolveAgentSession(card PersonaCard, live []LiveAgentSession) (AgentSessionBinding, error) {
	matches := make([]LiveAgentSession, 0, 1)
	for _, session := range live {
		if session.Name == card.ID {
			matches = append(matches, session)
		}
	}
	switch len(matches) {
	case 0:
		return AgentSessionBinding{}, fmt.Errorf("%w: no live session for agent %q", ErrAgentSessionOffline, card.ID)
	case 1:
		return AgentSessionBinding{AgentID: card.ID, SessionStem: card.ID, Session: matches[0]}, nil
	default:
		return AgentSessionBinding{}, fmt.Errorf("%w: agent %q matched %d live sessions", ErrAmbiguousAgentBinding, card.ID, len(matches))
	}
}

func ProjectAgentRoster(cards []PersonaCard, live []LiveAgentSession, filter AgentRosterFilter) (AgentRoster, error) {
	usedSessions := map[string]bool{}
	projections := make([]AgentProjection, 0, len(cards)+len(live))
	for _, card := range cards {
		if filter.Capable != "" && !hasBareCapability(card.Tags, filter.Capable) {
			continue
		}
		projection := projectCard(card, live)
		if projection.SessionID != "" {
			usedSessions[projection.SessionID] = true
		}
		if filter.AssignableOnly && !projection.Assignable {
			continue
		}
		projections = append(projections, projection)
	}
	if filter.Capable == "" {
		for _, session := range live {
			if usedSessions[session.Name] {
				continue
			}
			projection := AgentProjection{
				ID:          session.Name,
				DisplayName: session.Name,
				Liveness:    AgentLivenessLive,
				SessionID:   session.Name,
				Status:      session.Status,
				ContextPct:  session.ContextPct,
				BeadID:      session.BeadID,
				Attached:    session.Attached,
				Assignable:  false,
				Unbound:     true,
			}
			if !filter.AssignableOnly {
				projections = append(projections, projection)
			}
		}
	}
	sort.Slice(projections, func(i, j int) bool {
		if projections[i].Unbound != projections[j].Unbound {
			return !projections[i].Unbound
		}
		return projections[i].ID < projections[j].ID
	})
	return AgentRoster{Agents: projections}, nil
}

func (r AgentRoster) ByID(id string) *AgentProjection {
	for i := range r.Agents {
		if r.Agents[i].ID == id {
			return &r.Agents[i]
		}
	}
	return nil
}

func projectCard(card PersonaCard, live []LiveAgentSession) AgentProjection {
	projection := AgentProjection{
		ID:          card.ID,
		DisplayName: card.DisplayName,
		Kind:        card.Kind,
		Summary:     card.Summary,
		Tags:        append([]string{}, card.Tags...),
		Liveness:    AgentLivenessOffline,
		Assignable:  card.Status != "retired",
		Preset:      card.Preset,
		Customized:  card.Customized,
	}
	for _, session := range live {
		if session.Name != card.ID {
			continue
		}
		projection.Liveness = AgentLivenessLive
		projection.SessionID = session.Name
		projection.Status = session.Status
		projection.ContextPct = session.ContextPct
		projection.BeadID = session.BeadID
		projection.Attached = session.Attached
		return projection
	}
	return projection
}

func parsePersonaCard(expectedID string, raw []byte) (*PersonaCard, error) {
	parser := newPersonaParser(raw)
	schema := parser.schema
	if schema > CurrentPersonaSchema {
		return nil, fmt.Errorf("%w: schema %d", ErrUnsupportedSchema, schema)
	}
	card := &PersonaCard{
		Schema:      schema,
		ID:          parser.card["id"],
		DisplayName: parser.card["display_name"],
		Kind:        parser.card["kind"],
		Summary:     parser.card["summary"],
		Status:      parser.card["status"],
		Tags:        parser.cardTags,
		Notes:       parser.notes,
		ETag:        etag(raw),
		TOML:        string(raw),
	}
	name := expectedID
	if name == "" {
		name = card.ID
	}
	if card.Schema == 0 {
		return nil, fmt.Errorf("%w: agent card %q needs schema", ErrInvalidAgentCard, name)
	}
	missing := []string{}
	for field, value := range map[string]string{"card.id": card.ID, "card.kind": card.Kind} {
		if value == "" {
			missing = append(missing, field)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, fmt.Errorf("%w: agent card %q is missing %s", ErrInvalidAgentCard, name, strings.Join(missing, ", "))
	}
	if expectedID != "" && card.ID != expectedID {
		return nil, fmt.Errorf("%w: agent card file %q holds card.id %q; they must match", ErrInvalidAgentCard, expectedID, card.ID)
	}
	return card, nil
}

type personaParser struct {
	schema   int
	card     map[string]string
	cardTags []string
	notes    []PersonaNote
	section  string
	noteIx   int
}

// newPersonaParser reads a card's [card] table and [[note]] entries; it keeps
// other tables in the TOML untouched and reads nothing from them.
func newPersonaParser(raw []byte) *personaParser {
	p := &personaParser{
		card:   map[string]string{},
		noteIx: -1,
	}
	for _, line := range splitLines(raw) {
		trimmed := strings.TrimSpace(line.body)
		switch {
		case trimmed == "[card]":
			p.section = "card"
			continue
		case trimmed == "[[note]]":
			p.section = "note"
			p.notes = append(p.notes, PersonaNote{})
			p.noteIx = len(p.notes) - 1
			continue
		case strings.HasPrefix(trimmed, "["):
			p.section = "other"
			continue
		}
		key, ok := topLevelKey(line.body)
		if !ok {
			continue
		}
		value := strings.TrimSpace(valuePart(line.body))
		switch p.section {
		case "":
			if key == "schema" {
				p.schema, _ = strconv.Atoi(value)
			}
		case "card":
			if key == "tags" {
				p.cardTags = parseStringList(value)
			} else {
				p.card[key] = parseString(value)
			}
		case "note":
			if p.noteIx >= 0 {
				setNoteField(&p.notes[p.noteIx], key, parseString(value))
			}
		}
	}
	return p
}

func setNoteField(n *PersonaNote, key, value string) {
	switch key {
	case "ts":
		n.Timestamp = value
	case "actor":
		n.Actor = value
	case "text":
		n.Text = value
	}
}

func renderPersona(req CreatePersonaRequest, tags []string) string {
	displayName := req.DisplayName
	if displayName == "" {
		displayName = req.ID
	}
	var b strings.Builder
	b.WriteString("schema = " + renderInt(CurrentPersonaSchema) + "\n\n")
	b.WriteString("[card]\n")
	b.WriteString("id = " + renderString(req.ID) + "\n")
	b.WriteString("display_name = " + renderString(displayName) + "\n")
	b.WriteString("kind = " + renderString(req.Kind) + "\n")
	if req.Summary != "" {
		b.WriteString("summary = " + renderString(req.Summary) + "\n")
	}
	b.WriteString("tags = [" + renderStringList(tags) + "]\n")
	b.WriteString("status = \"active\"\n")
	return b.String()
}

func appendPersonaNote(raw string, note PersonaNote) string {
	var b strings.Builder
	b.WriteString(strings.TrimRight(raw, "\r\n"))
	b.WriteString("\n\n[[note]]\n")
	b.WriteString("ts = " + renderString(note.Timestamp) + "\n")
	b.WriteString("actor = " + renderString(note.Actor) + "\n")
	b.WriteString("text = " + renderString(note.Text) + "\n")
	return b.String()
}

func setSectionScalar(raw, section, key, value string) string {
	lines := splitLines([]byte(raw))
	inSection := false
	insertAt := len(lines)
	for i, line := range lines {
		trimmed := strings.TrimSpace(line.body)
		if trimmed == "["+section+"]" {
			inSection = true
			insertAt = i + 1
			continue
		}
		if inSection && strings.HasPrefix(trimmed, "[") {
			insertAt = i
			break
		}
		if inSection {
			if field, ok := topLevelKey(line.body); ok {
				if field == key {
					lines[i].body = replaceScalarValue(line.body, value)
					return renderLines(lines)
				}
				insertAt = i + 1
			}
		}
	}
	newLine := tomlLine{body: key + " = " + value, newline: "\n"}
	lines = append(lines, tomlLine{})
	copy(lines[insertAt+1:], lines[insertAt:])
	lines[insertAt] = newLine
	return renderLines(lines)
}

func renderLines(lines []tomlLine) string {
	var b strings.Builder
	for _, line := range lines {
		b.WriteString(line.body)
		b.WriteString(line.newline)
	}
	return b.String()
}

func parseString(value string) string {
	value = strings.TrimSpace(value)
	if unquoted, err := strconv.Unquote(value); err == nil {
		return unquoted
	}
	return value
}

func parseStringList(value string) []string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "[")
	value = strings.TrimSuffix(value, "]")
	if strings.TrimSpace(value) == "" {
		return []string{}
	}
	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		values = append(values, parseString(part))
	}
	return values
}

func renderStringList(values []string) string {
	rendered := make([]string, 0, len(values))
	for _, value := range values {
		rendered = append(rendered, renderString(value))
	}
	return strings.Join(rendered, ", ")
}

func normalizeTags(values []string) []string {
	tags := make([]string, 0, len(values))
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				tags = appendUnique(tags, part)
			}
		}
	}
	return tags
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func removeValue(values []string, value string) []string {
	next := values[:0]
	for _, existing := range values {
		if existing != value {
			next = append(next, existing)
		}
	}
	return next
}

func hasBareCapability(tags []string, capability string) bool {
	if !isBareCapability(capability) {
		return false
	}
	for _, tag := range tags {
		if tag == capability && isBareCapability(tag) {
			return true
		}
	}
	return false
}

func isBareCapability(tag string) bool {
	return tag != "" && !strings.Contains(tag, ":")
}

func validatePersonaID(id string) error {
	if err := validateSlug(id); err != nil {
		return err
	}
	if id != strings.ToLower(id) || strings.Contains(id, ".") {
		return ErrInvalidSlug
	}
	if strings.HasPrefix(id, "-") || strings.HasSuffix(id, "-") {
		return ErrInvalidSlug
	}
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return ErrInvalidSlug
	}
	return nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
