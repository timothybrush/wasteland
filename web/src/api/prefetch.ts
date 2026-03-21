import { browse, getActiveUpstream } from "./client";
import type { BrowseResponse } from "./types";

// Only prefetch when the browser already has an explicit active upstream so
// browse and bootstrap share the same source of truth.
let prefetchPromise: Promise<BrowseResponse> | null = null;
let prefetchedUpstream: string | null = null;

function shouldPrefetch(): boolean {
  return (
    import.meta.env.MODE !== "test" &&
    typeof window !== "undefined" &&
    window.location.pathname === "/" &&
    !window.location.search &&
    !!getActiveUpstream()
  );
}

// Start fetching the default browse data after runtime telemetry init so the
// first hosted browse request carries trace headers.
export function startPrefetch() {
  if (prefetchPromise || prefetchedUpstream || !shouldPrefetch()) {
    return;
  }
  prefetchedUpstream = getActiveUpstream();
  prefetchPromise = browse().catch(() => null as unknown as BrowseResponse);
}

/**
 * Consume the prefetched browse response. Returns null if not available
 * (wrong page, already consumed, or fetch failed). Can only be consumed once.
 */
export function consumePrefetch(activeUpstream: string | null): Promise<BrowseResponse> | null {
  const p = prefetchPromise;
  const upstream = prefetchedUpstream;
  prefetchPromise = null;
  prefetchedUpstream = null;
  if (upstream !== activeUpstream) {
    return null;
  }
  return p;
}
