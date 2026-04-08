package hosted

import (
	"testing"
	"time"
)

func TestAuthServiceWorkspaceResolver_InvalidateConnectionClearsPendingCaches(t *testing.T) {
	resolver := NewAuthServiceWorkspaceResolver(nil, NewSessionStore())
	cache1 := newPendingUpstreamCache(nil, "hop", "wl-commons", time.Hour)
	cache2 := newPendingUpstreamCache(nil, "gastownhall", "gascity", time.Hour)
	defer cache1.Stop()
	defer cache2.Stop()

	resolver.pendingCache["conn-1:hop/wl-commons"] = cache1
	resolver.pendingCache["conn-2:gastownhall/gascity"] = cache2

	resolver.InvalidateConnection("conn-1")

	if _, ok := resolver.pendingCache["conn-1:hop/wl-commons"]; ok {
		t.Fatal("expected conn-1 pending cache to be evicted")
	}
	if _, ok := resolver.pendingCache["conn-2:gastownhall/gascity"]; !ok {
		t.Fatal("expected unrelated pending cache to remain")
	}
}
