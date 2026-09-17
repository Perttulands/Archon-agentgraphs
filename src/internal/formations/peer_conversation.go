package formations

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Perttulands/Archon-agentgraphs/internal/filewatch"
)

const (
	PeerConversationMaxBytes = 1 << 20
	PeerMessageMaxBytes      = 256 << 10
	peerConversationFile     = "conversation.ndjson"
)

var (
	ErrPeerConversationClosed  = errors.New("peer conversation is closed")
	ErrPeerDeadlineExceeded    = errors.New("peer conversation deadline exceeded")
	ErrPeerConversationInvalid = errors.New("invalid peer conversation")
	ErrPeerProposalConflict    = errors.New("peer proposal is no longer current or is contested")
)

// PeerConversationID isolates every execution, including rework of the same node.
type PeerConversationID struct {
	RunID   string `json:"runId"`
	NodeID  string `json:"nodeId"`
	Attempt int    `json:"attempt"`
}

type peerConversationHeader struct {
	Schema int `json:"schema"`
	PeerConversationID
	StartedAt    time.Time `json:"startedAt"`
	Deadline     time.Time `json:"deadline"`
	Participants []string  `json:"participants"`
}

type PeerEntry struct {
	Seq         int       `json:"seq"`
	At          time.Time `json:"at"`
	Kind        string    `json:"kind"`
	SlotID      string    `json:"slotId,omitempty"`
	Text        string    `json:"text,omitempty"`
	ProposalSeq int       `json:"proposalSeq,omitempty"`
}

type PeerProposal struct {
	Seq          int      `json:"seq"`
	SlotID       string   `json:"slotId"`
	Text         string   `json:"text"`
	Acknowledged []string `json:"acknowledged"`
	Contested    bool     `json:"contested"`
}

type PeerConversation struct {
	PeerConversationID
	ArtifactPath string        `json:"artifactPath"`
	StartedAt    time.Time     `json:"startedAt"`
	Deadline     time.Time     `json:"deadline"`
	Participants []string      `json:"participants"`
	Entries      []PeerEntry   `json:"entries"`
	LastSeq      int           `json:"lastSeq"`
	Status       string        `json:"status"` // open, agreed, expired, closed
	Proposal     *PeerProposal `json:"proposal,omitempty"`
	FinalText    string        `json:"finalText,omitempty"`
	Reason       string        `json:"reason,omitempty"`
}

type PeerAppendRequest struct {
	SlotID      string
	Kind        string // message, proposal, ack, dissent
	Text        string
	ProposalSeq int
}

// CreatePeerConversation publishes all independent openings together. The
// caller collects them without sharing other participants' opening text.
// Existing attempt journals are never replaced or given a fresh deadline.
func (s *Store) CreatePeerConversation(req FormationExecution, participants []string, openings map[string]string, deadline time.Time) (string, error) {
	id := PeerConversationID{RunID: req.RunID, NodeID: req.NodeID, Attempt: req.Attempt}
	now := s.now().UTC()
	header := peerConversationHeader{Schema: 1, PeerConversationID: id, StartedAt: now, Deadline: deadline.UTC(), Participants: slices.Clone(participants)}
	if err := validatePeerHeader(header, id); err != nil {
		return "", err
	}
	if !now.Before(deadline) {
		return "", ErrPeerDeadlineExceeded
	}
	if len(openings) != len(participants) {
		return "", fmt.Errorf("%w: every participant must supply one opening", ErrPeerConversationInvalid)
	}
	var content bytes.Buffer
	encoder := json.NewEncoder(&content)
	if err := encoder.Encode(header); err != nil {
		return "", err
	}
	for index, slot := range participants {
		text, exists := openings[slot]
		if !exists || strings.TrimSpace(text) == "" || len(text) > PeerMessageMaxBytes || !utf8.ValidString(text) {
			return "", fmt.Errorf("%w: missing, empty or oversized opening for %s", ErrPeerConversationInvalid, slot)
		}
		if err := encoder.Encode(PeerEntry{Seq: index + 1, At: now, Kind: "opening", SlotID: slot, Text: text}); err != nil {
			return "", err
		}
	}
	if content.Len() > PeerConversationMaxBytes {
		return "", fmt.Errorf("%w: conversation exceeds byte limit", ErrPeerConversationInvalid)
	}
	if _, err := s.ReadRunEvents(id.RunID); err != nil {
		return "", err
	}
	directory, err := s.openPeerDirectory(id, true)
	if err != nil {
		return "", err
	}
	defer directory.close()
	err = withPeerLock(directory, func() error {
		return writeRunArtifactExclusiveAt(directory, peerConversationFile, content.Bytes())
	})
	return peerArtifactPath(id), err
}

