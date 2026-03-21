// Package ctxutil provides small context helpers for bounded background work.
package ctxutil

import (
	"context"
	"time"
)

const defaultDetachedTimeout = 30 * time.Second

// Detached returns a context that ignores parent cancellation and deadline.
// Shared work should not inherit the first caller's timeout budget; fallback
// provides the explicit upper bound instead.
func Detached(ctx context.Context, fallback time.Duration) (context.Context, context.CancelFunc) {
	base := context.WithoutCancel(ctx)
	if fallback <= 0 {
		fallback = defaultDetachedTimeout
	}
	return context.WithTimeout(base, fallback)
}
