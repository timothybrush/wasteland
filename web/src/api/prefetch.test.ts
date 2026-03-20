import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const originalFetch = globalThis.fetch;

describe("prefetch", () => {
  beforeEach(() => {
    vi.resetModules();
    vi.stubEnv("MODE", "production");
  });

  afterEach(() => {
    vi.unstubAllEnvs();
    globalThis.fetch = originalFetch;
    window.history.replaceState({}, "", "/");
  });

  it("prefetches default browse data on the root path and consumes it once", async () => {
    window.history.replaceState({}, "", "/");
    globalThis.fetch = vi.fn(
      async () =>
        new Response(JSON.stringify({ items: [{ id: "w-1" }] }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
    ) as typeof fetch;

    const prefetch = await import("./prefetch");
    const cached = prefetch.consumePrefetch();

    expect(cached).not.toBeNull();
    await expect(cached).resolves.toEqual({ items: [{ id: "w-1" }] });
    expect(globalThis.fetch).toHaveBeenCalledWith("/api/wanted");
    expect(prefetch.consumePrefetch()).toBeNull();
  });

  it("skips prefetching when not on the default browse route", async () => {
    window.history.replaceState({}, "", "/wanted/w-1");
    globalThis.fetch = vi.fn() as typeof fetch;

    const prefetch = await import("./prefetch");

    expect(prefetch.consumePrefetch()).toBeNull();
    expect(globalThis.fetch).not.toHaveBeenCalled();
  });

  it("converts a failed prefetch into a null result", async () => {
    window.history.replaceState({}, "", "/");
    globalThis.fetch = vi.fn(
      async () => new Response("boom", { status: 500, statusText: "Server Error" }),
    ) as typeof fetch;

    const prefetch = await import("./prefetch");
    const cached = prefetch.consumePrefetch();

    expect(cached).not.toBeNull();
    await expect(cached).resolves.toBeNull();
    expect(prefetch.consumePrefetch()).toBeNull();
  });
});
