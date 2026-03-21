package api

import (
	"net/http"
	"os"

	"github.com/gastownhall/wasteland/internal/observability"
)

func (s *Server) handleRuntimeConfig(w http.ResponseWriter, _ *http.Request) {
	environment := s.environment
	if environment == "" {
		environment = os.Getenv("WL_ENVIRONMENT")
	}
	resp := RuntimeConfigResponse{
		Environment:             environment,
		BrowserTracingEnabled:   observability.BrowserTracingEnabled(),
		BrowserTraceEndpoint:    observability.BrowserTraceIngressPath,
		BrowserTraceSampleRatio: observability.BrowserTraceSampleRatio(),
	}
	writeJSON(w, http.StatusOK, resp)
}
