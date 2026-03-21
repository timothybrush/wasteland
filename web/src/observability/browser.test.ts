import { beforeEach, describe, expect, it, vi } from "vitest";

const mocked = vi.hoisted(() => ({
  getTracer: vi.fn(),
  startSpan: vi.fn(),
  endSpan: vi.fn(),
  resourceFromAttributes: vi.fn((attrs) => ({ attrs })),
  exporter: vi.fn(),
  batchSpanProcessor: vi.fn(),
  tracerProvider: vi.fn(),
  register: vi.fn(),
  registerInstrumentations: vi.fn(),
  fetchInstrumentation: vi.fn(),
  xhrInstrumentation: vi.fn(),
  sampler: vi.fn(),
}));

vi.mock("@opentelemetry/api", () => ({
  trace: {
    getTracer: mocked.getTracer,
  },
}));

vi.mock("@opentelemetry/resources", () => ({
  resourceFromAttributes: mocked.resourceFromAttributes,
}));

vi.mock("@opentelemetry/exporter-trace-otlp-http", () => ({
  OTLPTraceExporter: class {
    constructor(...args: unknown[]) {
      mocked.exporter(...args);
    }
  },
}));

vi.mock("@opentelemetry/sdk-trace-web", () => ({
  BatchSpanProcessor: class {
    constructor(...args: unknown[]) {
      mocked.batchSpanProcessor(...args);
    }
  },
  TraceIdRatioBasedSampler: class {
    constructor(...args: unknown[]) {
      mocked.sampler(...args);
    }
  },
  WebTracerProvider: class {
    register = mocked.register;

    constructor(...args: unknown[]) {
      mocked.tracerProvider(...args);
    }
  },
}));

vi.mock("@opentelemetry/instrumentation", () => ({
  registerInstrumentations: mocked.registerInstrumentations,
}));

vi.mock("@opentelemetry/instrumentation-fetch", () => ({
  FetchInstrumentation: class {
    constructor(...args: unknown[]) {
      mocked.fetchInstrumentation(...args);
    }
  },
}));

vi.mock("@opentelemetry/instrumentation-xml-http-request", () => ({
  XMLHttpRequestInstrumentation: class {
    constructor(...args: unknown[]) {
      mocked.xhrInstrumentation(...args);
    }
  },
}));

describe("browser tracing", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.resetModules();
    mocked.getTracer.mockReset();
    mocked.startSpan.mockReset();
    mocked.endSpan.mockReset();
    mocked.resourceFromAttributes.mockClear();
    mocked.exporter.mockClear();
    mocked.batchSpanProcessor.mockClear();
    mocked.tracerProvider.mockClear();
    mocked.register.mockClear();
    mocked.registerInstrumentations.mockClear();
    mocked.fetchInstrumentation.mockClear();
    mocked.xhrInstrumentation.mockClear();
    mocked.sampler.mockClear();
    mocked.getTracer.mockReturnValue({
      startSpan: mocked.startSpan.mockImplementation(() => ({
        end: mocked.endSpan,
      })),
    });
    vi.spyOn(performance, "getEntriesByType").mockImplementation((type) => {
      if (type !== "navigation") {
        return [] as unknown as PerformanceEntryList;
      }
      return [{ duration: 125, startTime: 0, type: "navigate" }] as unknown as PerformanceEntryList;
    });
  });

  it("initializes browser tracing once for same-origin api requests", async () => {
    const { initBrowserTracing, resetBrowserTracingForTests } = await import("./browser");
    resetBrowserTracingForTests();

    expect(
      initBrowserTracing({
        environment: "staging",
        browser_tracing_enabled: true,
        browser_trace_endpoint: "/api/telemetry/v1/traces",
        browser_trace_sample_ratio: 0.3,
      }),
    ).toBe(true);
    expect(
      initBrowserTracing({
        environment: "staging",
        browser_tracing_enabled: true,
        browser_trace_endpoint: "/api/telemetry/v1/traces",
        browser_trace_sample_ratio: 0.3,
      }),
    ).toBe(false);

    expect(mocked.sampler).toHaveBeenCalledWith(0.3);
    expect(mocked.exporter).toHaveBeenCalledWith({ url: "/api/telemetry/v1/traces" });
    expect(mocked.register).toHaveBeenCalled();
    expect(mocked.getTracer).toHaveBeenCalledWith("wasteland-web");
    expect(mocked.startSpan).toHaveBeenCalledWith(
      "document-load",
      expect.objectContaining({
        attributes: expect.objectContaining({
          "app.route.url": expect.any(String),
          "navigation.type": "navigate",
        }),
      }),
    );
    expect(mocked.fetchInstrumentation).toHaveBeenCalledWith(
      expect.objectContaining({
        clearTimingResources: true,
        ignoreUrls: [expect.any(RegExp)],
        propagateTraceHeaderCorsUrls: [expect.any(RegExp), expect.any(RegExp)],
      }),
    );
    expect(mocked.xhrInstrumentation).toHaveBeenCalledWith(
      expect.objectContaining({
        ignoreUrls: [expect.any(RegExp)],
        propagateTraceHeaderCorsUrls: [expect.any(RegExp), expect.any(RegExp)],
      }),
    );

    window.history.pushState({}, "", "/wanted/w-123");
    expect(mocked.startSpan).toHaveBeenCalledWith(
      "navigation",
      expect.objectContaining({
        attributes: expect.objectContaining({
          "app.route.source": "pushState",
          "app.route.url": "/wanted/w-123",
        }),
      }),
    );
  });

  it("fails closed when browser tracing is disabled", async () => {
    const { initBrowserTracing, resetBrowserTracingForTests } = await import("./browser");
    resetBrowserTracingForTests();

    expect(
      initBrowserTracing({
        environment: "staging",
        browser_tracing_enabled: false,
        browser_trace_endpoint: "/api/telemetry/v1/traces",
        browser_trace_sample_ratio: 0.3,
      }),
    ).toBe(false);

    expect(mocked.tracerProvider).not.toHaveBeenCalled();
    expect(mocked.registerInstrumentations).not.toHaveBeenCalled();
  });
});
