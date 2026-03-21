import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const originalFetch = globalThis.fetch;

describe("prefetch", () => {
  beforeEach(() => {
    vi.resetModules();
    vi.stubEnv("MODE", "production");
    localStorage.clear();
  });

  afterEach(() => {
    vi.unstubAllEnvs();
    globalThis.fetch = originalFetch;
    window.history.replaceState({}, "", "/");
  });

  it("prefetches default browse data on the root path when an active upstream is already known", async () => {
    window.history.replaceState({}, "", "/");
    localStorage.setItem("wl_active", "hop/wl-commons");
    globalThis.fetch = vi.fn(
      async () =>
        new Response(JSON.stringify({ items: [{ id: "w-1" }] }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
    ) as typeof fetch;

    const prefetch = await import("./prefetch");
    prefetch.startPrefetch();
    const cached = prefetch.consumePrefetch("hop/wl-commons");

    expect(cached).not.toBeNull();
    await expect(cached).resolves.toEqual({ items: [{ id: "w-1" }] });
    expect(globalThis.fetch).toHaveBeenCalledWith("/api/wanted", {
      headers: new Headers({ "X-Wasteland": "hop/wl-commons" }),
    });
    expect(prefetch.consumePrefetch("hop/wl-commons")).toBeNull();
  });

  it("skips prefetching when not on the default browse route", async () => {
    window.history.replaceState({}, "", "/wanted/w-1");
    localStorage.setItem("wl_active", "hop/wl-commons");
    globalThis.fetch = vi.fn() as typeof fetch;

    const prefetch = await import("./prefetch");
    prefetch.startPrefetch();

    expect(prefetch.consumePrefetch("hop/wl-commons")).toBeNull();
    expect(globalThis.fetch).not.toHaveBeenCalled();
  });

  it("skips prefetching when no active upstream is selected yet", async () => {
    window.history.replaceState({}, "", "/");
    globalThis.fetch = vi.fn() as typeof fetch;

    const prefetch = await import("./prefetch");
    prefetch.startPrefetch();

    expect(prefetch.consumePrefetch(null)).toBeNull();
    expect(globalThis.fetch).not.toHaveBeenCalled();
  });

  it("converts a failed prefetch into a null result", async () => {
    window.history.replaceState({}, "", "/");
    localStorage.setItem("wl_active", "hop/wl-commons");
    globalThis.fetch = vi.fn(
      async () => new Response("boom", { status: 500, statusText: "Server Error" }),
    ) as typeof fetch;

    const prefetch = await import("./prefetch");
    prefetch.startPrefetch();
    const cached = prefetch.consumePrefetch("hop/wl-commons");

    expect(cached).not.toBeNull();
    await expect(cached).resolves.toBeNull();
    expect(prefetch.consumePrefetch("hop/wl-commons")).toBeNull();
  });

  it("drops prefetched data when bootstrap selects a different upstream", async () => {
    window.history.replaceState({}, "", "/");
    localStorage.setItem("wl_active", "hop/wl-commons");
    globalThis.fetch = vi.fn(
      async () =>
        new Response(JSON.stringify({ items: [{ id: "w-1" }] }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
    ) as typeof fetch;

    const prefetch = await import("./prefetch");
    prefetch.startPrefetch();

    expect(prefetch.consumePrefetch("gastownhall/gascity")).toBeNull();
  });
});
