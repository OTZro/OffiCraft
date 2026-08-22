// hooks/useOwnerName.ts — the owner's display nickname in the topbar (T-0b41).
//
// The nickname lives SERVER-SIDE now (DB owner.name, behind /api/settings) so
// every device shows the same name. This hook is the profile pill's seam:
// mount-fetch the stored value, PATCH on commit, and resolve to the localized
// default when unset. Mirrors useOrgName (T-d693); unlike org.name the nickname
// is NOT an agent read path (it never enters get_global_context).
//
// Owner-only surface: the whole cockpit is owner-authed, and /api/settings is
// owner-gated — a member/agent never reaches this write path. It replaces the
// old localStorage-only override (client cache dropped: the server is now the
// single source of truth, so a stale per-browser copy could only mislead).

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
} from "react";
import { api } from "../api";
import {
  adoptServerSettings,
  loadServerSettings,
} from "./sharedServerSettings";

interface UseOwnerName {
  /** The name to render in the profile pill: the stored nickname if the owner
   * has set one, else the caller-supplied localized default (`t.user`). */
  ownerName: string;
  /** Commit an edited nickname: PATCH /api/settings {owner_name}, adopting the
   * server's echoed value. Optimistic — reverts to the last confirmed value if
   * the write rejects. */
  setOwnerName: (next: string) => void;
}

export function useOwnerName(fallback: string): UseOwnerName {
  // null = not yet loaded / never set; "" is never stored (a set name is
  // non-empty). Either way an empty resolved value falls back to the default.
  const [stored, setStored] = useState<string | null>(null);

  useEffect(() => {
    let alive = true;
    loadServerSettings()
      .then((s) => {
        if (alive) setStored(s.ownerName);
      })
      .catch((e) => {
        // A failed load must never masquerade as "no name" — keep the localized
        // default showing rather than fabricating a value.
        console.warn("useOwnerName: load failed", e);
      });
    return () => {
      alive = false;
    };
  }, []);

  const setOwnerName = useCallback(
    (next: string) => {
      const trimmed = next.trim();
      const prev = stored;
      setStored(trimmed); // optimistic
      api
        .patchServerSettings({ ownerName: trimmed })
        .then((s) => {
          adoptServerSettings(s); // shared snapshot, see useOrgName (T-8115)
          setStored(s.ownerName);
        })
        .catch((e) => {
          console.warn("useOwnerName: save failed", e);
          setStored(prev); // snap back to the last server-confirmed value
        });
    },
    [stored]
  );

  const ownerName = stored && stored.length > 0 ? stored : fallback;
  return { ownerName, setOwnerName };
}

// ── the same name, everywhere it is spoken ──────────────────────────────────
//
// 🔴 THE NICKNAME IS NOT ONLY THE PROFILE PILL'S. Anything that renders the
// owner as a PARTICIPANT — the chat thread's `nameOf`, a document-history actor
// line — is naming the same person, and until T-4e95 those printed `t.user`
// instead: the THEME's default word for the human (「CEO（你）」, 「市長（你）」
// under 仙俠). The owner reported it from the running cockpit: his pill said
// 「韓立（你）」 while the thread called him 「市長（你）」.
//
// 🔴 IT IS A CONTEXT, NOT A SECOND useOwnerName CALL. <ChatArea> may not fetch
// while it paints — ChatArea.quote-no-fetch.test.tsx asserts the api client is
// touched ZERO times to render a thread — and mounting the hook there would put
// a settings read behind every chat repaint. App already resolved the value
// once; this hands that one answer down.
//
// The context default is `null`, meaning "nobody provided one" — and a consumer
// then renders the localized default it was going to render anyway. That is the
// same answer a load FAILURE produces (useOwnerName keeps `stored` null), which
// is the point: no path here can invent a name.
const OwnerNameContext = createContext<string | null>(null);

/** Publish the owner's resolved display name to the whole tree. `value` is
 * `useOwnerName(t.user).ownerName` — already fallen back to the localized
 * default when unset or unloaded. */
export function OwnerNameProvider({
  value,
  children,
}: {
  value: string;
  children: React.ReactNode;
}) {
  return (
    <OwnerNameContext.Provider value={value}>
      {children}
    </OwnerNameContext.Provider>
  );
}

/** The owner's display name for a participant label: the nickname he set, else
 * `fallback` (the caller's localized default). Never throws and never demands a
 * provider — outside one it simply answers `fallback`, which is precisely what
 * a not-yet-loaded and a failed read also answer. */
export function useOwnerDisplayName(fallback: string): string {
  const name = useContext(OwnerNameContext);
  return name !== null && name.length > 0 ? name : fallback;
}