// ReadPeerConversation keeps partial, expired and closed evidence readable.
func (s *Store) ReadPeerConversation(id PeerConversationID) (*PeerConversation, error) {
	directory, err := s.openPeerDirectory(id, false)
	if err != nil {
		return nil, err
	}
	defer directory.close()
	return s.readPeerConversationAt(directory, id)
}

func (s *Store) AppendPeerConversation(id PeerConversationID, request PeerAppendRequest) (*PeerConversation, error) {
	directory, err := s.openPeerDirectory(id, false)
	if err != nil {
		return nil, err
	}
	defer directory.close()
	return s.appendPeerConversationAt(directory, id, request, false)
}

// ClosePeerConversation preserves the transcript when execution is canceled or
// cannot continue. Closing an expired conversation records the runtime's reason.
func (s *Store) ClosePeerConversation(id PeerConversationID, reason string) (*PeerConversation, error) {
	directory, err := s.openPeerDirectory(id, false)
	if err != nil {
		return nil, err
	}
	defer directory.close()
	return s.appendPeerConversationAt(directory, id, PeerAppendRequest{Kind: "closed", Text: reason}, true)
}

// WaitPeerConversation installs the watcher before reading, so an append cannot
// slip between the read and subscription. The watcher and all reads stay pinned
// to the same safely opened directory. Only changes and the deadline wake it.
func (s *Store) WaitPeerConversation(ctx context.Context, id PeerConversationID, afterSeq int) (*PeerConversation, error) {
	if afterSeq < 0 {
		return nil, fmt.Errorf("%w: afterSeq must be nonnegative", ErrPeerConversationInvalid)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	directory, err := s.openPeerDirectory(id, false)
	if err != nil {
		return nil, err
	}
	defer directory.close()
	// The trailing /. lets filewatch inspect the descriptor's directory rather
	// than treating /proc/self/fd/N as an untraversed symbolic link.
	watch, err := filewatch.New(fmt.Sprintf("/proc/self/fd/%d/.", directory.file.Fd()))
	if err != nil {
		return nil, err
	}
	defer watch.Close()
	state, err := s.readPeerConversationAt(directory, id)
	if err != nil {
		return nil, err
	}
	remaining := state.Deadline.Sub(s.now())
	timer := time.NewTimer(max(remaining, 0))
	defer timer.Stop()
	for {
		if state.Status == "expired" {
			return state, ErrPeerDeadlineExceeded
		}
		if state.LastSeq > afterSeq || state.Status != "open" {
			return state, nil
		}
		select {
		case <-ctx.Done():
			return state, ctx.Err()
		case <-timer.C:
			state, err = s.readPeerConversationAt(directory, id)
			if err != nil {
				return nil, err
			}
			if state.Status == "agreed" || state.Status == "closed" {
				return state, nil
			}
			state.Status = "expired"
			return state, ErrPeerDeadlineExceeded
		case event, ok := <-watch.Events:
			if !ok {
				return state, errors.New("peer conversation watcher ended")
			}
			if event.Err != nil {
				return state, event.Err
			}
			if filepath.Base(event.Path) != peerConversationFile {
				continue
			}
			state, err = s.readPeerConversationAt(directory, id)
			if err != nil {
				return nil, err
			}
		}
	}
}

func (s *Store) appendPeerConversationAt(directory *runArtifactDirectory, id PeerConversationID, request PeerAppendRequest, close bool) (*PeerConversation, error) {
	var state *PeerConversation
	err := withPeerLock(directory, func() error {
		var err error
		state, err = readPeerConversationUnlocked(directory, id, s.now())
		if err != nil {
			return err
		}
		if state.Status == "agreed" || state.Status == "closed" {
			return ErrPeerConversationClosed
		}
		if state.Status == "expired" && !close {
			return ErrPeerDeadlineExceeded
		}
		entry := PeerEntry{Seq: state.LastSeq + 1, At: s.now().UTC(), SlotID: request.SlotID, Kind: request.Kind, Text: request.Text, ProposalSeq: request.ProposalSeq}
		if request.Kind == "closed" && !close {
			return fmt.Errorf("%w: only the executor closes a conversation", ErrPeerConversationInvalid)
		}
		if err := applyPeerEntry(state, entry); err != nil {
			return err
		}
		raw, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		return appendRunArtifactAt(directory, peerConversationFile, append(raw, '\n'), PeerConversationMaxBytes)
	})
	if err != nil {
		return nil, err
	}
	return state, nil
}

func (s *Store) readPeerConversationAt(directory *runArtifactDirectory, id PeerConversationID) (*PeerConversation, error) {
	var state *PeerConversation
	err := withPeerLock(directory, func() error {
		var err error
		state, err = readPeerConversationUnlocked(directory, id, s.now())
		return err
	})
	return state, err
}

func readPeerConversationUnlocked(directory *runArtifactDirectory, id PeerConversationID, now time.Time) (*PeerConversation, error) {
	raw, err := readRunArtifactAt(directory, peerConversationFile, PeerConversationMaxBytes)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || raw[len(raw)-1] != '\n' {
		return nil, fmt.Errorf("%w: incomplete journal record", ErrPeerConversationInvalid)
	}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 4096), PeerConversationMaxBytes+1)
	if !scanner.Scan() {
		return nil, fmt.Errorf("%w: header missing", ErrPeerConversationInvalid)
	}
	var header peerConversationHeader
	if err := decodePeerRecord(scanner.Bytes(), &header); err != nil {
		return nil, err
	}
	if err := validatePeerHeader(header, id); err != nil {
		return nil, err
	}
	state := &PeerConversation{PeerConversationID: id, ArtifactPath: peerArtifactPath(id), StartedAt: header.StartedAt, Deadline: header.Deadline, Participants: header.Participants, Entries: []PeerEntry{}, Status: "open"}
	for scanner.Scan() {
		var entry PeerEntry
		if err := decodePeerRecord(scanner.Bytes(), &entry); err != nil {
			return nil, err
		}
		if err := applyPeerEntry(state, entry); err != nil {
			return nil, fmt.Errorf("%w: seq %d: %v", ErrPeerConversationInvalid, entry.Seq, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPeerConversationInvalid, err)
	}
	if len(state.Entries) < len(header.Participants) {
		return nil, fmt.Errorf("%w: incomplete independent openings", ErrPeerConversationInvalid)
	}
	if state.Status == "open" && !now.Before(state.Deadline) {
		state.Status = "expired"
	}
	return state, nil
}

