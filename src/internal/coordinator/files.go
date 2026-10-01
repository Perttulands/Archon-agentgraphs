package coordinator

import (
	"bytes"
	"errors"
	"mime"
	"net/http"
	"os"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// Referenced files (mission, formation brief and gate files) open by absolute
// path anywhere the daemon can read, as in CHROTE (ADR-0021).

func (c *Coordinator) registerFileRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/formations/files/preview", c.filePreview)
	mux.HandleFunc("GET /api/formations/files/raw", c.fileRaw)
}

func (c *Coordinator) filePreview(w http.ResponseWriter, r *http.Request) {
	preview, err := formations.PreviewReferencedFile(r.URL.Query().Get("path"))
	if err != nil {
		fileFailure(w, err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"file": preview})
}

// fileRaw serves a referenced file like a raw run artifact. The file window is
// a reader: never active content, text as text/plain, only known image types
// kept.
func (c *Coordinator) fileRaw(w http.ResponseWriter, r *http.Request) {
	content, err := formations.ReadReferencedFile(r.URL.Query().Get("path"))
	if err != nil {
		fileFailure(w, err)
		return
	}
	disposition := "inline"
	if content.ContentType == "application/octet-stream" {
		disposition = "attachment"
	}
	header := w.Header()
	header.Set("Content-Type", content.ContentType)
	header.Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": content.Name}))
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Content-Security-Policy", "sandbox; default-src 'none'; img-src 'self'; style-src 'unsafe-inline'")
	header.Set("Cache-Control", "no-store")
	http.ServeContent(w, r, "", content.ModifiedAt, bytes.NewReader(content.Body))
}

func fileFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, formations.ErrRelativeFileRef):
		reply(w, http.StatusBadRequest, map[string]string{"error": "a relative file reference has no base here; name the file by its absolute path"})
	case errors.Is(err, os.ErrPermission):
		reply(w, http.StatusForbidden, map[string]string{"error": "the daemon's user may not read this file"})
	case errors.Is(err, formations.ErrNotFound), errors.Is(err, os.ErrNotExist):
		reply(w, http.StatusNotFound, map[string]string{"error": "file not found"})
	case errors.Is(err, formations.ErrEvidenceTooLarge):
		reply(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "file exceeds the 16 MiB read limit; read it on the host"})
	default:
		reply(w, http.StatusInternalServerError, map[string]string{"error": "file unavailable"})
	}
}
