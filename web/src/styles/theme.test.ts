import { describe, expect, it } from "vitest";
import { ayu, priorityLabel, statusColor } from "./theme";

describe("theme helpers", () => {
  it("returns 'all' for negative priority filters", () => {
    expect(priorityLabel(-1)).toBe("all");
  });

  it("formats priority labels", () => {
    expect(priorityLabel(0)).toBe("P0");
    expect(priorityLabel(3)).toBe("P3");
  });

  it("maps statuses to the expected palette colors", () => {
    expect(statusColor.open).toBe(ayu.green);
    expect(statusColor.claimed).toBe(ayu.steel);
    expect(statusColor.in_review).toBe(ayu.brass);
    expect(statusColor.completed).toBe(ayu.accent);
    expect(statusColor.validated).toBe(ayu.accent);
    expect(statusColor.withdrawn).toBe(ayu.dim);
  });
});
