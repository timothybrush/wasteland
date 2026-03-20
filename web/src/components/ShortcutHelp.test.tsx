import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { ShortcutHelp } from "./ShortcutHelp";

describe("ShortcutHelp", () => {
  it("renders nothing when closed", () => {
    const { container } = render(<ShortcutHelp open={false} onClose={() => {}} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("renders shortcut sections and closes", async () => {
    const onClose = vi.fn();
    render(<ShortcutHelp open={true} onClose={onClose} />);

    expect(screen.getByText("Keyboard Shortcuts")).toBeInTheDocument();
    expect(screen.getByText("Navigation")).toBeInTheDocument();
    expect(screen.getByText("Command Palette")).toBeInTheDocument();
    expect(screen.getByText("Move Down")).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(onClose).toHaveBeenCalled();
  });
});
