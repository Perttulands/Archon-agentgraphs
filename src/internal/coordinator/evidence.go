package coordinator

import (
	"bytes"
	"errors"
	"mime"
	"net/http"
	"os"
	"strconv"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// Run evidence routes serve a run's recorded content to the trusted operator
// (ADR-0017). Projections and SSE stay sanitized; these reads are capped and
// confined to the run's own ledger, artifact directory and dispatched briefs.
// The pending-gate read route in gate_request.go is this API's view of a human
// request still waiting for an answer.
func (c *Coordinator) registerEvidenceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/formations/runs/{runId}/evidence/nodes/{nodeId}", c.nodeEvidence)
	mux.HandleFunc("GET /api/formations/runs/{runId}/evidence/briefs/{dispatchSeq}", c.briefEvidence)
	mux.HandleFunc("GET /api/formations/runs/{runId}/evidence/artifacts", c.artifactList)
	mux.HandleFunc("GET /api/formations/runs/{runId}/evidence/artifacts/{name...}", c.artifactPreview)
	mux.HandleFunc("GET /api/formations/runs/{runId}/artifacts/{name...}", c.artifactRaw)
}

func (c *Coordinator) nodeEvidence(w http.ResponseWriter, r *http.Request) {
	evidence, err := c.store.ProjectNodeEvidence(r.PathValue("runId"), r.PathValue("nodeId"))
	if err != nil {
		evidenceFailure(w, err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"evidence": evidence})
}

func (c *Coordinator) briefEvidence(w http.ResponseWriter, r *http.Request) {
	seq, err := strconv.Atoi(r.PathValue("dispatchSeq"))
	if err != nil || seq <= 0 {
		reply(w, http.StatusNotFound, map[string]string{"error": "dispatch not found"})
		return
	}
	brief, err := c.store.ReadRunBrief(r.PathValue("runId"), seq)
	if err != nil {
		evidenceFailure(w, err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"brief": brief})
}

func (c *Coordinator) artifactList(w http.ResponseWriter, r *http.Request) {
	artifacts, truncated, err := c.store.ListRunArtifacts(r.PathValue("runId"))
	if err != nil {
		evidenceFailure(w, err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"artifacts": artifacts, "truncated": truncated})
}

func (c *Coordinator) artifactPreview(w http.ResponseWriter, r *http.Request) {
	preview, err := c.store.PreviewRunArtifact(r.PathValue("runId"), r.PathValue("name"))
	if err != nil {
		evidenceFailure(w, err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"artifact": preview})
}

// artifactRaw serves an artifact for a browser tab. It never serves active
// content: text is plain, only known image types keep their type, and the
// sandbox policy applies to anything a browser would still render.
func (c *Coordinator) artifactRaw(w http.ResponseWriter, r *http.Request) {
	content, err := c.store.ReadRunArtifact(r.PathValue("runId"), r.PathValue("name"))
	if err != nil {
		evidenceFailure(w, err)
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

func evidenceFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, formations.ErrNotFound), errors.Is(err, os.ErrNotExist):
		reply(w, http.StatusNotFound, map[string]string{"error": "run evidence not found"})
	case errors.Is(err, formations.ErrEvidenceTooLarge):
		reply(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "artifact exceeds the 16 MiB read limit; read it on the host"})
	default:
		reply(w, http.StatusInternalServerError, map[string]string{"error": "run evidence unavailable"})
	}
}
