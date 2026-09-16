package formations

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

// Evidence file reads open every component from the workspace root with
// O_NOFOLLOW, using the run artifact helpers, so neither a request name nor a
// symlink placed by an agent can leave the run's own directories.

type RunArtifactEntry struct {
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	ModifiedAt string `json:"modifiedAt"`
}

type RunArtifactPreview struct {
	Name       string        `json:"name"`
	Size       int64         `json:"size"`
	ModifiedAt string        `json:"modifiedAt"`
	Kind       string        `json:"kind"`
	Text       *EvidenceText `json:"text,omitempty"`
}

type RunBriefEvidence struct {
	DispatchSeq int          `json:"dispatchSeq"`
	NodeID      string       `json:"nodeId"`
	SlotID      string       `json:"slotId,omitempty"`
	Attempt     int          `json:"attempt,omitempty"`
	Text        EvidenceText `json:"text"`
}

// RunArtifactContent is a whole artifact read for the raw route: textual
// content is redacted like every other served text.
type RunArtifactContent struct {
	Name        string
	ModifiedAt  time.Time
	ContentType string
	Body        []byte
}

// ListRunArtifacts lists regular single-link files under the run's artifact
// directory, sorted by name. Truncated reports entries past the count or depth
// caps. A run without an artifact directory has none.
func (s *Store) ListRunArtifacts(runID string) ([]RunArtifactEntry, bool, error) {
	if _, err := s.ReadRunEvents(runID); err != nil {
		return nil, false, evidenceNotFound(err)
	}
	root, err := s.openRunArtifactsRoot(runID)
	if errors.Is(err, ErrNotFound) {
		return []RunArtifactEntry{}, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer root.Close()
	entries := []RunArtifactEntry{}
	truncated := false
	if err := listRunArtifactsAt(root, "", 1, &entries, &truncated); err != nil {
		return nil, false, err
	}
	return entries, truncated, nil
}

func listRunArtifactsAt(directory *os.File, prefix string, depth int, entries *[]RunArtifactEntry, truncated *bool) error {
	names, err := directory.Readdirnames(-1)
	if err != nil {
		return err
	}
	sort.Strings(names)
	for _, name := range names {
		if len(*entries) >= EvidenceArtifactListMax {
			*truncated = true
			return nil
		}
		if !runtimeAuthorityPathComponent(name) {
			continue
		}
		child, err := openRuntimeAuthorityDirectoryAt(directory, name)
		if err == nil {
			if depth >= EvidenceArtifactListDepth {
				_ = child.Close()
				*truncated = true
				continue
			}
			err = listRunArtifactsAt(child, prefix+name+"/", depth+1, entries, truncated)
			_ = child.Close()
			if err != nil {
				return err
			}
			continue
		}
		// Symlinks fail with ELOOP and are skipped; only a non-directory is
		// tried as a regular single-link file.
		if !errors.Is(err, syscall.ENOTDIR) {
			continue
		}
		file, err := openRunArtifactFileAt(directory, name, syscall.O_RDONLY, false)
		if err != nil {
			continue
		}
		info, err := file.Stat()
		_ = file.Close()
		if err != nil {
			continue
		}
		*entries = append(*entries, RunArtifactEntry{
			Name:       prefix + name,
			Size:       info.Size(),
			ModifiedAt: info.ModTime().UTC().Format(time.RFC3339),
		})
	}
	return nil
}

// PreviewRunArtifact reads the start of one artifact and classifies it.
func (s *Store) PreviewRunArtifact(runID, name string) (*RunArtifactPreview, error) {
	file, info, err := s.openRunArtifact(runID, name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	head, err := io.ReadAll(io.LimitReader(file, EvidenceArtifactPreviewMaxBytes+utf8.UTFMax))
	if err != nil {
		return nil, err
	}
	partial := int64(len(head)) < info.Size()
	preview := &RunArtifactPreview{Name: name, Size: info.Size(), ModifiedAt: info.ModTime().UTC().Format(time.RFC3339), Kind: evidenceArtifactKind(name, head, partial)}
	if evidenceTextKind(preview.Kind) {
		redacted := redactEvidenceText(string(head))
		text, cut := CapEvidenceText(redacted, EvidenceArtifactPreviewMaxBytes)
		size := int(info.Size())
		if !partial {
			size = len(redacted)
		}
		preview.Text = &EvidenceText{Text: text, Bytes: size, Truncated: cut || partial}
	}
	return preview, nil
}

// ReadRunArtifact reads a whole artifact for the raw route, up to the raw cap.
func (s *Store) ReadRunArtifact(runID, name string) (*RunArtifactContent, error) {
	file, info, err := s.openRunArtifact(runID, name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if info.Size() > EvidenceArtifactRawMaxBytes {
		return nil, ErrEvidenceTooLarge
	}
	body, err := io.ReadAll(io.LimitReader(file, EvidenceArtifactRawMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > EvidenceArtifactRawMaxBytes {
		return nil, ErrEvidenceTooLarge
	}
	return evidenceRawContent(name, info.ModTime(), body), nil
}

// evidenceTextKind reports whether a preview of this kind carries text.
func evidenceTextKind(kind string) bool {
	return kind != "image" && kind != "pdf" && kind != "binary"
}

// evidenceRawContent types a whole file for a raw route: known images and PDFs
// keep their types, other binaries download, and text is plain and redacted.
func evidenceRawContent(name string, modifiedAt time.Time, body []byte) *RunArtifactContent {
	content := &RunArtifactContent{Name: path.Base(name), ModifiedAt: modifiedAt, Body: body}
	switch evidenceArtifactKind(name, body, false) {
	case "image":
		content.ContentType = evidenceImageTypes[strings.ToLower(path.Ext(name))]
	case "pdf":
		content.ContentType = "application/pdf"
	case "binary":
		content.ContentType = "application/octet-stream"
	default:
		content.ContentType = "text/plain; charset=utf-8"
		content.Body = []byte(redactEvidenceText(string(body)))
	}
	return content
}

// ReadRunBrief reads the brief a run's own dispatch recorded. The path comes
// only from that slot_dispatch event and must name a direct child of
// <workspace>/briefs.
func (s *Store) ReadRunBrief(runID string, dispatchSeq int) (*RunBriefEvidence, error) {
	events, err := s.ReadRunEvents(runID)
	if err != nil {
		return nil, evidenceNotFound(err)
	}
	var dispatch *RunEvent
	for i := range events {
		if events[i].Seq == dispatchSeq && events[i].Type == RunEventSlotDispatch {
			dispatch = &events[i]
		}
	}
	if dispatch == nil {
		return nil, fmt.Errorf("%w: dispatch %d", ErrNotFound, dispatchSeq)
	}
	briefPath := stringFromEventData(*dispatch, "briefPath")
	workspace, err := s.workspaceAbsolutePath()
	if err != nil {
		return nil, err
	}
	if briefPath == "" || filepath.Clean(briefPath) != briefPath || !evidenceBriefDirectory(filepath.Dir(briefPath), s.workspaceRoot(), workspace) {
		return nil, fmt.Errorf("%w: dispatch %d brief", ErrNotFound, dispatchSeq)
	}
	root, err := openRunWorkspaceRoot(workspace)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	briefs, err := openRuntimeAuthorityDirectoryAt(root, "briefs")
	if err != nil {
		return nil, evidenceOpenError(err)
	}
	defer briefs.Close()
	file, err := openRunArtifactFileAt(briefs, filepath.Base(briefPath), syscall.O_RDONLY, false)
	if err != nil {
		return nil, evidenceOpenError(err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	head, err := io.ReadAll(io.LimitReader(file, EvidenceBriefMaxBytes+utf8.UTFMax))
	if err != nil {
		return nil, err
	}
	redacted := redactEvidenceText(string(head))
	text, cut := CapEvidenceText(redacted, EvidenceBriefMaxBytes)
	size := int(info.Size())
	if int64(len(head)) >= info.Size() {
		size = len(redacted)
	}
	return &RunBriefEvidence{
		DispatchSeq: dispatch.Seq,
		NodeID:      dispatch.NodeID,
		SlotID:      dispatch.SlotID,
		Attempt:     dispatch.Attempt,
		Text:        EvidenceText{Text: text, Bytes: size, Truncated: cut || int64(len(head)) < info.Size()},
	}, nil
}

func evidenceBriefDirectory(directory string, roots ...string) bool {
	for _, root := range roots {
		if root != "" && directory == filepath.Join(filepath.Clean(root), "briefs") {
			return true
		}
	}
	return false
}

// openRunArtifactsRoot opens <workspace>/.formations/artifacts/<runID>.
func (s *Store) openRunArtifactsRoot(runID string) (*os.File, error) {
	if !validRunID(runID) {
		return nil, ErrNotFound
	}
	workspace, err := s.workspaceAbsolutePath()
	if err != nil {
		return nil, err
	}
	current, err := openRunWorkspaceRoot(workspace)
	if err != nil {
		return nil, err
	}
	for _, component := range []string{".formations", "artifacts", runID} {
		next, err := openRuntimeAuthorityDirectoryAt(current, component)
		_ = current.Close()
		if err != nil {
			return nil, evidenceOpenError(err)
		}
		current = next
	}
	return current, nil
}

// openRunArtifact walks a relative artifact name one component at a time.
func (s *Store) openRunArtifact(runID, name string) (*os.File, os.FileInfo, error) {
	components, ok := evidenceArtifactComponents(name)
	if !ok {
		return nil, nil, fmt.Errorf("%w: artifact name", ErrNotFound)
	}
	if _, err := s.ReadRunEvents(runID); err != nil {
		return nil, nil, evidenceNotFound(err)
	}
	current, err := s.openRunArtifactsRoot(runID)
	if err != nil {
		return nil, nil, err
	}
	for _, component := range components[:len(components)-1] {
		next, err := openRuntimeAuthorityDirectoryAt(current, component)
		_ = current.Close()
		if err != nil {
			return nil, nil, evidenceOpenError(err)
		}
		current = next
	}
	file, err := openRunArtifactFileAt(current, components[len(components)-1], syscall.O_RDONLY, false)
	_ = current.Close()
	if err != nil {
		return nil, nil, evidenceOpenError(err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	return file, info, nil
}

func evidenceArtifactComponents(name string) ([]string, bool) {
	if name == "" || strings.HasPrefix(name, "/") {
		return nil, false
	}
	components := strings.Split(name, "/")
	if len(components) > EvidenceArtifactListDepth {
		return nil, false
	}
	for _, component := range components {
		if !runtimeAuthorityPathComponent(component) {
			return nil, false
		}
	}
	return components, true
}

// evidenceOpenError turns a refused or missing component into ErrNotFound; a
// permission or I/O failure stays a server error.
func evidenceOpenError(err error) error {
	if errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EIO) {
		return err
	}
	return fmt.Errorf("%w: %v", ErrNotFound, err)
}

var evidenceImageTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
}

func evidenceArtifactKind(name string, head []byte, partial bool) string {
	extension := strings.ToLower(path.Ext(name))
	if _, ok := evidenceImageTypes[extension]; ok {
		return "image"
	}
	// A PDF is named and starts like one; the browser's viewer draws it.
	if extension == ".pdf" && bytes.HasPrefix(head, []byte("%PDF-")) {
		return "pdf"
	}
	if !evidenceTextual(head, partial) {
		return "binary"
	}
	switch extension {
	case ".md", ".markdown":
		return "markdown"
	case ".json":
		return "json"
	}
	return "text"
}

// evidenceTextual accepts UTF-8 without NUL bytes. When a read cap cut the
// file, up to three trailing bytes may belong to a split rune.
func evidenceTextual(raw []byte, partial bool) bool {
	if bytes.IndexByte(raw, 0) >= 0 {
		return false
	}
	if !partial {
		return utf8.Valid(raw)
	}
	for trim := 0; trim < utf8.UTFMax && trim <= len(raw); trim++ {
		if utf8.Valid(raw[:len(raw)-trim]) {
			return true
		}
	}
	return false
}
