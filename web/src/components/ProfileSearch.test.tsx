import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";

const mocked = vi.hoisted(() => ({
  profileSearch: vi.fn(),
  toastError: vi.fn(),
}));

vi.mock("../api/client", async () => {
  const actual = await vi.importActual<typeof import("../api/client")>("../api/client");
  return {
    ...actual,
    profileSearch: mocked.profileSearch,
  };
});

vi.mock("sonner", () => ({
  toast: {
    error: mocked.toastError,
  },
}));

import { ProfileSearch } from "./ProfileSearch";

describe("ProfileSearch", () => {
  afterEach(() => {
    vi.clearAllMocks();
  });

  it("searches after debounce and renders matching profiles", async () => {
    mocked.profileSearch.mockResolvedValue([{ handle: "alice", display_name: "Alice Chen" }]);

    render(
      <MemoryRouter>
        <ProfileSearch />
      </MemoryRouter>,
    );

    fireEvent.change(screen.getByPlaceholderText("Search by handle or name..."), {
      target: { value: "alice" },
    });

    await waitFor(() => {
      expect(mocked.profileSearch).toHaveBeenCalledWith("alice");
    });
    expect(await screen.findByText("@alice")).toBeInTheDocument();
    expect(screen.getByText("Alice Chen")).toBeInTheDocument();
  });

  it("shows an empty-state message when nothing matches", async () => {
    mocked.profileSearch.mockResolvedValue([]);

    render(
      <MemoryRouter>
        <ProfileSearch />
      </MemoryRouter>,
    );

    fireEvent.change(screen.getByPlaceholderText("Search by handle or name..."), {
      target: { value: "nobody" },
    });

    await waitFor(() => {
      expect(mocked.profileSearch).toHaveBeenCalledWith("nobody");
    });

    expect(await screen.findByText("No profiles found.")).toBeInTheDocument();
  });

  it("reports search failures", async () => {
    mocked.profileSearch.mockRejectedValue(new Error("search failed"));

    render(
      <MemoryRouter>
        <ProfileSearch />
      </MemoryRouter>,
    );

    fireEvent.change(screen.getByPlaceholderText("Search by handle or name..."), {
      target: { value: "alice" },
    });

    await waitFor(() => {
      expect(mocked.profileSearch).toHaveBeenCalledWith("alice");
    });

    await waitFor(() => expect(mocked.toastError).toHaveBeenCalledWith("search failed"));
  });
});
