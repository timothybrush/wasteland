package hosted

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/gastownhall/wasteland/internal/api"
	"github.com/gastownhall/wasteland/internal/sdk"
)

// NewClientFunc returns a ClientFunc that reads the client from request context.
// This bridges hosted auth middleware with api.Server's ClientFunc pattern.
func NewClientFunc() api.ClientFunc {
	return func(r *http.Request) (*sdk.Client, error) {
		client, ok := ClientFromContext(r.Context())
		if !ok {
			return nil, errNotAuthenticated
		}
		return client, nil
	}
}

// NewWorkspaceFunc returns a WorkspaceFunc that reads the workspace from request context.
func NewWorkspaceFunc() api.WorkspaceFunc {
	return func(r *http.Request) (*sdk.Workspace, error) {
		ws, ok := WorkspaceFromContext(r.Context())
		if !ok {
			return nil, errNotAuthenticated
		}
		return ws, nil
	}
}

var errNotAuthenticated = &authError{"not authenticated"}

type authError struct{ msg string }

func (e *authError) Error() string { return e.msg }

// healthHandler returns an HTTP handler that checks DoltHub API reachability.
// A GET to /healthz returns 200 with {"status":"ok","dolthub":"ok"} when
// DoltHub responds, or 200 with {"status":"ok","dolthub":"unreachable"} when it
// doesn't (the process is healthy even if upstream is degraded).
func healthHandler() http.HandlerFunc {
	client := &http.Client{Timeout: 3 * time.Second}
	probe := PublicDoltHubQueryURL("SELECT 1")

	return func(w http.ResponseWriter, _ *http.Request) {
		dolthub := "ok"
		resp, err := client.Get(probe)
		if err != nil {
			dolthub = "unreachable"
			slog.Warn("healthz: DoltHub probe failed", "error", err)
		} else {
			_ = resp.Body.Close()
			if resp.StatusCode >= 500 {
				dolthub = "degraded"
				slog.Warn("healthz: DoltHub probe returned error", "status", resp.StatusCode)
			}
		}

		writeJSON(w, http.StatusOK, map[string]string{
			"status":  "ok",
			"dolthub": dolthub,
		})
	}
}

// writeJSON writes a JSON response inside the hosted package without importing
// the api package's internal helpers.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
