import type { RuntimeConfigResponse } from "./types";

const defaultEnvironment = import.meta.env.VITE_ENVIRONMENT || "development";
const defaultRuntimeConfigTimeoutMs = 750;

export function defaultRuntimeConfig(): RuntimeConfigResponse {
  return {
    environment: defaultEnvironment,
    browser_tracing_enabled: false,
    browser_trace_endpoint: "",
    browser_trace_sample_ratio: 0,
  };
}

export function normalizeRuntimeConfig(
  value: Partial<RuntimeConfigResponse> | null | undefined,
): RuntimeConfigResponse {
  const fallback = defaultRuntimeConfig();
  return {
    environment:
      typeof value?.environment === "string" && value.environment !== "" ? value.environment : fallback.environment,
    browser_tracing_enabled: value?.browser_tracing_enabled === true,
    browser_trace_endpoint:
      typeof value?.browser_trace_endpoint === "string"
        ? value.browser_trace_endpoint
        : fallback.browser_trace_endpoint,
    browser_trace_sample_ratio:
      typeof value?.browser_trace_sample_ratio === "number"
        ? value.browser_trace_sample_ratio
        : fallback.browser_trace_sample_ratio,
  };
}

export async function loadRuntimeConfig(
  fetchImpl: typeof fetch = fetch,
  timeoutMs = defaultRuntimeConfigTimeoutMs,
): Promise<RuntimeConfigResponse> {
  const controller = typeof AbortController !== "undefined" ? new AbortController() : null;
  const timeoutId =
    controller && typeof setTimeout === "function" ? setTimeout(() => controller.abort(), timeoutMs) : null;
  try {
    const resp = await fetchImpl("/api/runtime-config", {
      cache: "no-store",
      credentials: "same-origin",
      signal: controller?.signal,
    });
    if (!resp.ok) {
      return defaultRuntimeConfig();
    }
    const body = (await resp.json()) as Partial<RuntimeConfigResponse>;
    return normalizeRuntimeConfig(body);
  } catch {
    return defaultRuntimeConfig();
  } finally {
    if (timeoutId !== null) {
      clearTimeout(timeoutId);
    }
  }
}
