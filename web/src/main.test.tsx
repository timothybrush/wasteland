import { beforeEach, describe, expect, it, vi } from "vitest";

const mocked = vi.hoisted(() => ({
  init: vi.fn(),
  getDefaultIntegrations: vi.fn(() => ["default-integration"]),
  globalHandlersIntegration: vi.fn(() => "global-handlers"),
  createRoot: vi.fn(),
  render: vi.fn(),
}));

vi.mock("./api/prefetch", () => ({}));

vi.mock("./App", () => ({
  App: () => <div>App Root</div>,
}));

vi.mock("./components/Toaster", () => ({
  Toaster: () => <div>Toast Root</div>,
}));

vi.mock("@sentry/browser", () => ({
  getDefaultIntegrations: mocked.getDefaultIntegrations,
  globalHandlersIntegration: mocked.globalHandlersIntegration,
}));

vi.mock("@sentry/react", () => ({
  init: mocked.init,
  browserTracingIntegration: () => "browser-tracing",
  replayIntegration: () => "replay",
}));

vi.mock("react-dom/client", () => ({
  createRoot: mocked.createRoot,
}));

describe("main", () => {
  beforeEach(() => {
    vi.resetModules();
    document.body.innerHTML = '<div id="root"></div>';
    mocked.init.mockReset();
    mocked.getDefaultIntegrations.mockClear();
    mocked.globalHandlersIntegration.mockClear();
    mocked.createRoot.mockReset();
    mocked.render.mockReset();
    mocked.createRoot.mockReturnValue({ render: mocked.render });
  });

  it("initializes Sentry and mounts the app", async () => {
    await import("./main");

    expect(mocked.init).toHaveBeenCalledWith(
      expect.objectContaining({
        integrations: expect.arrayContaining(["default-integration", "global-handlers", "browser-tracing", "replay"]),
        tracesSampleRate: 0.2,
        replaysSessionSampleRate: 0,
        replaysOnErrorSampleRate: 1.0,
      }),
    );
    expect(mocked.createRoot).toHaveBeenCalledWith(document.getElementById("root"));
    expect(mocked.render).toHaveBeenCalled();
  }, 30000);
});
