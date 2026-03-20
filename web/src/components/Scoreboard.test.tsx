import { screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { makeScoreboardEntry, makeScoreboardResponse, renderWithRouter } from "../test-utils";
import { Scoreboard } from "./Scoreboard";

const mocked = vi.hoisted(() => ({
  scoreboard: vi.fn(),
  toastError: vi.fn(),
}));

vi.mock("../api/client", () => ({
  scoreboard: mocked.scoreboard,
}));

vi.mock("sonner", () => ({
  toast: {
    error: mocked.toastError,
  },
}));

afterEach(() => {
  mocked.scoreboard.mockReset();
  mocked.toastError.mockReset();
});

describe("Scoreboard", () => {
  it("renders scoreboard entries", async () => {
    mocked.scoreboard.mockResolvedValue(
      makeScoreboardResponse({
        entries: [
          makeScoreboardEntry({ rig_handle: "alice", display_name: "Alice Chen" }),
          makeScoreboardEntry({ rig_handle: "bob", display_name: "Bob Lee" }),
        ],
      }),
    );

    renderWithRouter(<Scoreboard />);

    await waitFor(() => expect(screen.getAllByText("Alice Chen").length).toBeGreaterThan(0));
    expect(screen.getAllByText("Bob Lee").length).toBeGreaterThan(0);
  });

  it("shows empty state when no entries", async () => {
    mocked.scoreboard.mockResolvedValue(makeScoreboardResponse());

    renderWithRouter(<Scoreboard />);

    await waitFor(() => expect(screen.getByText("No scoreboard data yet")).toBeInTheDocument());
  });

  it("shows skeleton while loading", () => {
    mocked.scoreboard.mockReturnValue(new Promise(() => {}));

    renderWithRouter(<Scoreboard />);

    expect(screen.getByText("Scoreboard")).toBeInTheDocument();
  });

  it("shows error on fetch failure", async () => {
    mocked.scoreboard.mockRejectedValue(new Error("scoreboard error"));

    renderWithRouter(<Scoreboard />);

    await waitFor(() => expect(screen.getByText("scoreboard error")).toBeInTheDocument());
    expect(mocked.toastError).toHaveBeenCalledWith("scoreboard error");
  });

  it("top 3 podium cards render with correct ranks", async () => {
    mocked.scoreboard.mockResolvedValue(
      makeScoreboardResponse({
        entries: [
          makeScoreboardEntry({ rig_handle: "first", display_name: "First Place", weighted_score: 100 }),
          makeScoreboardEntry({ rig_handle: "second", display_name: "Second Place", weighted_score: 80 }),
          makeScoreboardEntry({ rig_handle: "third", display_name: "Third Place", weighted_score: 60 }),
        ],
      }),
    );

    renderWithRouter(<Scoreboard />);

    await waitFor(() => expect(screen.getByTestId("podium-1")).toBeInTheDocument());
    expect(screen.getByTestId("podium-2")).toBeInTheDocument();
    expect(screen.getByTestId("podium-3")).toBeInTheDocument();
    expect(screen.getByText("#1")).toBeInTheDocument();
    expect(screen.getByText("#2")).toBeInTheDocument();
    expect(screen.getByText("#3")).toBeInTheDocument();
  });

  it("rig handles link to profile pages", async () => {
    mocked.scoreboard.mockResolvedValue(
      makeScoreboardResponse({
        entries: [makeScoreboardEntry({ rig_handle: "alice", display_name: "Alice Chen" })],
      }),
    );

    renderWithRouter(<Scoreboard />);

    await waitFor(() => expect(screen.getAllByText("Alice Chen").length).toBeGreaterThan(0), { timeout: 5000 });
    const links = await screen.findAllByRole("link", { name: "Alice Chen" }, { timeout: 5000 });
    expect(links.length).toBeGreaterThan(0);
    expect(links[0]).toHaveAttribute("href", "/profile/alice");
  }, 10000);

  it("trust tier badges render with correct tier text", async () => {
    mocked.scoreboard.mockResolvedValue(
      makeScoreboardResponse({
        entries: [
          makeScoreboardEntry({ rig_handle: "a", trust_tier: "trusted" }),
          makeScoreboardEntry({ rig_handle: "b", trust_tier: "contributor" }),
        ],
      }),
    );

    renderWithRouter(<Scoreboard />);

    await waitFor(() => expect(screen.getAllByText("trusted").length).toBeGreaterThan(0));
    expect(screen.getAllByText("contributor").length).toBeGreaterThan(0);
  });
});