func applyPeerEntry(state *PeerConversation, entry PeerEntry) error {
	if entry.Seq != state.LastSeq+1 || entry.At.IsZero() || entry.At.Before(state.StartedAt) || len(entry.Text) > PeerMessageMaxBytes || !utf8.ValidString(entry.Text) {
		return fmt.Errorf("%w: invalid sequence, timestamp or message size", ErrPeerConversationInvalid)
	}
	if state.Status == "agreed" || state.Status == "closed" {
		return ErrPeerConversationClosed
	}
	if entry.Kind != "closed" && !entry.At.Before(state.Deadline) {
		return ErrPeerDeadlineExceeded
	}
	if entry.Kind != "closed" && !slices.Contains(state.Participants, entry.SlotID) {
		return fmt.Errorf("%w: unknown participant %q", ErrPeerConversationInvalid, entry.SlotID)
	}
	if state.LastSeq < len(state.Participants) {
		if entry.Kind != "opening" || entry.SlotID != state.Participants[state.LastSeq] || strings.TrimSpace(entry.Text) == "" || entry.ProposalSeq != 0 {
			return fmt.Errorf("%w: independent openings must come first", ErrPeerConversationInvalid)
		}
	} else {
		switch entry.Kind {
		case "message", "proposal":
			if strings.TrimSpace(entry.Text) == "" || entry.ProposalSeq != 0 {
				return fmt.Errorf("%w: message text required without a proposal sequence", ErrPeerConversationInvalid)
			}
			if entry.Kind == "proposal" {
				state.Proposal = &PeerProposal{Seq: entry.Seq, SlotID: entry.SlotID, Text: entry.Text, Acknowledged: []string{}}
			}
		case "ack", "dissent":
			if state.Proposal == nil || entry.ProposalSeq != state.Proposal.Seq || entry.Kind == "ack" && state.Proposal.Contested {
				return ErrPeerProposalConflict
			}
			if entry.Kind == "dissent" {
				if strings.TrimSpace(entry.Text) == "" {
					return fmt.Errorf("%w: explain the dissent", ErrPeerConversationInvalid)
				}
				state.Proposal.Contested = true
			} else {
				if entry.Text != "" || slices.Contains(state.Proposal.Acknowledged, entry.SlotID) {
					return fmt.Errorf("%w: acknowledgement must be new and have no text", ErrPeerConversationInvalid)
				}
				state.Proposal.Acknowledged = append(state.Proposal.Acknowledged, entry.SlotID)
				if len(state.Proposal.Acknowledged) == len(state.Participants) {
					state.Status, state.FinalText = "agreed", state.Proposal.Text
				}
			}
		case "closed":
			if entry.SlotID != "" || entry.ProposalSeq != 0 || strings.TrimSpace(entry.Text) == "" {
				return fmt.Errorf("%w: closure requires only a reason", ErrPeerConversationInvalid)
			}
			state.Status, state.Reason = "closed", entry.Text
		default:
			return fmt.Errorf("%w: unknown message kind %q", ErrPeerConversationInvalid, entry.Kind)
		}
	}
	state.LastSeq = entry.Seq
	state.Entries = append(state.Entries, entry)
	return nil
}

