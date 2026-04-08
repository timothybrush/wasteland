package hosted

import (
	"context"

	"github.com/gastownhall/wasteland/internal/sdk"
)

type contextKey string

const (
	clientContextKey     contextKey = "hosted-client"
	workspaceContextKey  contextKey = "hosted-workspace"
	connectionContextKey contextKey = "hosted-connection-id"
)

// ClientFromContext extracts the sdk.Client injected by hosted auth middleware.
func ClientFromContext(ctx context.Context) (*sdk.Client, bool) {
	client, ok := ctx.Value(clientContextKey).(*sdk.Client)
	return client, ok
}

// WorkspaceFromContext extracts the sdk.Workspace injected by hosted auth middleware.
func WorkspaceFromContext(ctx context.Context) (*sdk.Workspace, bool) {
	ws, ok := ctx.Value(workspaceContextKey).(*sdk.Workspace)
	return ws, ok
}

// ConnectionIDFromContext extracts the active hosted connection ID injected by
// auth middleware.
func ConnectionIDFromContext(ctx context.Context) (string, bool) {
	connectionID, ok := ctx.Value(connectionContextKey).(string)
	return connectionID, ok && connectionID != ""
}
