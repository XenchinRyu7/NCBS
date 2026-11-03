package problem

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
)

// Details represents an RFC 7807 Problem Details for HTTP APIs object.
type Details struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail"`
	Instance string `json:"instance"`
	TraceID  string `json:"trace_id"`
}

// Render writes an RFC 7807 compliant error response with application/problem+json content type.
func Render(w http.ResponseWriter, r *http.Request, status int, title, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	traceID := middleware.GetReqID(r.Context())
	if traceID != "" {
		w.Header().Set("X-Request-ID", traceID)
	}
	w.WriteHeader(status)

	prob := Details{
		Type:     fmt.Sprintf("https://api.cbs-bank.dev/errors/ERR-%d", status),
		Title:    title,
		Status:   status,
		Detail:   detail,
		Instance: r.URL.Path,
		TraceID:  traceID,
	}
	_ = json.NewEncoder(w).Encode(prob)
}

