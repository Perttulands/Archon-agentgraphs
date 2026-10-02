package formations

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Limit cards (archon-o7p.8). A run has no limits unless its mission holds a
// Limit card. A card covers one target: a step (a formation) or the Input
// card, which stands for the whole mission. Its knobs are optional; each one
// set is enforced. At a limit the covered work stops and the run blocks with a
// plain reason; the driver may grant one more allowance with run resume
// --grant, and the ledger records the grant and who gave it.
//
// Rounds count how many times a step may run, send-backs included, or how many
// steps the whole mission may run. A peer formation's rounds are its journal
// messages. Time counts wall time while the covered work runs: the step's own
// attempts, or any step of the mission; waiting on a human gate or a blocked
// run counts nothing. A warning is pasted once into the covered seats when the
// time left reaches the card's warnSeconds. Tokens count what the covered
// seats spend, by the approximate definition in token_usage.go; crossing the
// budget stops the working step (archon-o7p.9).

// FindingInvalidLimit reports a Limit card that covers nothing, covers a node
// it cannot, holds a value that is not a positive whole number, or shares its
// target with another card.
const FindingInvalidLimit = "invalid_limit"

// FindingEmptyLimit warns about a Limit card that sets no knob.
const FindingEmptyLimit = "empty_limit"

// LimitKindRounds is the rounds knob.
const LimitKindRounds = "rounds"

// LimitKindTime is the time knob, counted in whole seconds.
const LimitKindTime = "time"

// LimitKindTokens is the tokens knob.
const LimitKindTokens = "tokens"

// RunBlockLimitReached is the code of the block a spent limit records.
const RunBlockLimitReached = "limit_reached"

// ResumePolicyGrant marks a block that resumes only with a grant.
const ResumePolicyGrant = "grant"

// ErrInvalidLimit refuses a Limit card write with a malformed value or target.
var ErrInvalidLimit = errors.New("invalid_limit")

// LimitNode is a Limit card. A knob is nil when the card does not set it; a
// hand-written non-positive value is kept, so validation can name it. Seconds
// is the time knob and WarnSeconds how much of it is left when the covered
// seats are warned.
type LimitNode struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Target      string `json:"target"`
	Rounds      *int   `json:"rounds,omitempty"`
	Seconds     *int   `json:"seconds,omitempty"`
	WarnSeconds *int   `json:"warnSeconds,omitempty"`
	Tokens      *int   `json:"tokens,omitempty"`
}

// LimitCreateRequest sets each knob that is not zero.
type LimitCreateRequest struct {
	Title       string
	Target      string
	Rounds      int
	Seconds     int
	WarnSeconds int
	Tokens      int
	X           int
	Y           int
	UpdatedBy   string
}

type LimitCreateResult struct {
	Board  *BoardDocument  `json:"mission"`
	Layout *LayoutDocument `json:"layout"`
	Limit  LimitNode       `json:"limit"`
}

// LimitUpdateRequest changes only the fields it sets. A knob set to 0 is
// cleared.
type LimitUpdateRequest struct {
	LimitID     string
	Title       *string
	Target      *string
	Rounds      *int
	Seconds     *int
	WarnSeconds *int
	Tokens      *int
	UpdatedBy   string
}

type LimitDeleteRequest struct {
	ID        string
	UpdatedBy string
}

type LimitDeleteResult struct {
	Board   *BoardDocument  `json:"mission"`
	Layout  *LayoutDocument `json:"layout"`
	LimitID string          `json:"limitId"`
}

func defaultLimitTitle() string { return "Limit" }

// validLimitTarget reports whether a Limit card may cover nodeID: a step or
// the Input card.
func validLimitTarget(board *BoardDocument, nodeID string) bool {
	if _, ok := findFormation(board.Formations, nodeID); ok {
		return true
	}
	_, ok := findMission(board, nodeID)
	return ok
}

