import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

const mocked = vi.hoisted(() => ({
  props: undefined as Record<string, unknown> | undefined,
}));

vi.mock("sonner", () => ({
  Toaster: (props: Record<string, unknown>) => {
    mocked.props = props;
    return <div data-testid="sonner-toaster" />;
  },
}));

import { Toaster } from "./Toaster";

describe("Toaster", () => {
  it("configures the sonner toaster with app theme styles", () => {
    render(<Toaster />);

    expect(screen.getByTestId("sonner-toaster")).toBeInTheDocument();
    expect(mocked.props?.position).toBe("bottom-right");
    expect(mocked.props?.className).toBe("toaster");
    expect(mocked.props?.toastOptions).toMatchObject({
      style: {
        background: "var(--surface)",
        color: "var(--fg)",
        border: "1px solid var(--border)",
      },
    });
  });
});
