package formations

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// End nodes end a path on purpose (form-o7p.10). Every output and gate route
// leads to a step, a gate or an End node; an End node has one input, takes
// any number of routes into it, and leads nowhere. Its outcome says how the
// path ended: done, or rejected, which fails the run with the reason of the
// gate verdict that routed there once nothing else can run.

const (
	EndOutcomeDone     = "done"
	EndOutcomeRejected = "rejected"
	// EndPortIn is an End node's only port.
	EndPortIn = "in"
)

// ErrInvalidEndOutcome rejects an End node outcome other than done or rejected.
var ErrInvalidEndOutcome = errors.New("invalid_end_outcome")

type EndNode struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Outcome string `json:"outcome"`
}

type EndCreateRequest struct {
	Title     string
	Outcome   string
	X         int
	Y         int
	UpdatedBy string
}

type EndCreateResult struct {
	Board  *BoardDocument  `json:"board"`
	Layout *LayoutDocument `json:"layout"`
	End    EndNode         `json:"end"`
}

// EndUpdateRequest changes only the fields it sets.
type EndUpdateRequest struct {
	EndID     string
	Title     *string
	Outcome   *string
	UpdatedBy string
}

type EndDeleteRequest struct {
	ID        string
	UpdatedBy string
}

type EndDeleteResult struct {
	Board  *BoardDocument  `json:"board"`
	Layout *LayoutDocument `json:"layout"`
	EndID  string          `json:"endId"`
}

// NormalizeEndOutcome returns done for an empty outcome and refuses anything
// but done or rejected.
func NormalizeEndOutcome(value string) (string, error) {
	switch outcome := strings.TrimSpace(value); outcome {
	case "", EndOutcomeDone:
		return EndOutcomeDone, nil
	case EndOutcomeRejected:
		return outcome, nil
	default:
		return "", fmt.Errorf("%w: End outcome %q must be done or rejected", ErrInvalidEndOutcome, value)
	}
}

func defaultEndTitle(outcome string) string {
	if outcome == EndOutcomeRejected {
		return "Rejected"
	}
	return "Done"
}

func (s *Store) CreateEnd(slug string, req EndCreateRequest, opts WriteOptions) (*EndCreateResult, error) {
	if err := validateSlug(slug); err != nil {
		return nil, err
	}
	if opts.ExpectedETag == "" || opts.ExpectedRev == 0 {
		return nil, ErrPreconditionRequired
	}
	outcome, err := NormalizeEndOutcome(req.Outcome)
	if err != nil {
		return nil, err
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = defaultEndTitle(outcome)
	}
	end := EndNode{ID: newPrefixedID("end"), Title: title, Outcome: outcome}
	board, layout, err := s.createNode(slug, opts, nodeCreateCandidate{
		appendBoardBlock: func(raw []byte) []byte { return appendEndBlock(raw, end) },
		node:             LayoutNode{ID: end.ID, X: req.X, Y: req.Y},
		updatedBy:        req.UpdatedBy,
	}, nil)
	if err != nil {
		return nil, err
	}
	return &EndCreateResult{Board: board, Layout: layout, End: end}, nil
}

func (s *Store) UpdateEnd(slug string, req EndUpdateRequest, opts WriteOptions) (*BoardDocument, error) {
	if req.EndID == "" {
		return nil, ErrNotFound
	}
	var outcome string
	if req.Outcome != nil {
		normalized, err := NormalizeEndOutcome(*req.Outcome)
		if err != nil {
			return nil, err
		}
		outcome = normalized
	}
	return s.updateBoardDefinition(slug, req.UpdatedBy, opts, func(raw []byte, _ *BoardDocument) ([]byte, error) {
		lines := splitLines(raw)
		start, end, ok := findEndBlockByID(lines, req.EndID)
		if !ok {
			return nil, ErrNotFound
		}
		if req.Title != nil {
			lines = setScalarInLineRange(lines, start+1, end, "title", renderString(*req.Title))
			start, end, _ = findEndBlockByID(lines, req.EndID)
		}
		if req.Outcome != nil {
			lines = setScalarInLineRange(lines, start+1, end, "outcome", renderString(outcome))
		}
		return renderTOMLLines(lines), nil
	})
}