func checkLimitWrite(board *BoardDocument, target string, rounds, seconds, warnSeconds, tokens *int) error {
	if target != "" && !validLimitTarget(board, target) {
		return fmt.Errorf("%w: %q is not a step or the Input card; a Limit card covers one of them", ErrInvalidLimit, target)
	}
	if rounds != nil && *rounds < 0 {
		return fmt.Errorf("%w: rounds must be a positive whole number", ErrInvalidLimit)
	}
	if seconds != nil && *seconds < 0 {
		return fmt.Errorf("%w: time must be a positive whole number of seconds", ErrInvalidLimit)
	}
	if warnSeconds != nil && *warnSeconds < 0 {
		return fmt.Errorf("%w: the warning must be a positive whole number of seconds", ErrInvalidLimit)
	}
	if tokens != nil && *tokens < 0 {
		return fmt.Errorf("%w: tokens must be a positive whole number", ErrInvalidLimit)
	}
	return nil
}

// knob is a create request's knob: nil for zero, which sets nothing.
func knob(value int) *int {
	if value == 0 {
		return nil
	}
	return &value
}

func (s *Store) CreateLimit(slug string, req LimitCreateRequest, opts WriteOptions) (*LimitCreateResult, error) {
	if err := validateSlug(slug); err != nil {
		return nil, err
	}
	if opts.ExpectedETag == "" || opts.ExpectedRev == 0 {
		return nil, ErrPreconditionRequired
	}
	limit := LimitNode{ID: newPrefixedID("lim"), Title: strings.TrimSpace(req.Title), Target: strings.TrimSpace(req.Target),
		Rounds: knob(req.Rounds), Seconds: knob(req.Seconds), WarnSeconds: knob(req.WarnSeconds), Tokens: knob(req.Tokens)}
	if limit.Title == "" {
		limit.Title = defaultLimitTitle()
	}
	board, layout, err := s.createNode(slug, opts, nodeCreateCandidate{
		prepare: func(_ []byte, current *BoardDocument) error {
			return checkLimitWrite(current, limit.Target, limit.Rounds, limit.Seconds, limit.WarnSeconds, limit.Tokens)
		},
		appendBoardBlock: func(raw []byte) []byte { return appendLimitBlock(raw, limit) },
		node:             LayoutNode{ID: limit.ID, X: req.X, Y: req.Y},
		updatedBy:        req.UpdatedBy,
	}, nil)
	if err != nil {
		return nil, err
	}
	return &LimitCreateResult{Board: board, Layout: layout, Limit: limit}, nil
}

func (s *Store) UpdateLimit(slug string, req LimitUpdateRequest, opts WriteOptions) (*BoardDocument, error) {
	if req.LimitID == "" {
		return nil, ErrNotFound
	}
	return s.updateBoardDefinition(slug, req.UpdatedBy, opts, func(raw []byte, current *BoardDocument) ([]byte, error) {
		lines := splitLines(raw)
		start, end, ok := findLimitBlockByID(lines, req.LimitID)
		if !ok {
			return nil, ErrNotFound
		}
		var target string
		if req.Target != nil {
			target = strings.TrimSpace(*req.Target)
		}
		if err := checkLimitWrite(current, target, req.Rounds, req.Seconds, req.WarnSeconds, req.Tokens); err != nil {
			return nil, err
		}
		if req.Title != nil {
			title := strings.TrimSpace(*req.Title)
			if title == "" {
				title = defaultLimitTitle()
			}
			lines = setScalarInLineRange(lines, start+1, end, "title", renderString(title))
			start, end, _ = findLimitBlockByID(lines, req.LimitID)
		}
		if req.Target != nil {
			lines = setScalarInLineRange(lines, start+1, end, "target", renderString(target))
			start, end, _ = findLimitBlockByID(lines, req.LimitID)
		}
		for _, knob := range []struct {
			key   string
			value *int
		}{{"rounds", req.Rounds}, {"seconds", req.Seconds}, {"warnSeconds", req.WarnSeconds}, {"tokens", req.Tokens}} {
			if knob.value == nil {
				continue
			}
			if *knob.value == 0 {
				lines = removeScalarInLineRange(lines, start+1, end, knob.key)
			} else {
				lines = setScalarInLineRange(lines, start+1, end, knob.key, renderInt(*knob.value))
			}
			start, end, _ = findLimitBlockByID(lines, req.LimitID)
		}
		return renderTOMLLines(lines), nil
	})
}

