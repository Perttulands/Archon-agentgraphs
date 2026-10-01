package formations

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// Reference files named on missions, formation briefs and gates open the way
// CHROTE's file viewer opens a file: any absolute path the daemon can read,
// following symlinks (ADR-0021). Only a regular file is read, because opening
// a FIFO or device would block or never end, and reads are capped. Served text
// is redacted like run evidence.

// ErrRelativeFileRef marks a reference with no base to resolve against.
var ErrRelativeFileRef = errors.New("relative file reference")

// ReferencedFilePreview is the start of a referenced file, classified like a
// run artifact preview. Path is the absolute path that was read.
type ReferencedFilePreview struct {
	Path       string        `json:"path"`
	Name       string        `json:"name"`
	Size       int64         `json:"size"`
	ModifiedAt string        `json:"modifiedAt"`
	Kind       string        `json:"kind"`
	Text       *EvidenceText `json:"text,omitempty"`
}

// PreviewReferencedFile reads the start of a referenced file.
func PreviewReferencedFile(ref string) (*ReferencedFilePreview, error) {
	file, info, resolved, err := openReferencedFile(ref)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	head, err := io.ReadAll(io.LimitReader(file, EvidenceArtifactPreviewMaxBytes+utf8.UTFMax))
	if err != nil {
		return nil, err
	}
	partial := int64(len(head)) < info.Size()
	preview := &ReferencedFilePreview{Path: resolved, Name: path.Base(resolved), Size: info.Size(), ModifiedAt: info.ModTime().UTC().Format(time.RFC3339), Kind: evidenceArtifactKind(resolved, head, partial)}
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

// ReadReferencedFile reads a whole referenced file for the raw route, up to
// the raw cap.
func ReadReferencedFile(ref string) (*RunArtifactContent, error) {
	file, info, resolved, err := openReferencedFile(ref)
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
	return evidenceRawContent(resolved, info.ModTime(), body), nil
}

// openReferencedFile opens an absolute reference as authored.
func openReferencedFile(ref string) (*os.File, os.FileInfo, string, error) {
	if ref == "" || strings.ContainsRune(ref, 0) {
		return nil, nil, "", fmt.Errorf("%w: file reference", ErrNotFound)
	}
	if !filepath.IsAbs(ref) {
		return nil, nil, "", fmt.Errorf("%w: %s", ErrRelativeFileRef, ref)
	}
	resolved := filepath.Clean(ref)
	file, info, err := openRegularFile(resolved)
	if err != nil {
		return nil, nil, "", err
	}
	return file, info, resolved, nil
}