func (s *Store) DeleteEnd(slug string, req EndDeleteRequest, opts WriteOptions) (*EndDeleteResult, error) {
	if err := validateSlug(slug); err != nil {
		return nil, err
	}
	if req.ID == "" {
		return nil, ErrNotFound
	}
	if opts.ExpectedETag == "" || opts.ExpectedRev == 0 {
		return nil, ErrPreconditionRequired
	}
	deletedIDs := map[string]bool{req.ID: true}
	var result *EndDeleteResult
	err := s.withBoardDefinitionLock(slug, func(definition *definitionFile) error {
		raw, err := definition.readBytes()
		if err != nil {
			return err
		}
		current, err := parseBoardForWrite(raw)
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
		nextRaw, deleted := deleteTopLevelBlockByID(doc.bytes(), "end", req.ID)
		if !deleted {
			return ErrNotFound
		}
		nextRaw = deleteConnectionsTouchingNodes(nextRaw, deletedIDs)
		if err := definition.writeAtomic(nextRaw); err != nil {
			return err
		}
		board, err := parseBoardForWrite(nextRaw)
		if err != nil {
			return err
		}
		layout, err := s.deleteLayoutNodes(slug, board.ID, board.Rev, deletedIDs)
		if err != nil {
			return err
		}
		result = &EndDeleteResult{Board: board, Layout: layout, EndID: req.ID}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func appendEndBlock(raw []byte, end EndNode) []byte {
	var b strings.Builder
	b.Write(raw)
	text := string(raw)
	if text != "" && !strings.HasSuffix(text, "\n") {
		b.WriteByte('\n')
	}
	if text != "" {
		b.WriteByte('\n')
	}
	b.WriteString("[[end]]\n")
	b.WriteString("id = " + renderString(end.ID) + "\n")
	b.WriteString("title = " + renderString(end.Title) + "\n")
	b.WriteString("outcome = " + renderString(end.Outcome) + "\n")
	return []byte(b.String())
}

func findEndBlockByID(lines []tomlLine, endID string) (int, int, bool) {
	for i := 0; i < len(lines); i++ {
		section, ok := tomlLineSectionName(lines[i])
		if !ok || section != "end" {
			continue
		}
		end := tomlBlockEnd(lines, i)
		if scalarInBlock(lines, i+1, end, "id") == endID {
			return i, end, true
		}
	}
	return 0, 0, false
}

func decodeEndNodes(document map[string]any) ([]EndNode, error) {
	tables, err := tomlTableArray(document, "end")
	if err != nil || tables == nil {
		return nil, err
	}
	nodes := make([]EndNode, 0, len(tables))
	for _, table := range tables {
		var node EndNode
		if node.ID, err = tomlString(table, "id"); err != nil {
			return nil, err
		}
		if node.Title, err = tomlString(table, "title"); err != nil {
			return nil, err
		}
		if node.Outcome, err = tomlString(table, "outcome"); err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

func parseEndNodes(raw []byte) []EndNode {
	var ends []EndNode
	var current *EndNode
	active := false
	for _, line := range splitLines(raw) {
		trimmed := strings.TrimSpace(line.body)
		section, isSection := tomlLineSectionName(line)
		switch {
		case isSection && strings.HasPrefix(trimmed, "[[") && section == "end":
			ends = append(ends, EndNode{})
			current = &ends[len(ends)-1]
			active = true
			continue
		case isTOMLHeader(line):
			active = false
			continue
		}
		if line.valueContinuation || !active || current == nil {
			continue
		}
		key, value, ok := tomlKeyValue(line.body)
		if !ok {
			continue
		}
		switch key {
		case "id":
			current.ID = value
		case "title":
			current.Title = value
		case "outcome":
			current.Outcome = value
		}
	}
	return ends
}

func findEnd(board *BoardDocument, endID string) (EndNode, bool) {
	if board == nil {
		return EndNode{}, false
	}
	for _, end := range board.Ends {
		if end.ID == endID {
			return end, true
		}
	}
	return EndNode{}, false
}

// isEndEndpoint reports whether an endpoint is an End node's input. Any number
// of routes may lead into one End node.
func isEndEndpoint(board *BoardDocument, endpoint string) bool {
	nodeID, portID, ok := splitEndpoint(endpoint)
	if !ok || portID != EndPortIn {
		return false
	}
	_, found := findEnd(board, nodeID)
	return found
}

// endPathText is how the cockpit and CLI say where a path ends.
func endPathText(end EndNode) string {
	return "this path ends (" + end.Outcome + ")"
}