func validatePeerHeader(header peerConversationHeader, expected PeerConversationID) error {
	if header.Schema != 1 || header.PeerConversationID != expected || !validRunID(expected.RunID) || !peerPathID(expected.NodeID) || expected.Attempt < 1 || header.StartedAt.IsZero() || !header.StartedAt.Before(header.Deadline) || len(header.Participants) < 2 {
		return fmt.Errorf("%w: identity, participants or deadline", ErrPeerConversationInvalid)
	}
	seen := map[string]bool{}
	for _, slot := range header.Participants {
		if !peerPathID(slot) || seen[slot] {
			return fmt.Errorf("%w: invalid or repeated participant", ErrPeerConversationInvalid)
		}
		seen[slot] = true
	}
	return nil
}

func decodePeerRecord(raw []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return fmt.Errorf("%w: %v", ErrPeerConversationInvalid, err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("%w: extra record data", ErrPeerConversationInvalid)
	}
	return nil
}

func peerPathID(id string) bool {
	return len(id) <= 128 && runtimeAuthorityPathComponent(id) && !strings.Contains(id, "..")
}

func peerArtifactPath(id PeerConversationID) string {
	return filepath.Join("peer", id.NodeID, fmt.Sprintf("attempt-%d", id.Attempt), peerConversationFile)
}

func withPeerLock(directory *runArtifactDirectory, fn func() error) error {
	return withRunArtifactLock(directory, peerConversationFile, directory.path+"/"+peerConversationFile, fn)
}

func (s *Store) openPeerDirectory(id PeerConversationID, create bool) (*runArtifactDirectory, error) {
	if !validRunID(id.RunID) || !peerPathID(id.NodeID) || id.Attempt < 1 {
		return nil, fmt.Errorf("%w: invalid identity", ErrPeerConversationInvalid)
	}
	workspace, err := s.workspaceAbsolutePath()
	if err != nil {
		return nil, err
	}
	current, err := openRunWorkspaceRoot(workspace)
	if err != nil {
		return nil, err
	}
	path := workspace
	for _, component := range []string{".formations", "artifacts", id.RunID, "peer", id.NodeID, fmt.Sprintf("attempt-%d", id.Attempt)} {
		var next *os.File
		if create {
			next, err = openOrCreateRunArtifactDirectoryAt(current, component)
		} else {
			next, err = openRuntimeAuthorityDirectoryAt(current, component)
		}
		current.Close()
		if err != nil {
			return nil, err
		}
		current = next
		path = filepath.Join(path, component)
	}
	return &runArtifactDirectory{file: current, path: path}, nil
}
