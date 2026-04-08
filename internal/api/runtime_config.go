package api

import (
	"net/http"
	"os"
	"strings"

	"github.com/gastownhall/wasteland/internal/observability"
)

func (s *Server) effectiveEnvironment() string {
	if environment := strings.TrimSpace(s.environment); environment != "" {
		return environment
	}
	return strings.TrimSpace(os.Getenv("WL_ENVIRONMENT"))
}

func (s *Server) allowStagingImpersonation() bool {
	return s.effectiveEnvironment() == "staging"
}

func (s *Server) handleRuntimeConfig(w http.ResponseWriter, _ *http.Request) {
	resp := RuntimeConfigResponse{
		Environment:             s.effectiveEnvironment(),
		BrowserTracingEnabled:   observability.BrowserTracingEnabled(),
		BrowserTraceEndpoint:    observability.BrowserTraceIngressPath,
		BrowserTraceSampleRatio: observability.BrowserTraceSampleRatio(),
	}
	writeJSON(w, http.StatusOK, resp)
}
