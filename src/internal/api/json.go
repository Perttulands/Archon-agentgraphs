package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/Perttulands/Archon-agentgraphs/internal/core"
)

const maxJSONRequestBytes int64 = 1 << 20

func decodeJSONBody(w http.ResponseWriter, r *http.Request, v any) bool {
	return decodeBody(w, r, v, nil)
}

// decodeStrictJSONBody refuses a field the request does not take with 422 and
// code, naming the field and why (unknown says it), rather than dropping it.
func decodeStrictJSONBody(w http.ResponseWriter, r *http.Request, v any, code string, unknown func(field string) string) bool {
	return decodeBody(w, r, v, func(field string) {
		core.WriteError(w, http.StatusUnprocessableEntity, code, unknown(field))
	})
}

func decodeBody(w http.ResponseWriter, r *http.Request, v any, refuseUnknown func(field string)) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONRequestBytes))
	if refuseUnknown != nil {
		decoder.DisallowUnknownFields()
	}
	if err := decoder.Decode(v); err != nil {
		if field, ok := strings.CutPrefix(err.Error(), "json: unknown field "); ok && refuseUnknown != nil {
			refuseUnknown(strings.Trim(field, `"`))
			return false
		}
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			core.WriteError(w, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "JSON body is too large")
			return false
		}
		core.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON body")
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			core.WriteError(w, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "JSON body is too large")
			return false
		}
		core.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON body")
		return false
	}
	return true
}
