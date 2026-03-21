import { type Tracer, trace } from "@opentelemetry/api";
import { OTLPTraceExporter } from "@opentelemetry/exporter-trace-otlp-http";
import { registerInstrumentations } from "@opentelemetry/instrumentation";
import { FetchInstrumentation } from "@opentelemetry/instrumentation-fetch";
import { XMLHttpRequestInstrumentation } from "@opentelemetry/instrumentation-xml-http-request";
import { resourceFromAttributes } from "@opentelemetry/resources";
import { BatchSpanProcessor, TraceIdRatioBasedSampler, WebTracerProvider } from "@opentelemetry/sdk-trace-web";
import type { RuntimeConfigResponse } from "../api/types";

const telemetryIngressSuffix = "/api/telemetry/v1/traces";
const serviceName = "wasteland-web";
const serviceNamespace = "wasteland";

let initialized = false;
let disposeNavigationTracing: (() => void) | null = null;
let lastTracedURL = "";

function buildSameOriginMatchers(origin: string): RegExp[] {
  const escapedOrigin = origin.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return [/^\//, new RegExp(`^${escapedOrigin}/`)];
}

function currentRouteURL(): string {
  return `${window.location.pathname}${window.location.search}${window.location.hash}`;
}

function normalizeRouteURL(input?: string | URL | null): string {
  if (!input) {
    return currentRouteURL();
  }
  try {
    const url =
      typeof input === "string" || input instanceof URL ? new URL(input.toString(), window.location.origin) : null;
    if (url) {
      return `${url.pathname}${url.search}${url.hash}`;
    }
  } catch {
    return currentRouteURL();
  }
  return currentRouteURL();
}

function traceDocumentLoad(tracer: Tracer) {
  lastTracedURL = currentRouteURL();

  const navigation = performance.getEntriesByType?.("navigation")?.[0] as PerformanceNavigationTiming | undefined;
  if (!navigation) {
    return;
  }

  const startTime = performance.timeOrigin + navigation.startTime;
  const endTime = startTime + navigation.duration;
  const span = tracer.startSpan("document-load", {
    attributes: {
      "app.route.url": lastTracedURL,
      "navigation.type": navigation.type || "navigate",
    },
    startTime,
  });
  span.end(endTime);
}

function traceNavigation(tracer: Tracer, source: string, target?: string | URL | null) {
  const routeURL = normalizeRouteURL(target);
  if (routeURL === lastTracedURL) {
    return;
  }
  lastTracedURL = routeURL;

  const span = tracer.startSpan("navigation", {
    attributes: {
      "app.route.source": source,
      "app.route.url": routeURL,
    },
  });

  if (typeof requestAnimationFrame === "function") {
    requestAnimationFrame(() => span.end());
    return;
  }
  setTimeout(() => span.end(), 0);
}

function installNavigationTracing(tracer: Tracer): () => void {
  traceDocumentLoad(tracer);

  const historyRef = window.history;
  const originalPushState = historyRef.pushState;
  const originalReplaceState = historyRef.replaceState;
  const onPopState = () => traceNavigation(tracer, "popstate");

  historyRef.pushState = function pushState(data: unknown, unused: string, url?: string | URL | null) {
    originalPushState.call(historyRef, data, unused, url ?? null);
    traceNavigation(tracer, "pushState", url);
  };

  historyRef.replaceState = function replaceState(data: unknown, unused: string, url?: string | URL | null) {
    originalReplaceState.call(historyRef, data, unused, url ?? null);
    traceNavigation(tracer, "replaceState", url);
  };

  window.addEventListener("popstate", onPopState);

  return () => {
    historyRef.pushState = originalPushState;
    historyRef.replaceState = originalReplaceState;
    window.removeEventListener("popstate", onPopState);
    lastTracedURL = "";
  };
}

export function initBrowserTracing(config: RuntimeConfigResponse): boolean {
  if (
    initialized ||
    typeof window === "undefined" ||
    !config.browser_tracing_enabled ||
    !config.browser_trace_endpoint ||
    (config.browser_trace_sample_ratio ?? 0) <= 0
  ) {
    return false;
  }

  const provider = new WebTracerProvider({
    resource: resourceFromAttributes({
      "deployment.environment.name": config.environment || "development",
      "service.name": serviceName,
      "service.namespace": serviceNamespace,
    }),
    sampler: new TraceIdRatioBasedSampler(config.browser_trace_sample_ratio ?? 0),
    spanProcessors: [
      new BatchSpanProcessor(
        new OTLPTraceExporter({
          url: config.browser_trace_endpoint,
        }),
      ),
    ],
  });
  provider.register();
  disposeNavigationTracing = installNavigationTracing(trace.getTracer(serviceName));

  const sameOriginMatchers = buildSameOriginMatchers(window.location.origin);
  registerInstrumentations({
    tracerProvider: provider,
    instrumentations: [
      new FetchInstrumentation({
        clearTimingResources: true,
        ignoreUrls: [new RegExp(`${telemetryIngressSuffix}$`)],
        propagateTraceHeaderCorsUrls: sameOriginMatchers,
      }),
      new XMLHttpRequestInstrumentation({
        ignoreUrls: [new RegExp(`${telemetryIngressSuffix}$`)],
        propagateTraceHeaderCorsUrls: sameOriginMatchers,
      }),
    ],
  });

  initialized = true;
  return true;
}

export function resetBrowserTracingForTests() {
  disposeNavigationTracing?.();
  disposeNavigationTracing = null;
  lastTracedURL = "";
  initialized = false;
}
