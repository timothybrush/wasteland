import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { setActiveUpstream } from "../api/client";
import { CommandsContext } from "../hooks/useCommands";
import { makeConfigResponse, makeDetailResponse, makeItem, mockFetch } from "../test-utils";
import { DetailView } from "./DetailView";

const defaultCommands = { commands: [], register: () => () => {} };

function renderDetail(id = "item-1") {
  return render(
    <MemoryRouter initialEntries={[`/wanted/${id}`]}>
      <CommandsContext.Provider value={defaultCommands}>
        <Routes>
          <Route path="/wanted/:id" element={<DetailView />} />
          <Route path="/" element={<div data-testid="home">Home</div>} />
        </Routes>
      </CommandsContext.Provider>
    </MemoryRouter>,
  );
}

let cleanupFetch: () => void;

afterEach(() => {
  setActiveUpstream(null);
  localStorage.removeItem("wl_active");
  cleanupFetch?.();
});

describe("DetailView", () => {
  it("shows skeleton while loading", () => {
    cleanupFetch = mockFetch(() => new Promise(() => {}));
    renderDetail();
    // While loading, the component renders skeletons (SkeletonLine etc)
    // No title text should be present yet
    expect(screen.queryByText("Fix the thing")).not.toBeInTheDocument();
  });

  it("renders item title and badges", async () => {
    cleanupFetch = mockFetch((url) => {
      if (url.includes("/api/config")) return makeConfigResponse();
      return makeDetailResponse({ item: makeItem({ title: "My Task", priority: 1, status: "open", type: "feature" }) });
    });
    renderDetail();
    await waitFor(() => expect(screen.getByText("My Task")).toBeInTheDocument());
    expect(screen.getByText("P1")).toBeInTheDocument();
    expect(screen.getByText("open")).toBeInTheDocument();
    expect(screen.getByText("feature")).toBeInTheDocument();
  });

  it("shows error on fetch failure", async () => {
    cleanupFetch = mockFetch((url) => {
      if (url.includes("/api/config")) return makeConfigResponse();
      return new Response(JSON.stringify({ error: "not found" }), { status: 404 });
    });
    renderDetail();
    await waitFor(() => expect(screen.getByText("not found")).toBeInTheDocument());
  });

  it("shows Edit button when canEdit is true", async () => {
    cleanupFetch = mockFetch((url) => {
      if (url.includes("/api/config")) return makeConfigResponse({ rig_handle: "alice" });
      return makeDetailResponse({ item: makeItem({ posted_by: "alice" }) });
    });
    renderDetail();
    await waitFor(() => expect(screen.getByText("Edit")).toBeInTheDocument());
  });

  it("hides Edit button when poster is different", async () => {
    cleanupFetch = mockFetch((url) => {
      if (url.includes("/api/config")) return makeConfigResponse({ rig_handle: "bob" });
      return makeDetailResponse({ item: makeItem({ posted_by: "alice" }) });
    });
    renderDetail();
    await waitFor(() => expect(screen.getByText("Fix the thing")).toBeInTheDocument());
    expect(screen.queryByText("Edit")).not.toBeInTheDocument();
  });

  it("shows submission actions for admins when backend allows them", async () => {
    cleanupFetch = mockFetch((url) => {
      if (url.includes("/api/config")) return makeConfigResponse({ rig_handle: "admin" });
      return makeDetailResponse({
        item: makeItem({ posted_by: "alice", status: "claimed" }),
        actions: ["accept", "reject", "close"],
        upstream_prs: [
          {
            rig_handle: "charlie",
            status: "in_review",
            evidence: "https://github.com/org/repo/pull/1",
            pr_url: "https://www.dolthub.com/repositories/org/db/pulls/1",
          },
        ],
      });
    });
    renderDetail();
    await waitFor(() => expect(screen.getByText("charlie")).toBeInTheDocument());
    expect(screen.getByRole("button", { name: "accept" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "reject" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "close" })).toBeInTheDocument();
  });

  it("shows submission actions for the poster when backend allows them", async () => {
    cleanupFetch = mockFetch((url) => {
      if (url.includes("/api/config")) return makeConfigResponse({ rig_handle: "alice" });
      return makeDetailResponse({
        item: makeItem({ posted_by: "alice", status: "claimed" }),
        actions: ["accept", "reject", "close"],
        upstream_prs: [
          {
            rig_handle: "charlie",
            status: "in_review",
            evidence: "https://github.com/org/repo/pull/1",
            pr_url: "https://www.dolthub.com/repositories/org/db/pulls/1",
          },
        ],
      });
    });
    renderDetail();
    await waitFor(() => expect(screen.getByText("charlie")).toBeInTheDocument());
    expect(screen.getByRole("button", { name: "accept" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "reject" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "close" })).toBeInTheDocument();
  });

  it("hides submission actions when backend does not allow them", async () => {
    cleanupFetch = mockFetch((url) => {
      if (url.includes("/api/config")) return makeConfigResponse({ rig_handle: "alice" });
      return makeDetailResponse({
        item: makeItem({ posted_by: "alice", status: "claimed" }),
        actions: [],
        upstream_prs: [
          {
            rig_handle: "charlie",
            status: "in_review",
            evidence: "https://github.com/org/repo/pull/1",
            pr_url: "https://www.dolthub.com/repositories/org/db/pulls/1",
          },
        ],
      });
    });
    renderDetail();
    await waitFor(() => expect(screen.getByText("charlie")).toBeInTheDocument());
    expect(screen.queryByRole("button", { name: "accept" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "reject" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "close" })).not.toBeInTheDocument();
  });

  it("action buttons trigger API calls", async () => {
    setActiveUpstream("hop/wl-commons");
    const fetchFn = vi.fn((url: string, init?: RequestInit) => {
      if (url.includes("/api/config")) return makeConfigResponse();
      if (url.endsWith("/claim") && init?.method === "POST") return { detail: makeItem({ status: "claimed" }) };
      return makeDetailResponse({ actions: ["claim"] });
    });
    cleanupFetch = mockFetch(fetchFn);
    renderDetail();
    await waitFor(() => expect(screen.getByText("claim")).toBeInTheDocument());
    await userEvent.click(screen.getByText("claim"));
    await waitFor(() => {
      const claimCalls = fetchFn.mock.calls.filter(([u]) => u.includes("/claim"));
      expect(claimCalls.length).toBeGreaterThan(0);
    });
  });

  it("destructive action shows confirm dialog", async () => {
    cleanupFetch = mockFetch((url) => {
      if (url.includes("/api/config")) return makeConfigResponse();
      return makeDetailResponse({ actions: ["close"] });
    });
    renderDetail();
    await waitFor(() => expect(screen.getByText("close")).toBeInTheDocument());
    await userEvent.click(screen.getByText("close"));
    expect(screen.getByText(/Are you sure/)).toBeInTheDocument();
  });

  it("delete navigates to /", async () => {
    setActiveUpstream("hop/wl-commons");
    cleanupFetch = mockFetch((url, init) => {
      if (url.includes("/api/config")) return makeConfigResponse();
      if (url.endsWith("/api/wanted/item-1") && init?.method === "DELETE") return { detail: null };
      return makeDetailResponse({ actions: ["delete"] });
    });
    renderDetail();
    await waitFor(() => expect(screen.getByText("delete")).toBeInTheDocument());
    await userEvent.click(screen.getByText("delete"));
    // Confirm dialog appears
    await waitFor(() => expect(screen.getByText("Confirm")).toBeInTheDocument());
    await userEvent.click(screen.getByText("Confirm"));
    await waitFor(() => expect(screen.getByTestId("home")).toBeInTheDocument());
  });

  it("done form submits evidence", async () => {
    setActiveUpstream("hop/wl-commons");
    const fetchFn = vi.fn((url: string, init?: RequestInit) => {
      if (url.includes("/api/config")) return makeConfigResponse();
      if (url.endsWith("/done") && init?.method === "POST") return { detail: makeItem({ status: "claimed" }) };
      return makeDetailResponse({ actions: ["done"] });
    });
    cleanupFetch = mockFetch(fetchFn);
    renderDetail();
    await waitFor(() => expect(screen.getByText("done")).toBeInTheDocument());
    await userEvent.click(screen.getByText("done"));
    const input = screen.getByPlaceholderText("https://github.com/...");
    await userEvent.type(input, "https://example.com/pr/1");
    await userEvent.click(screen.getByText("Submit"));
    await waitFor(() => {
      const doneCalls = fetchFn.mock.calls.filter(([u]) => u.includes("/done"));
      expect(doneCalls.length).toBeGreaterThan(0);
    });
  });

  it("view diff button loads diff content", async () => {
    cleanupFetch = mockFetch((url) => {
      if (url.includes("/api/config")) return makeConfigResponse();
      if (url.includes("/diff/")) return { diff: "+added line" };
      return makeDetailResponse({
        branch: "wl/fix",
        delta: "1 table changed",
      });
    });
    renderDetail();
    await waitFor(() => expect(screen.getByText("View diff")).toBeInTheDocument());
    await userEvent.click(screen.getByText("View diff"));
    await waitFor(() => expect(screen.getByText("+added line")).toBeInTheDocument());
  });
});
