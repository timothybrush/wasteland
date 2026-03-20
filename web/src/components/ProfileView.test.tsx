import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";

const mocked = vi.hoisted(() => ({
  profile: vi.fn(),
  toastError: vi.fn(),
}));

vi.mock("../api/client", async () => {
  const actual = await vi.importActual<typeof import("../api/client")>("../api/client");
  return {
    ...actual,
    profile: mocked.profile,
  };
});

vi.mock("sonner", () => ({
  toast: {
    error: mocked.toastError,
  },
}));

import { ProfileView } from "./ProfileView";

function renderProfile(path = "/profile/alice") {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/profile/:handle" element={<ProfileView />} />
      </Routes>
    </MemoryRouter>,
  );
}

afterEach(() => {
  vi.clearAllMocks();
});

describe("ProfileView", () => {
  it("renders profile details and evidence tables", async () => {
    mocked.profile.mockResolvedValue({
      handle: "alice",
      display_name: "Alice Chen",
      bio: "Builds things",
      location: "Berlin",
      source: "github",
      confidence: 0.92,
      created_at: "2026-03-01T00:00:00Z",
      total_repos: 12,
      total_stars: 345,
      followers: 1200,
      account_age: 4.5,
      quality: 4.2,
      reliability: 4.1,
      creativity: 3.9,
      assessment_count: 2,
      languages: [{ name: "TypeScript", quality: 5, reliability: 4, creativity: 4, confidence: 0.8, message: "OSS" }],
      domains: [{ name: "Frontend", quality: 4, reliability: 4, creativity: 5, confidence: 0.8, message: "UI work" }],
      capabilities: [
        { name: "Testing", quality: 5, reliability: 5, creativity: 3, confidence: 0.9, message: "Deep tests" },
      ],
      notable_projects: [
        { name: "wasteland", stars: 345, impact_tier: "high", role: "maintainer", languages: ["TypeScript", "Go"] },
      ],
    });

    renderProfile();

    await waitFor(() => expect(screen.getByText("Alice Chen")).toBeInTheDocument());
    expect(screen.getByText("@alice")).toBeInTheDocument();
    expect(screen.getByText("Builds things")).toBeInTheDocument();
    expect(screen.getByText("1,200 followers")).toBeInTheDocument();
    expect(screen.getByText("92% confidence")).toBeInTheDocument();
    expect(screen.getByText("Languages (1)")).toBeInTheDocument();
    expect(screen.getByText("Domains (1)")).toBeInTheDocument();
    expect(screen.getByText("Capabilities (1)")).toBeInTheDocument();
    expect(screen.getByText("Notable Projects (1)")).toBeInTheDocument();
    expect(screen.getByText("wasteland")).toBeInTheDocument();
    expect(screen.getByText("TypeScript, Go")).toBeInTheDocument();
  });

  it("shows an error when profile loading fails", async () => {
    mocked.profile.mockRejectedValue(new Error("profile failed"));

    renderProfile();

    await waitFor(() => expect(screen.getByText("profile failed")).toBeInTheDocument());
    expect(mocked.toastError).toHaveBeenCalledWith("profile failed");
  });

  it("shows a fallback when no profile data is returned", async () => {
    mocked.profile.mockResolvedValue(null);

    renderProfile();

    await waitFor(() => expect(screen.getByText("No profile data.")).toBeInTheDocument());
  });
});
