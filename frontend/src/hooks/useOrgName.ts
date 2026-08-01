// hooks/useOrgName.ts — the studio display name in the topbar (T-d693).
//
// The name lives SERVER-SIDE now (DB org.name, behind /api/settings) so every
// device sees the same studio name and each agent can read it back through
// get_global_context. This hook is the topbar's seam: mount-fetch the stored
// value, PATCH on commit, and resolve to the localized default when unset.
//
// Owner-only surface: the whole cockpit is owner-authed, and /api/settings is
// governance-gated (owner / admin agent, T-6020) — a plain agent never reaches
// this write path. It replaces the
// old localStorage-only override (client cache dropped: the server is now the
// single source of truth, so a stale per-browser copy could only mislead).

import { useCallback, useEffect, useState } from "react";
import { api } from "../api";
import {
  adoptServerSettings,
  loadServerSettings,
} from "./sharedServerSettings";

interface UseOrgName {
  /** The name to render in the topbar: the stored studio name if the owner has
   * set one, else the caller-supplied localized default (`t.orgName`). */
  orgName: string;
  /** Commit an edited name: PATCH /api/settings {org_name}, adopting the
   * server's echoed value. Optimistic — reverts to the last confirmed value if
   * the write rejects. (The topbar's InlineEdit treats empty as cancel, so a
   * blank never reaches here; "" clearing stays a settings-API capability.) */
  setOrgName: (next: string) => void;
}

export function useOrgName(fallback: string): UseOrgName {
  // null = not yet loaded / never set; "" is never stored (a set name is
  // non-empty). Either way an empty resolved value falls back to the default.
  const [stored, setStored] = useState<string | null>(null);

  useEffect(() => {
    let alive = true;
    loadServerSettings()
      .then((s) => {
        if (alive) setStored(s.orgName);
      })
      .catch((e) => {
        // A failed load must never masquerade as "no name" — keep the localized
        // default showing rather than fabricating a value.
        console.warn("useOrgName: load failed", e);
      });
    return () => {
      alive = false;
    };
  }, []);

  const setOrgName = useCallback(
    (next: string) => {
      const trimmed = next.trim();
      const prev = stored;
      setStored(trimmed); // optimistic
      api
        .patchServerSettings({ orgName: trimmed })
        .then((s) => {
          // The echo is the newest truth this tab has — hand it to the shared
          // snapshot so the other settings consumers do not keep serving the
          // pre-save copy (T-8115).
          adoptServerSettings(s);
          setStored(s.orgName);
        })
        .catch((e) => {
          console.warn("useOrgName: save failed", e);
          setStored(prev); // snap back to the last server-confirmed value
        });
    },
    [stored]
  );

  const orgName = stored && stored.length > 0 ? stored : fallback;
  return { orgName, setOrgName };
}
