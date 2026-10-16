package formations

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Every attempt of every step, judges included, has its own artifact
// directory, .archon/artifacts/<run>/<node>/<attempt>/, created at dispatch
// (archon-5tg1). The seat is told to write the files it produces there. When
// the attempt's output is accepted, each file or folder output whose path lies
// outside that directory is copied in, to outputs/<port>/<basename>, so a later
// attempt that rewrites the same workspace file in place cannot change what an
// earlier attempt produced. The copy is what the ledger records and routes.

// attemptOutputsDir holds an attempt's snapshots of its file and folder outputs.
const attemptOutputsDir = "outputs"

// ficlone is the Linux FICLONE ioctl, a copy-on-write clone of a whole file.
const ficlone = 0x40049409

// attemptArtifactDir is the absolute artifact directory of one attempt.
func (s *Store) attemptArtifactDir(runID, nodeID string, attempt int) (string, error) {
	if !validRunID(runID) || !validPathComponent(nodeID) {
		return "", fmt.Errorf("%w: attempt directory for %q in %q", ErrRunLedgerInvalid, nodeID, runID)
	}
	workspace, err := s.workspaceAbsolutePath()
	if err != nil {
		return "", err
	}
	return filepath.Join(workspace, ".archon", "artifacts", runID, nodeID, strconv.Itoa(attempt)), nil
}

// createAttemptArtifactDir makes an attempt's artifact directory.
func (s *Store) createAttemptArtifactDir(runID, nodeID string, attempt int) (string, error) {
	dir, err := s.attemptArtifactDir(runID, nodeID, attempt)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o770); err != nil {
		return "", runExecutionError("unavailable_artifact_dir", fmt.Sprintf("attempt artifact directory %s could not be created", dir), "engine", err)
	}
	return dir, nil
}

// latestNodeAttempt is the attempt of a node's latest node_started.
func latestNodeAttempt(events []RunEvent, nodeID string) int {
	attempt := 0
	for _, event := range events {
		if event.Type == RunEventNodeStarted && event.NodeID == nodeID {
			attempt = event.Attempt
		}
	}
	return attempt
}

// acceptAttemptOutputs checks an attempt's file and folder outputs and
// snapshots each into the attempt's directory. A text output gets its kind and
// nothing else. The check fails like an unreadable ref does: the step blocks.
func (s *Store) acceptAttemptOutputs(runID string, formation FormationNode, attempt int, outputs map[string]FormationOutputPayload) error {
	if len(outputs) == 0 {
		return nil
	}
	var dir string
	for _, port := range formation.Outputs {
		payload, ok := outputs[port.ID]
		if !ok {
			continue
		}
		kind := PortKind(port)
		if kind == PortKindText {
			continue
		}
		path, err := outputPortPath(formation.ID, port, payload.Ref)
		if err != nil {
			return err
		}
		if dir == "" {
			if dir, err = s.createAttemptArtifactDir(runID, formation.ID, attempt); err != nil {
				return err
			}
		}
		if !resolvedPathWithin(path, dir) {
			if path, err = snapshotOutput(path, filepath.Join(dir, attemptOutputsDir, port.ID), append(s.runArtifactRoots(runID), dir)); err != nil {
				return runExecutionError("unavailable_output_ref", fmt.Sprintf("%s output %s at %s could not be copied into the attempt directory: %v", formation.ID, port.ID, payload.Ref, err), "engine", err)
			}
		}
		payload.Ref = path
		payload.Kind = kind
		outputs[port.ID] = payload
	}
	return nil
}

// outputPortPath checks a file or folder output's ref: an absolute path to an
// existing regular file, or directory, as the port's kind says.
func outputPortPath(nodeID string, port FormationPort, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	kind := PortKind(port)
	switch {
	case ref == "":
		return "", runExecutionError("invalid_output_ref", fmt.Sprintf("formation %q output %q is a %s port; its ref must be the absolute path of the %s", nodeID, port.ID, kind, kind), "engine", nil)
	case strings.ContainsRune(ref, 0) || strings.Contains(ref, "://") || !filepath.IsAbs(ref):
		return "", runExecutionError("invalid_output_ref", fmt.Sprintf("formation %q output %q ref %q must be an absolute path", nodeID, port.ID, ref), "engine", nil)
	}
	path := filepath.Clean(ref)
	info, err := os.Stat(path)
	if err != nil {
		return "", runExecutionError("unavailable_output_ref", fmt.Sprintf("formation %q output %q names %s, which does not exist or cannot be read", nodeID, port.ID, path), "engine", err)
	}
	if kind == PortKindFolder && !info.IsDir() {
		return "", runExecutionError("invalid_output_ref", fmt.Sprintf("formation %q output %q is a folder port; %s is not a directory", nodeID, port.ID, path), "engine", nil)
	}
	if kind == PortKindFile && !info.Mode().IsRegular() {
		return "", runExecutionError("invalid_output_ref", fmt.Sprintf("formation %q output %q is a file port; %s is not a regular file", nodeID, port.ID, path), "engine", nil)
	}
	return path, nil
}

func pathWithin(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// resolvedPathWithin says whether path, its symlinks followed, lies in dir, so
// a link in the attempt directory to a workspace file is still snapshotted.
func resolvedPathWithin(path, dir string) bool {
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false
	}
	return pathWithin(resolvedPath, resolvedDir)
}

// snapshotOutput copies a file or a directory tree into parent, keeping its
// base name, and returns the copy's path. A directory's symlinks are copied as
// links and its special files are left out. The excluded directories inside
// the tree, the run's artifact directory and the attempt's own, are not
// copied, so a folder output that holds the workspace cannot copy itself.
func snapshotOutput(source, parent string, exclude []string) (string, error) {
	if err := os.MkdirAll(parent, 0o770); err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(source)
	if err != nil {
		return "", err
	}
	name := filepath.Base(source)
	if name == string(filepath.Separator) || name == "." {
		name = "root"
	}
	target := filepath.Join(parent, name)
	// An attempt accepted again, after a restart, replaces its unrecorded copy.
	if err := os.RemoveAll(target); err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return target, cloneOrCopyFile(resolved, target, info.Mode().Perm())
	}
	skipped := map[string]bool{}
	for _, dir := range append(exclude, parent) {
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			dir = real
		}
		if dir != resolved && pathWithin(dir, resolved) {
			skipped[dir] = true
		}
	}
	err = filepath.WalkDir(resolved, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if skipped[path] {
			return filepath.SkipDir
		}
		rel, err := filepath.Rel(resolved, path)
		if err != nil {
			return err
		}
		dest := filepath.Join(target, rel)
		switch mode := entry.Type(); {
		case mode.IsDir():
			return os.MkdirAll(dest, 0o770)
		case mode&fs.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, dest)
		case mode.IsRegular():
			info, err := entry.Info()
			if err != nil {
				return err
			}
			return cloneOrCopyFile(path, dest, info.Mode().Perm())
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return target, nil
}

// cloneOrCopyFile writes a copy of source to a new file at target: a
// copy-on-write clone where the file system supports FICLONE, otherwise an
// ordinary copy. Never a hard link, so rewriting the source leaves it intact.
func cloneOrCopyFile(source, target string, perm fs.FileMode) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm|0o600)
	if err != nil {
		return err
	}
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, out.Fd(), ficlone, in.Fd()); errno != 0 {
		if _, err := io.Copy(out, in); err != nil {
			_ = out.Close()
			return err
		}
	}
	return out.Close()
}
