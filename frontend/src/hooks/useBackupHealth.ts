// hooks/useBackupHealth.ts — is the SCHEDULED database backup still producing
// retreat points? (T-da06)
//
// Read-only, mount-fetch + explicit refresh — the SAME seam as useVersion, and
// deliberately NOT a poller: the indicator that consumes this is mounted
// app-wide, and a backup schedule is measured in hours, so a per-second (or
// per-SSE-event) re-fetch would be a storm for an answer that cannot have
// changed. The topbar refresh button is a full page reload, which re-mounts
// this hook; the monitor card calls `refresh()`.
//
// 🔴 A FAILED fetch is NOT healthy and NOT unhealthy — `health` stays null and
// `error` goes true, and every consumer must render that as the muted
// "cannot tell" look. That is the whole point of the ticket: a missing retreat
// point must never look like a present one, and neither must an unanswerable
// question.

import { useCallback, useEffect, useState } from "react";
import type { BackupHealthView } from "../types";
import { api } from "../api";

interface UseBackupHealth {
  /** The server's verdict; null while loading OR after a failed load. */
  health: BackupHealthView | null;
  loading: boolean;
  /** True when the fetch REJECTED (non-401; 401 bounces to login at the http
   * layer). Guards against a failed load reading as "the backup is fine". */
  error: boolean;
  /** Re-fetch /api/backup-health. */
  refresh: () => void;
}

export function useBackupHealth(): UseBackupHealth {
  const [health, setHealth] = useState<BackupHealthView | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);
  // Bumping the epoch re-runs the fetch effect; the effect's `alive` guard
  // keeps a stale response from landing after unmount / a newer run.
  const [epoch, setEpoch] = useState(0);

  useEffect(() => {
    let alive = true;
    api
      .getBackupHealth()
      .then((next) => {
        if (alive) {
          setHealth(next);
          setError(false);
        }
      })
      .catch((e) => {
        console.warn("useBackupHealth: load failed", e);
        // Drop the stale verdict too: a backup health answer we could not
        // refresh must not keep claiming green from a previous load.
        if (alive) {
          setHealth(null);
          setError(true);
        }
      })
      .finally(() => {
        if (alive) setLoading(false);
      });
    return () => {
      alive = false;
    };
  }, [epoch]);

  const refresh = useCallback(() => setEpoch((n) => n + 1), []);

  return { health, loading, error, refresh };
}
