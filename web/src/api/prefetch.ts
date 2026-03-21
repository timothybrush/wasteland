import { browse, getActiveUpstream } from "./client";
import type { BrowseResponse } from "./types";

// Start fetching the default browse data immediately when the module loads,
// before React mounts. Only prefetch when the browser already has an explicit
// active upstream so browse and bootstrap share the same source of truth.
let prefetchPromise: Promise<BrowseResponse> | null = null;
let prefetchedUpstream: string | null = null;

// Only prefetch for the root path with no query params (default browse).
// Skip in test environments where fetch is mocked after module load.
const isTest = import.meta.env.MODE === "test";
if (
  !isTest &&
  typeof window !== "undefined" &&
  window.location.pathname === "/" &&
  !window.location.search &&
  getActiveUpstream()
) {
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
