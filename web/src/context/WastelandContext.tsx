import type { ReactNode } from "react";
import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";
import { bootstrap, setActiveUpstream } from "../api/client";
import type { WastelandConfig } from "../api/types";

interface WastelandContextValue {
  wastelands: WastelandConfig[];
  active: string | null;
  authenticated: boolean;
  connected: boolean;
  environment?: string;
  viewerRigHandle?: string;
  ready: boolean;
  switchTo: (upstream: string) => void;
  refresh: () => Promise<void>;
}

export const WastelandContext = createContext<WastelandContextValue>({
  wastelands: [],
  active: null,
  authenticated: false,
  connected: false,
  ready: false,
  switchTo: () => {},
  refresh: async () => {},
});

const STORAGE_KEY = "wl_active";

export function WastelandProvider({ children }: { children: ReactNode }) {
  const [wastelands, setWastelands] = useState<WastelandConfig[]>([]);
  const [active, setActive] = useState<string | null>(null);
  const [authenticated, setAuthenticated] = useState(false);
  const [connected, setConnected] = useState(false);
  const [environment, setEnvironment] = useState<string | undefined>();
  const [viewerRigHandle, setViewerRigHandle] = useState<string | undefined>();
  const [ready, setReady] = useState(false);

  const applyActive = useCallback((upstream: string | null) => {
    setActive(upstream);
    setActiveUpstream(upstream);
    if (upstream) {
      localStorage.setItem(STORAGE_KEY, upstream);
    } else {
      localStorage.removeItem(STORAGE_KEY);
    }
  }, []);

  const refresh = useCallback(async () => {
    setReady(false);
    try {
      const status = await bootstrap();
      const wls = status.wastelands ?? [];
      setAuthenticated(Boolean(status.authenticated));
      setConnected(Boolean(status.connected));
      setWastelands(wls);
      setEnvironment(status.environment);
      setViewerRigHandle(status.rig_handle);

      if (wls.length === 0) {
        applyActive(null);
        return;
      }

      if (status.active_upstream && wls.some((w) => w.upstream === status.active_upstream)) {
        applyActive(status.active_upstream);
        return;
      }

      // Pick active: prefer stored, fall back to first.
      const stored = localStorage.getItem(STORAGE_KEY);
      const match = stored && wls.some((w) => w.upstream === stored);
      applyActive(match ? stored : wls[0].upstream);
    } catch {
      // Not in hosted mode or server not running — no wastelands.
      setAuthenticated(false);
      setConnected(false);
      setWastelands([]);
      setEnvironment(undefined);
      setViewerRigHandle(undefined);
      applyActive(null);
    } finally {
      setReady(true);
    }
  }, [applyActive]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const switchTo = useCallback(
    (upstream: string) => {
      applyActive(upstream);
    },
    [applyActive],
  );

  const value = useMemo(
    () => ({
      wastelands,
      active,
      authenticated,
      connected,
      environment,
      viewerRigHandle,
      ready,
      switchTo,
      refresh,
    }),
    [wastelands, active, authenticated, connected, environment, viewerRigHandle, ready, switchTo, refresh],
  );

  return <WastelandContext.Provider value={value}>{children}</WastelandContext.Provider>;
}

export function useWasteland(): WastelandContextValue {
  return useContext(WastelandContext);
}
