import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { Outlet } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("@sentry/react", () => ({
  ErrorBoundary: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

vi.mock("./context/WastelandContext", () => ({
  WastelandProvider: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

vi.mock("./components/Layout", () => ({
  Layout: () => (
    <div data-testid="layout">
      <Outlet />
    </div>
  ),
}));

vi.mock("./components/BrowseList", () => ({ BrowseList: () => <div>Board Route</div> }));
vi.mock("./components/DetailView", () => ({ DetailView: () => <div>Detail Route</div> }));
vi.mock("./components/Dashboard", () => ({ Dashboard: () => <div>Dashboard Route</div> }));
vi.mock("./components/ProfileSearch", () => ({ ProfileSearch: () => <div>Profile Search Route</div> }));
vi.mock("./components/ProfileView", () => ({ ProfileView: () => <div>Profile View Route</div> }));
vi.mock("./components/Scoreboard", () => ({ Scoreboard: () => <div>Scoreboard Route</div> }));
vi.mock("./components/Settings", () => ({ Settings: () => <div>Settings Route</div> }));
vi.mock("./components/ConnectPage", () => ({ ConnectPage: () => <div>Connect Route</div> }));

import { App } from "./App";

afterEach(() => {
  window.history.pushState({}, "", "/");
});

describe("App", () => {
  it("renders the configured route inside the layout", () => {
    window.history.pushState({}, "", "/profile/alice");
    render(<App />);

    expect(screen.getByTestId("layout")).toBeInTheDocument();
    expect(screen.getByText("Profile View Route")).toBeInTheDocument();
  });

  it("renders the connect route", () => {
    window.history.pushState({}, "", "/connect");
    render(<App />);

    expect(screen.getByText("Connect Route")).toBeInTheDocument();
  });
});
