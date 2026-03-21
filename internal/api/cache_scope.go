package api

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/gastownhall/wasteland/internal/sdk"
)

const (
	localCacheUpstream = "local"
	publicCacheViewer  = "anon"
)

type readIdentityContextKey string

type readCacheScope struct {
	upstream    string
	viewer      string
	mode        string
	impersonate string
	cacheable   bool
}

// ResolvedReadIdentity is the canonical hosted read identity used for cache
// scoping after middleware or public-read fallback resolves the active upstream
// and viewer bucket.
type ResolvedReadIdentity struct {
	Upstream string
	Viewer   string
	Public   bool
}

const resolvedReadIdentityContextKey readIdentityContextKey = "resolved-read-identity"

// WithResolvedReadIdentity annotates a request context with the canonical
// hosted upstream and viewer identity for downstream cache scoping.
func WithResolvedReadIdentity(ctx context.Context, identity ResolvedReadIdentity) context.Context {
	identity.Upstream = canonicalHostedCacheUpstream(identity.Upstream)
	identity.Viewer = strings.TrimSpace(identity.Viewer)
	return context.WithValue(ctx, resolvedReadIdentityContextKey, identity)
}

// ResolvedReadIdentityFromContext returns the hosted read identity when one has
// been established by middleware or the public-read fallback.
func ResolvedReadIdentityFromContext(ctx context.Context) (ResolvedReadIdentity, bool) {
	identity, ok := ctx.Value(resolvedReadIdentityContextKey).(ResolvedReadIdentity)
	return identity, ok
}

func (s *Server) readCacheScope(r *http.Request, client *sdk.Client) readCacheScope {
	scope := readCacheScope{
		mode:        strings.TrimSpace(client.Mode()),
		impersonate: strings.TrimSpace(r.Header.Get("X-Impersonate")),
		cacheable:   true,
	}

	if !s.hosted {
		scope.upstream = strings.TrimSpace(client.Upstream())
		scope.viewer = strings.TrimSpace(client.RigHandle())
		return normalizeReadCacheScope(scope, false)
	}

	if identity, ok := ResolvedReadIdentityFromContext(r.Context()); ok {
		if identity.Public {
			if identity.Upstream == "" {
				scope.cacheable = false
				return normalizeReadCacheScope(scope, true)
			}
			scope.upstream = identity.Upstream
			return normalizeReadCacheScope(scope, true)
		}
		if identity.Upstream == "" || identity.Viewer == "" {
			scope.cacheable = false
			return normalizeReadCacheScope(scope, true)
		}
		scope.upstream = identity.Upstream
		scope.viewer = identity.Viewer
		return normalizeReadCacheScope(scope, true)
	}

	// Hosted reads without canonical identity should not populate or reuse
	// shared cache entries. This fails closed on miswired handlers instead of
	// trusting request-derived client state.
	scope.cacheable = false
	return normalizeReadCacheScope(scope, true)
}

func normalizeReadCacheScope(scope readCacheScope, hosted bool) readCacheScope {
	if scope.upstream == "" {
		scope.upstream = localCacheUpstream
	}

	if hosted {
		scope.viewer = canonicalHostedCacheViewer(scope.viewer)
	}

	return scope
}

func browseCacheKey(scope readCacheScope, r *http.Request) string {
	return strings.Join([]string{
		"browse",
		cacheKeyPart(scope.upstream),
		cacheKeyPart(scope.viewer),
		cacheKeyPart(scope.mode),
		cacheKeyPart(scope.impersonate),
		cacheKeyPart(canonicalBrowseKey(r)),
	}, ":")
}

func browseCachePrefix(upstream string) string {
	return strings.Join([]string{"browse", cacheKeyPart(upstream)}, ":") + ":"
}

func detailCacheKey(scope readCacheScope, wantedID string) string {
	return strings.Join([]string{
		"detail",
		cacheKeyPart(scope.upstream),
		cacheKeyPart(scope.viewer),
		cacheKeyPart(scope.mode),
		cacheKeyPart(scope.impersonate),
		cacheKeyPart(wantedID),
	}, ":")
}

func detailCachePrefix(upstream string) string {
	return strings.Join([]string{"detail", cacheKeyPart(upstream)}, ":") + ":"
}

func detailCacheSuffix(wantedID string) string {
	return ":" + cacheKeyPart(wantedID)
}

func cacheKeyPart(value string) string {
	return url.QueryEscape(strings.TrimSpace(value))
}

func canonicalHostedCacheUpstream(upstream string) string {
	upstream = strings.TrimSpace(upstream)
	switch upstream {
	case "hop/wl-commons":
		return "wasteland/wl-commons"
	default:
		return upstream
	}
}

func canonicalHostedCacheViewer(viewer string) string {
	viewer = strings.TrimSpace(viewer)
	if viewer == "" {
		return publicCacheViewer
	}
	return "user:" + viewer
}
