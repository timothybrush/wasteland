import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ActionButton } from "./ActionButton";

describe("ActionButton", () => {
  it("shows action label", () => {
    render(<ActionButton action="claim" onAction={async () => {}} />);
    expect(screen.getByText("claim")).toBeInTheDocument();
  });

  it("shows loading state during async action", async () => {
    let resolveAction: () => void;
    const onAction = () =>
      new Promise<void>((r) => {
        resolveAction = r;
      });
    render(<ActionButton action="claim" onAction={onAction} />);
    fireEvent.click(screen.getByText("claim"));
    expect(screen.getByText("claim...")).toBeInTheDocument();
    expect(screen.getByRole("button")).toBeDisabled();
    await act(async () => {
      resolveAction!();
    });
    await waitFor(() => expect(screen.getByText("claim")).toBeInTheDocument());
  }, 10000);

  it("is disabled during async", async () => {
    let resolveAction: () => void;
    const onAction = () =>
      new Promise<void>((r) => {
        resolveAction = r;
      });
    render(<ActionButton action="do" onAction={onAction} />);
    fireEvent.click(screen.getByText("do"));
    expect(screen.getByRole("button")).toBeDisabled();
    await act(async () => {
      resolveAction!();
    });
    await waitFor(() => expect(screen.getByRole("button")).not.toBeDisabled());
  }, 10000);

  it("sets data-action attribute", () => {
    render(<ActionButton action="claim" onAction={async () => {}} />);
    expect(screen.getByRole("button")).toHaveAttribute("data-action", "claim");
  });
});
