import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { getActiveUpstream } from "../api/client";
import { makeBootstrapResponse, mockFetch } from "../test-utils";
import { useWasteland, WastelandProvider } from "./WastelandContext";

function Consumer() {
  const { active, authenticated, connected, ready, viewerRigHandle, wastelands, switchTo } = useWasteland();
  return (
    <div>
      <div data-testid="active">{active ?? "none"}</div>
      <div data-testid="auth">{authenticated ? "yes" : "no"}</div>
      <div data-testid="connected">{connected ? "yes" : "no"}</div>
      <div data-testid="ready">{ready ? "yes" : "no"}</div>
      <div data-testid="viewer">{viewerRigHandle ?? "none"}</div>
      <div data-testid="count">{String(wastelands.length)}</div>
      <button type="button" onClick={() => switchTo("org/wl-two")}>
        switch
      </button>
    </div>
  );
}

let cleanupFetch: () => void;

afterEach(() => {
  cleanupFetch?.();
  localStorage.clear();
  sessionStorage.clear();
});

describe("WastelandContext", () => {
  it("restores the stored active wasteland when it still exists", async () => {
    localStorage.setItem("wl_active", "org/wl-two");
    cleanupFetch = mockFetch((url) => {
      if (url.includes("/api/bootstrap")) {
        return makeBootstrapResponse({
          wastelands: [
            { upstream: "org/wl-one", fork_org: "alice", fork_db: "wl-commons", mode: "pr", signing: false },
            { upstream: "org/wl-two", fork_org: "alice", fork_db: "wl-commons", mode: "pr", signing: false },
          ],
        });
      }
      return {};
    });

    render(
      <WastelandProvider>
        <Consumer />
      </WastelandProvider>,
    );

    await waitFor(() => expect(screen.getByTestId("active")).toHaveTextContent("org/wl-two"));
    expect(screen.getByTestId("auth")).toHaveTextContent("yes");
    expect(screen.getByTestId("connected")).toHaveTextContent("yes");
    expect(screen.getByTestId("ready")).toHaveTextContent("yes");
    expect(screen.getByTestId("viewer")).toHaveTextContent("alice");
    expect(getActiveUpstream()).toBe("org/wl-two");
  });

  it("falls back to the first wasteland when the stored one is stale", async () => {
    localStorage.setItem("wl_active", "org/missing");
    cleanupFetch = mockFetch((url) => {
      if (url.includes("/api/bootstrap")) {
        return makeBootstrapResponse({
          wastelands: [
            { upstream: "org/wl-one", fork_org: "alice", fork_db: "wl-commons", mode: "pr", signing: false },
            { upstream: "org/wl-two", fork_org: "alice", fork_db: "wl-commons", mode: "pr", signing: false },
          ],
        });
      }
      return {};
    });

    render(
      <WastelandProvider>
        <Consumer />
      </WastelandProvider>,
    );

    await waitFor(() => expect(screen.getByTestId("active")).toHaveTextContent("org/wl-one"));
    expect(localStorage.getItem("wl_active")).toBe("org/wl-one");
    expect(getActiveUpstream()).toBe("org/wl-one");
  });

  it("switchTo updates active upstream and persists it", async () => {
    cleanupFetch = mockFetch((url) => {
      if (url.includes("/api/bootstrap")) {
        return makeBootstrapResponse({
          wastelands: [
            { upstream: "org/wl-one", fork_org: "alice", fork_db: "wl-commons", mode: "pr", signing: false },
            { upstream: "org/wl-two", fork_org: "alice", fork_db: "wl-commons", mode: "pr", signing: false },
          ],
        });
      }
      return {};
    });

    render(
      <WastelandProvider>
        <Consumer />
      </WastelandProvider>,
    );

    await waitFor(() => expect(screen.getByTestId("active")).toHaveTextContent("org/wl-one"), { timeout: 5000 });
    fireEvent.click(screen.getByRole("button", { name: "switch" }));
    expect(screen.getByTestId("active")).toHaveTextContent("org/wl-two");
    expect(localStorage.getItem("wl_active")).toBe("org/wl-two");
    expect(getActiveUpstream()).toBe("org/wl-two");
  }, 10000);

  it("uses the bootstrap-selected active upstream when no local selection exists", async () => {
    cleanupFetch = mockFetch((url) => {
      if (url.includes("/api/bootstrap")) {
        return makeBootstrapResponse({
          active_upstream: "org/wl-one",
          wastelands: [
            { upstream: "org/wl-one", fork_org: "alice", fork_db: "wl-commons", mode: "pr", signing: false },
            { upstream: "org/wl-two", fork_org: "alice", fork_db: "wl-commons", mode: "pr", signing: false },
          ],
        });
      }
      return {};
    });

    render(
      <WastelandProvider>
        <Consumer />
      </WastelandProvider>,
    );

    await waitFor(() => expect(screen.getByTestId("active")).toHaveTextContent("org/wl-one"));
    expect(localStorage.getItem("wl_active")).toBe("org/wl-one");
  });

  it("preserves authenticated state even when no wasteland is joined", async () => {
    cleanupFetch = mockFetch((url) => {
      if (url.includes("/api/bootstrap")) {
        return makeBootstrapResponse({
          authenticated: true,
          connected: false,
          wastelands: [],
        });
      }
      return {};
    });

    render(
      <WastelandProvider>
        <Consumer />
      </WastelandProvider>,
    );

    await waitFor(() => expect(screen.getByTestId("ready")).toHaveTextContent("yes"));
    expect(screen.getByTestId("auth")).toHaveTextContent("yes");
    expect(screen.getByTestId("connected")).toHaveTextContent("no");
    expect(screen.getByTestId("count")).toHaveTextContent("0");
  });
});
