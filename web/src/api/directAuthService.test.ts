import { afterEach, describe, expect, it, vi } from "vitest";
import { buildConnectTokenMetadata, redeemConnectToken } from "./directAuthService";

let cleanup: (() => void) | undefined;

function mockFetch(handler: (url: string, init?: RequestInit) => Response | Promise<Response>) {
  const original = globalThis.fetch;
  globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === "string" ? input : input.toString();
    return handler(url, init);
  }) as typeof fetch;
  cleanup = () => {
    globalThis.fetch = original;
  };
}

afterEach(() => {
  cleanup?.();
  cleanup = undefined;
});

describe("buildConnectTokenMetadata", () => {
  it("builds canonical metadata with default mode and signing", () => {
    expect(
      buildConnectTokenMetadata({
        rig_handle: "alice",
        fork_org: "alice-org",
        fork_db: "wl-commons",
        upstream: "hop/wl-commons",
      }),
    ).toEqual({
      rig_handle: "alice",
      wastelands: [
        {
          upstream: "hop/wl-commons",
          fork_org: "alice-org",
          fork_db: "wl-commons",
          mode: "pr",
          signing: true,
        },
      ],
    });
  });
});

describe("redeemConnectToken", () => {
  it("posts the redemption payload to the auth service", async () => {
    mockFetch((_url, _init) =>
      Promise.resolve(
        new Response(JSON.stringify({ connection_id: "conn-1", status: "active" }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );

    await expect(
      redeemConnectToken({
        auth_service_base_url: "https://auth.example.test/",
        connect_token: "connect-token",
        redeem_secret: "redeem-secret",
        api_key: "secret-token",
        metadata: buildConnectTokenMetadata({
          rig_handle: "alice",
          fork_org: "alice-org",
          fork_db: "wl-commons",
          upstream: "hop/wl-commons",
        }),
      }),
    ).resolves.toEqual({
      connection_id: "conn-1",
      status: "active",
    });

    expect(vi.mocked(globalThis.fetch)).toHaveBeenCalledWith("https://auth.example.test/v1/connect-tokens/redeem", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        connect_token: "connect-token",
        redeem_secret: "redeem-secret",
        api_key: "secret-token",
        metadata: {
          rig_handle: "alice",
          wastelands: [
            {
              upstream: "hop/wl-commons",
              fork_org: "alice-org",
              fork_db: "wl-commons",
              mode: "pr",
              signing: true,
            },
          ],
        },
      }),
    });
  });

  it("surfaces auth-service user messages on failures", async () => {
    mockFetch(() =>
      Promise.resolve(
        new Response(
          JSON.stringify({
            error_code: "invalid_api_key",
            user_message: "That DoltHub API key was rejected. Check the key and try again.",
            retryable: false,
            request_id: "req_123",
          }),
          {
            status: 401,
            statusText: "Unauthorized",
            headers: { "Content-Type": "application/json" },
          },
        ),
      ),
    );

    await expect(
      redeemConnectToken({
        auth_service_base_url: "https://auth.example.test",
        connect_token: "connect-token",
        redeem_secret: "redeem-secret",
        api_key: "secret-token",
        metadata: buildConnectTokenMetadata({
          rig_handle: "alice",
          fork_org: "alice-org",
          fork_db: "wl-commons",
          upstream: "hop/wl-commons",
        }),
      }),
    ).rejects.toMatchObject({
      message: "That DoltHub API key was rejected. Check the key and try again.",
      status: 401,
      errorCode: "invalid_api_key",
      retryable: false,
      requestId: "req_123",
    });
  });
});
