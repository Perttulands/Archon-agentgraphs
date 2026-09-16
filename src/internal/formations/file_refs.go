package formations

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

// Reference files named on missions, formation briefs and gates are read only
// under the daemon's configured file roots, with the run evidence confinement
// of ADR-0017: every component below the root opens with O_NOFOLLOW, only
// regular single-link files are read, reads are capped and served text is
// redacted.

// ErrFileOutsideRoots marks a reference that no configured root contains.
var ErrFileOutsideRoots = errors.New("file_outside_roots")

// FileRefMaxComponents bounds how deep below a root a reference may go.
const FileRefMaxComponents = 64

type FileRoots struct {
	roots []string
}

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

// NewFileRoots accepts absolute, clean paths of existing directories.
func NewFileRoots(paths []string) (*FileRoots, error) {
	roots := &FileRoots{}
	for _, root := range paths {
		if !filepath.IsAbs(root) || filepath.Clean(root) != root {
			return nil, fmt.Errorf("file root %q must be an absolute clean path", root)
		}
		info, err := os.Stat(root)
		if err != nil {
			return nil, fmt.Errorf("file root %q: %w", root, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("file root %q is not a directory", root)
		}
		duplicate := false
		for _, existing := range roots.roots {
			duplicate = duplicate || existing == root
		}
		if !duplicate {
			roots.roots = append(roots.roots, root)
		}
	}
	return roots, nil
}

// Roots lists the configured roots in the order given.
func (r *FileRoots) Roots() []string {
	if r == nil {
		return nil
	}
	return append([]string(nil), r.roots...)
}

// Preview reads the start of a referenced file.
func (r *FileRoots) Preview(ref string) (*ReferencedFilePreview, error) {
	file, info, resolved, err := r.open(ref)
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

// Read reads a whole referenced file for the raw route, up to the raw cap.
func (r *FileRoots) Read(ref string) (*RunArtifactContent, error) {
	file, info, resolved, err := r.open(ref)
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

// open resolves a reference. An absolute path is read under the deepest root
// that contains it. A relative path is tried under each root in order and the
// first readable file wins.
func (r *FileRoots) open(ref string) (*os.File, os.FileInfo, string, error) {
	if ref == "" || strings.ContainsRune(ref, 0) {
		return nil, nil, "", fmt.Errorf("%w: file reference", ErrNotFound)
	}
	roots := r.Roots()
	if filepath.IsAbs(ref) {
		if filepath.Clean(ref) != ref {
			return nil, nil, "", fmt.Errorf("%w: file reference must be a clean path", ErrNotFound)
		}
		best := ""
		for _, root := range roots {
			if (ref == root || strings.HasPrefix(ref, strings.TrimSuffix(root, "/")+"/")) && len(root) > len(best) {
				best = root
			}
		}
		if best == "" {
			return nil, nil, "", fmt.Errorf("%w: %s", ErrFileOutsideRoots, ref)
		}
		if ref == best {
			return nil, nil, "", fmt.Errorf("%w: a file root is not a file", ErrNotFound)
		}
		file, info, err := openUnderFileRoot(best, strings.TrimPrefix(strings.TrimPrefix(ref, best), "/"))
		return file, info, ref, err
	}
	if len(roots) == 0 {
		return nil, nil, "", fmt.Errorf("%w: %s", ErrFileOutsideRoots, ref)
	}
	var firstErr error
	for _, root := range roots {
		file, info, err := openUnderFileRoot(root, ref)
		if err == nil {
			return file, info, filepath.Join(root, ref), nil
		}
		if firstErr == nil || !errors.Is(err, ErrNotFound) {
			firstErr = err
		}
	}
	return nil, nil, "", firstErr
}

// openUnderFileRoot walks a relative path below a root one component at a time.
func openUnderFileRoot(root, relative string) (*os.File, os.FileInfo, error) {
	components := strings.Split(relative, "/")
	if len(components) > FileRefMaxComponents {
		return nil, nil, fmt.Errorf("%w: file reference is too deep", ErrNotFound)
	}
	for _, component := range components {
		if !runtimeAuthorityPathComponent(component) {
			return nil, nil, fmt.Errorf("%w: file reference component %q", ErrNotFound, component)
		}
	}
	// The root itself may be a symlink the operator configured; everything
	// below it is opened with O_NOFOLLOW relative to this descriptor.
	fd, err := syscall.Open(root, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NONBLOCK|syscall.O_DIRECTORY, 0)
	if err != nil {
		return nil, nil, evidenceOpenError(&os.PathError{Op: "open", Path: root, Err: err})
	}
	current := os.NewFile(uintptr(fd), root)
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
