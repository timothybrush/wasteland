import { describe, expect, it, vi } from "vitest";
import { defaultRuntimeConfig, loadRuntimeConfig, normalizeRuntimeConfig } from "./runtimeConfig";

describe("runtimeConfig", () => {
  it("normalizes a partial response", () => {
    expect(
      normalizeRuntimeConfig({
        browser_tracing_enabled: true,
        browser_trace_sample_ratio: 0.25,
      }),
    ).toEqual({
      environment: defaultRuntimeConfig().environment,
      browser_tracing_enabled: true,
      browser_trace_endpoint: "",
      browser_trace_sample_ratio: 0.25,
    });
  });

  it("loads runtime config from the api", async () => {
    const fetchImpl = vi.fn(
      async () =>
        new Response(
          JSON.stringify({
            environment: "staging",
            browser_tracing_enabled: true,
            browser_trace_endpoint: "/api/telemetry/v1/traces",
            browser_trace_sample_ratio: 0.4,
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        ),
    );

    await expect(loadRuntimeConfig(fetchImpl as unknown as typeof fetch)).resolves.toEqual({
      environment: "staging",
      browser_tracing_enabled: true,
      browser_trace_endpoint: "/api/telemetry/v1/traces",
      browser_trace_sample_ratio: 0.4,
    });
    expect(fetchImpl).toHaveBeenCalledWith("/api/runtime-config", {
      cache: "no-store",
      credentials: "same-origin",
      signal: expect.any(AbortSignal),
    });
  });

  it("fails open when the api request fails", async () => {
    const fetchImpl = vi.fn(async () => {
      throw new Error("boom");
    });

    await expect(loadRuntimeConfig(fetchImpl as unknown as typeof fetch)).resolves.toEqual(defaultRuntimeConfig());
  });

  it("fails open when the runtime config fetch is aborted", async () => {
    const fetchImpl = vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
      await new Promise((_, reject) => {
        init?.signal?.addEventListener("abort", () => reject(new DOMException("Aborted", "AbortError")));
      });
      return new Response();
    });

    await expect(loadRuntimeConfig(fetchImpl as unknown as typeof fetch, 1)).resolves.toEqual(defaultRuntimeConfig());
  });
});
