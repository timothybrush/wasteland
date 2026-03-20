import { describe, expect, it, vi } from "vitest";
import { connectDoltHub, initNango } from "./nango";

const mocked = vi.hoisted(() => ({
  constructorArgs: vi.fn(),
  auth: vi.fn(),
}));

vi.mock("@nangohq/frontend", () => ({
  default: class MockNango {
    auth = mocked.auth;

    constructor(args: unknown) {
      mocked.constructorArgs(args);
    }
  },
}));

describe("nango helpers", () => {
  it("initializes Nango with the connect session token", () => {
    const nango = initNango("session-token");

    expect(mocked.constructorArgs).toHaveBeenCalledWith({ connectSessionToken: "session-token" });
    expect(nango).toBeTruthy();
  });

  it("passes API key credentials to the DoltHub auth flow", async () => {
    mocked.auth.mockResolvedValueOnce({ connectionId: "conn-1" });
    const nango = { auth: mocked.auth } as never;

    await expect(connectDoltHub(nango, "dolthub", "secret-token")).resolves.toEqual({ connectionId: "conn-1" });
    expect(mocked.auth).toHaveBeenCalledWith("dolthub", {
      credentials: { apiKey: "secret-token" },
    });
  });
});
