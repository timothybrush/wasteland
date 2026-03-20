import { fireEvent, render, screen } from "@testing-library/react";
import { beforeAll, describe, expect, it, vi } from "vitest";
import { CommandsContext } from "../hooks/useCommands";
import { CommandPalette } from "./CommandPalette";

beforeAll(() => {
  class ResizeObserverMock {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
  vi.stubGlobal("ResizeObserver", ResizeObserverMock);
  HTMLElement.prototype.scrollIntoView = vi.fn();
});

describe("CommandPalette", () => {
  it("renders nothing when closed", () => {
    const { container } = render(
      <CommandsContext.Provider value={{ commands: [], register: () => () => {} }}>
        <CommandPalette open={false} onClose={() => {}} />
      </CommandsContext.Provider>,
    );

    expect(container).toBeEmptyDOMElement();
  });

  it("renders commands and executes selection", async () => {
    const action = vi.fn();
    const onClose = vi.fn();

    render(
      <CommandsContext.Provider
        value={{
          commands: [{ id: "nav-board", label: "Go to Board", group: "Navigation", shortcut: "g b", action }],
          register: () => () => {},
        }}
      >
        <CommandPalette open={true} onClose={onClose} />
      </CommandsContext.Provider>,
    );

    expect(screen.getByText("Navigation")).toBeInTheDocument();
    fireEvent.click(screen.getByText("Go to Board"));
    expect(action).toHaveBeenCalled();
    expect(onClose).toHaveBeenCalled();
  }, 10000);
});