func (s *Store) DeleteLimit(slug string, req LimitDeleteRequest, opts WriteOptions) (*LimitDeleteResult, error) {
	if err := validateSlug(slug); err != nil {
		return nil, err
	}
	if req.ID == "" {
		return nil, ErrNotFound
	}
	if opts.ExpectedETag == "" || opts.ExpectedRev == 0 {
		return nil, ErrPreconditionRequired
	}
	var result *LimitDeleteResult
	err := s.withBoardDefinitionLock(slug, func(definition *definitionFile) error {
		raw, err := definition.readBytes()
		if err != nil {
			return err
		}
		current, err := parseBoard(raw)
		if err != nil {
			return err
		}
		if opts.ExpectedETag != current.ETag || opts.ExpectedRev != current.Rev {
			return ErrConflict
		}
		if err := s.validateExistingLayoutSourceForWrite(slug); err != nil {
			return err
		}
		doc := parseTOMLDocument(raw)
		if req.UpdatedBy != "" {
			doc.setScalar("updatedBy", renderString(req.UpdatedBy))
		}
		doc.setScalar("rev", renderInt(current.Rev+1))
		doc.setScalar("updatedAt", renderString(s.now().Format(time.RFC3339)))
		nextRaw, deleted := deleteTopLevelBlockByID(doc.bytes(), "limit", req.ID)
		if !deleted {
			return ErrNotFound
		}
		if err := definition.writeAtomic(nextRaw); err != nil {
			return err
		}
		board, err := parseBoard(nextRaw)
		if err != nil {
			return err
		}
		layout, err := s.deleteLayoutNodes(slug, board.ID, board.Rev, map[string]bool{req.ID: true})
		if err != nil {
			return err
		}
		result = &LimitDeleteResult{Board: board, Layout: layout, LimitID: req.ID}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func appendLimitBlock(raw []byte, limit LimitNode) []byte {
	var b strings.Builder
	b.Write(raw)
	text := string(raw)
	if text != "" && !strings.HasSuffix(text, "\n") {
		b.WriteByte('\n')
	}
	if text != "" {
		b.WriteByte('\n')
	}
	b.WriteString("[[limit]]\n")
	b.WriteString("id = " + renderString(limit.ID) + "\n")
	b.WriteString("title = " + renderString(limit.Title) + "\n")
	b.WriteString("target = " + renderString(limit.Target) + "\n")
	if limit.Rounds != nil {
		b.WriteString("rounds = " + renderInt(*limit.Rounds) + "\n")
	}
	if limit.Seconds != nil {
		b.WriteString("seconds = " + renderInt(*limit.Seconds) + "\n")
	}
	if limit.WarnSeconds != nil {
		b.WriteString("warnSeconds = " + renderInt(*limit.WarnSeconds) + "\n")
	}
	if limit.Tokens != nil {
		b.WriteString("tokens = " + renderInt(*limit.Tokens) + "\n")
	}
	return []byte(b.String())
}

func findLimitBlockByID(lines []tomlLine, limitID string) (int, int, bool) {
	for i := 0; i < len(lines); i++ {
		section, ok := tomlLineSectionName(lines[i])
		if !ok || section != "limit" {
			continue
		}
		end := tomlBlockEnd(lines, i)
		if scalarInBlock(lines, i+1, end, "id") == limitID {
			return i, end, true
		}
	}
	return 0, 0, false
}

func decodeLimitNodes(document map[string]any) ([]LimitNode, error) {
	tables, err := tomlTableArray(document, "limit")
	if err != nil || tables == nil {
		return nil, err
	}
	nodes := make([]LimitNode, 0, len(tables))
	for _, table := range tables {
		var node LimitNode
		if node.ID, err = tomlString(table, "id"); err != nil {
			return nil, err
		}
		if node.Title, err = tomlString(table, "title"); err != nil {
			return nil, err
		}
		if node.Target, err = tomlString(table, "target"); err != nil {
			return nil, err
		}
		for _, knob := range []struct {
			key   string
			value **int
		}{{"rounds", &node.Rounds}, {"seconds", &node.Seconds}, {"warnSeconds", &node.WarnSeconds}, {"tokens", &node.Tokens}} {
			if _, present := table[knob.key]; present {
				value, err := tomlInt(table, knob.key)
				if err != nil {
					return nil, err
				}
				*knob.value = &value
			}
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

func findLimit(board *BoardDocument, limitID string) (LimitNode, bool) {
	if board == nil {
		return LimitNode{}, false
	}
	for _, limit := range board.Limits {
		if limit.ID == limitID {
			return limit, true
		}
	}
	return LimitNode{}, false
}

// limitCovering is the Limit card that covers a step or the Input card.
func limitCovering(board *BoardDocument, nodeID string) (LimitNode, bool) {
	if board == nil || nodeID == "" {
		return LimitNode{}, false
	}
	for _, limit := range board.Limits {
		if limit.Target == nodeID {
			return limit, true
		}
	}
	return LimitNode{}, false
}

// limitFindings validates every Limit card: it covers one step or the Input
// card, no other card covers the same target, and each knob it sets is a
// positive whole number. A card that sets no knob limits nothing.
func limitFindings(board *BoardDocument) (errs, warnings []BoardFinding) {
	covered := map[string]string{}
	for _, limit := range board.Limits {
		name := nodeName(board, limit.ID)
		add := func(message string) {
			errs = append(errs, BoardFinding{Code: FindingInvalidLimit, NodeID: limit.ID, Message: message})
		}
		switch {
		case strings.TrimSpace(limit.Target) == "":
			add(fmt.Sprintf("Limit %s is wired to nothing: wire it to a step, or to the Input card for the whole mission", name))
		case !validLimitTarget(board, limit.Target):
			add(fmt.Sprintf("Limit %s covers %s, which is not a step or the Input card: wire it to a step, or to the Input card for the whole mission", name, limit.Target))
		case covered[limit.Target] != "":
			add(fmt.Sprintf("%s has two Limit cards, %s and %s: keep one", nodeName(board, limit.Target), nodeName(board, covered[limit.Target]), name))
		default:
			covered[limit.Target] = limit.ID
		}
		if limit.Rounds != nil && *limit.Rounds <= 0 {
			add(fmt.Sprintf("Limit %s holds rounds = %d: rounds must be a positive whole number", name, *limit.Rounds))
		}
		if limit.Seconds != nil && *limit.Seconds <= 0 {
			add(fmt.Sprintf("Limit %s holds seconds = %d: time must be a positive whole number of seconds", name, *limit.Seconds))
		}
		switch warn := limit.WarnSeconds; {
		case warn == nil:
		case *warn <= 0:
			add(fmt.Sprintf("Limit %s holds warnSeconds = %d: the warning must be a positive whole number of seconds", name, *warn))
		case limit.Seconds == nil:
			add(fmt.Sprintf("Limit %s warns with %s left but sets no time: give it time, or clear the warning", name, durationWords(*warn)))
		case *limit.Seconds > 0 && *warn >= *limit.Seconds:
			add(fmt.Sprintf("Limit %s warns with %s left of %s, before any work: warn with less time left", name, durationWords(*warn), durationWords(*limit.Seconds)))
		}
		if limit.Tokens != nil && *limit.Tokens <= 0 {
			add(fmt.Sprintf("Limit %s holds tokens = %d: tokens must be a positive whole number", name, *limit.Tokens))
		}
		if limit.Rounds == nil && limit.Seconds == nil && limit.Tokens == nil {
			warnings = append(warnings, BoardFinding{Code: FindingEmptyLimit, NodeID: limit.ID,
				Message: fmt.Sprintf("Limit %s sets no limit: give it rounds, time or tokens, or delete it", name)})
		}
	}
	return errs, warnings
}

// durationWords says whole seconds plainly: "45 s", "5 min", "1 h 30 min",
// "1 min 30 s".
func durationWords(seconds int) string {
	if seconds < 60 {
		return fmt.Sprintf("%d s", seconds)
	}
	hours, minutes, rest := seconds/3600, seconds%3600/60, seconds%60
	parts := []string{}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%d h", hours))
	}
	if minutes > 0 {
		parts = append(parts, fmt.Sprintf("%d min", minutes))
	}
	if rest > 0 {
		parts = append(parts, fmt.Sprintf("%d s", rest))
	}
	return strings.Join(parts, " ")
}

// tokenWords says a token count plainly, with thousands separated: "1 token",
// "50,000 tokens".
func tokenWords(count int) string {
	digits := fmt.Sprint(count)
	if count < 0 {
		digits = digits[1:]
	}
	var b strings.Builder
	for index, digit := range digits {
		if index > 0 && (len(digits)-index)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(digit)
	}
	words := b.String()
	if count < 0 {
		words = "-" + words
	}
	if count == 1 {
		return words + " token"
	}
	return words + " tokens"
}

func plural(count int, noun string) string {
	if count == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", count, noun)
}
