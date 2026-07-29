// hooks/useMonitoring.ts — load the monitoring telemetry through the api client
// + keep it fresh. Mirrors useMembers: reconcile-by-refetch on the relevant SSE
// topic (any topic containing "monitor"). In M1 the mock's subscribeEvents is a
// no-op, so the initial load is the only fetch — but the wiring is identical for
// the real backend.
//
// `enabled` (T-ec2c) gates BOTH the initial fetch and the SSE subscription. The
// monitoring fold is a large payload refetched on EVERY agent telemetry
// heartbeat (topic "monitoring" ⊂ "monitor"); the Monitor page needs that
// liveness, but the office page only reads it when a member detail panel is
// open (joinSessionRuntime for the selected member's live cost/context). A
// caller that isn't showing that panel passes enabled=false and this hook makes
// ZERO requests and holds no subscription — so merely being on the office page
// no longer streams monitoring. Default true keeps the Monitor page unchanged.

import { useCallback, useEffect, useRef, useState } from "react";
import type { MonitoringView } from "../types";
import { api } from "../api";

interface UseMonitoring {
  monitoring: MonitoringView | null;
  loading: boolean;
  /** True when the mount fetch REJECTED (non-401; 401 bounces to login at the
   * http layer). Lets the UI distinguish a failed load from honest-empty. */
  error: boolean;
  refetch: () => Promise<void>;
}

export function useMonitoring(opts?: { enabled?: boolean; refreshSeconds?: number }): UseMonitoring {
  const enabled = opts?.enabled ?? true;
  const refreshSeconds = opts?.refreshSeconds ?? 5;
  const [monitoring, setMonitoring] = useState<MonitoringView | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);
  const requestVersion = useRef(0);

  const refetch = useCallback(async () => {
    const next = await api.getMonitoring();
    setMonitoring(next);
  }, []);

  useEffect(() => {
    // Disabled: make NO request and hold NO subscription. Settle loading so a
    // gated caller never hangs on a perpetual spinner.
    if (!enabled) {
      setLoading(false);
      return;
    }

    let alive = true;

    let timer: ReturnType<typeof setTimeout> | null = null;
    let lastStarted = 0;
    let trailing = false;
    let inFlight = false;
    const schedule = () => {
      if (timer || !trailing) return;
      const delay = Math.max(0, refreshSeconds * 1000 - (Date.now() - lastStarted));
      timer = setTimeout(() => {
        timer = null;
        if (!alive || !trailing || inFlight) return;
        refresh().catch((e) => console.warn("useMonitoring: SSE refetch failed", e));
      }, delay);
    };
    const refresh = () => {
      if (inFlight) return Promise.resolve();
      timer = null;
      trailing = false;
      inFlight = true;
      lastStarted = Date.now();
      const version = ++requestVersion.current;
      return api.getMonitoring()
      .then((next) => {
        if (alive && version === requestVersion.current) {
          setMonitoring(next);
          setError(false);
        }
      })
      .catch((e) => {
        console.warn("useMonitoring: initial load failed", e);
        if (alive) setError(true);
      })
      .finally(() => {
        inFlight = false;
        if (alive) setLoading(false);
        schedule();
      });
    };

    refresh()
      .catch((e) => {
        console.warn("useMonitoring: initial load failed", e);
        if (alive) setError(true);
      })
      .finally(() => { if (alive) setLoading(false); });

    // SSE: refetch the telemetry on any monitoring-related topic.
    const unsubscribe = api.subscribeEvents((topic) => {
      if (topic.includes("monitor")) {
        requestVersion.current += 1;
        trailing = true;
        schedule();
      }
    });

    return () => {
      alive = false;
      if (timer) clearTimeout(timer);
      unsubscribe();
    };
  }, [enabled, refreshSeconds]);

  return { monitoring, loading, error, refetch };
}
