// hooks/useMembers.ts — load the roster through the api client + keep it fresh.
//
// Reconcile-by-refetch (contract B): on a "member" SSE topic (the server's
// roster/presence delta — see service/repository.py _publish("member")) we
// REFETCH the roster rather than merging any event payload. In M1 the mock's
// subscribeEvents is a no-op, so refetch is driven by explicit action callbacks
// (activate/patch/refocus) — but the wiring is identical for the real backend.
//
// T-8115: a "chat" / "chat_read" delta that moves EXACTLY ONE card's unread badge
// re-reads that one member (`GET /api/members/{id}`) instead of the company, and
// re-reads nothing at all when it names a peer that is not on the roster — a chat
// line can neither add, remove nor rename a member. Naming TWO OR MORE held
// members re-pulls the list, because per-item costs one full ListChat() scan PER
// REQUEST server-side while the list costs exactly one however many were named —
// the crossover is derived, see the note at the call site. Still no payload
// merging: the delta only says WHICH card changed, the server says what it holds.
//
// 🔴 That per-item read is only sound because the single-member endpoint was FIXED
// to compute `unread_count` the same way the list does (review of the first
// T-8115 cut: it used to hand back a literal 0, so this very path drove the badge
// to 0 exactly when a new message should have raised it). If you are about to add
// a per-item refetch anywhere else, check `api/dtoParity.ts` FIRST — `useTasks`
// still cannot have one.

import { useCallback, useEffect, useRef, useState } from "react";
import type { Member } from "../types";
import { api } from "../api";
import { createDeltaSink, narrowToHeld } from "../lib/deltaSink";
import type { SseDelta } from "../api/adapter";

interface UseMembers {
  members: Member[];
  loading: boolean;
  /** True when the mount fetch REJECTED — so the UI can tell an honest empty
   * roster apart from a failed load (never render a failure as "members · 0").
   * A 401 is handled globally (api/http.ts → login bounce); this guards the
   * non-401 case (500 / network). */
  error: boolean;
  refetch: () => Promise<void>;
}

// Topics that mutate the roster/presence view → trigger a refetch. The server
// fans a single "member" topic for roster + presence deltas (NOT "members" /
// "presence" — those never arrive; matching them left the UI stale on wake).
// "chat" / "chat_read" also mutate the roster view since MemberDTO.unread_count
// (the M2-1 count badge) derives from the chat stream + read watermark: a new
// inbound message bumps a card's badge, an advancing watermark clears it.
// "role_def" rides along so a CUSTOM role rename (role_def delta) re-resolves
// every member card's role display name (single truth: the role doc's name).
const ROSTER_TOPICS = new Set(["member", "chat", "chat_read", "role_def"]);

// The LIGHT topic set (T-cf91): identity only (name + role), so chat / chat_read
// are DELIBERATELY excluded — a light roster carries no unread badge (the server
// returns unread_count honest-empty), and a chat line changes no name or role.
// A light consumer (請示卡頁) therefore never re-pulls the roster when anyone in
// the company speaks; only a genuine roster or role change refetches.
const ROSTER_TOPICS_LIGHT = new Set(["member", "role_def"]);

// The topics whose ONLY effect on this view is one card's unread badge
// (MemberDTO.unread_count). A chat line and an advancing read watermark cannot
// add, remove, rename or re-order a member — the roster is served ordered by
// name (dal.go) — so when such a delta NAMES a member we already hold, the
// honest refetch is that member, not the company. Any other topic ("member",
// "role_def") can change list membership or every row at once, and stays a full
// re-pull.
const BADGE_ONLY_TOPICS = new Set(["chat", "chat_read"]);

// The owner's fixed wire id — the `sub` of the owner token. Kept local, the same
// as ChatArea's / ChatGalleryPanel's / actorLabel's OWNER_ID: the value is the
// wire's, not a new name to invent.
const OWNER_ID = "owner";

/**
 * Could this delta move ANY unread badge on THIS roster?
 *
 * The only roster field a badge-only topic can move is `unread_count`, and that
 * count is the inverse of the READER's watermark: `UnreadCounts`
 * (server/ocserverd/domain.go:411-425) counts a message only when
 * `m.Recipient == reader` — its own doc comment says "Messages between two other
 * participants never count". This roster's reader is the owner. So a chat line
 * or a read receipt with NO owner at either end cannot change a single number
 * here, and refetching for it — by EITHER path — buys nothing at all.
 *
 * The identity we need is already on the delta (`toSseDelta` keeps
 * from/to/reader/peer), so this costs no request to decide.
 *
 * ⚠️ The two topics carry DIFFERENT field names — `chat` has from/to, and
 * `chat_read` has reader/peer. Checking only one pair silently answers "no owner
 * here" for every delta of the other topic, which would skip real work.
 *
 * ⚠️ This is deliberately the LOOSE test ("is the owner at either end"), not the
 * tightest one. The tightest would be `to === owner` for chat (a message the
 * owner SENT moves nobody's badge either) and `reader === owner` for chat_read.
 * Both remaining cases are still-wasted k=1 per-item GETs, and narrowing them is
 * a SECOND optimisation that deserves its own measurement — the loose test errs
 * toward fetching, which is the safe direction.
 */
function couldMoveAnOwnerBadge(d: SseDelta): boolean {
  const n = d.names;
  return (
    n.from === OWNER_ID ||
    n.to === OWNER_ID ||
    n.reader === OWNER_ID ||
    n.peer === OWNER_ID
  );
}

export function useMembers(opts?: { light?: boolean }): UseMembers {
  const light = opts?.light ?? false;
  const topics = light ? ROSTER_TOPICS_LIGHT : ROSTER_TOPICS;
  const [members, setMembers] = useState<Member[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);
  // Which ids the roster currently holds, readable from an SSE callback (a
  // state read there would be a stale closure). Only membership is mirrored —
  // the VALUES always come from the server.
  const heldRef = useRef<Set<string>>(new Set());

  // Adopt a whole roster: state and the id mirror move together, so the mirror
  // can never disagree with what is rendered.
  const adopt = useCallback((next: Member[]) => {
    heldRef.current = new Set(next.map((m) => m.id));
    setMembers(next);
  }, []);

  const refetch = useCallback(async () => {
    const next = await api.listMembers(light ? { light: true } : undefined);
    adopt(next);
  }, [light, adopt]);

  useEffect(() => {
    let alive = true;

    const full = () =>
      api
        .listMembers(light ? { light: true } : undefined)
        .then((next) => {
          if (alive) {
            adopt(next);
            setError(false);
          }
        })
        .catch((e) => console.warn("useMembers: SSE refetch failed", e));

    // Replace the named card, IN PLACE. The server stays the source of every
    // value (one GET /api/members/{id}, unread_count included — see the header);
    // the position is kept because the roster's order is by name and these topics
    // cannot change a name. A rejection falls back to the full re-pull rather
    // than leaving one card stale behind a badge that already moved.
    // NOTE: takes an array but the caller only ever passes ONE id — see the
    // crossover note at the call site. The array shape is kept so the fan-out
    // stays visible if that rule is ever revisited with numbers behind it.
    const patchOne = (ids: string[]) =>
      Promise.all(ids.map((id) => api.getMember(id)))
        .then((fresh) => {
          if (!alive) return;
          setMembers((prev) =>
            prev.map((m) => fresh.find((f) => f.id === m.id) ?? m)
          );
          setError(false);
        })
        .catch((e) => {
          console.warn("useMembers: member refetch failed", e);
          return full();
        });

    // Initial load. On rejection surface an honest error flag instead of
    // swallowing it into an empty roster. (Do NOT clearToken here — a 401 is
    // already handled at the http layer, which bounces to login.)
    api
      .listMembers(light ? { light: true } : undefined)
      .then((next) => {
        if (alive) {
          adopt(next);
          setError(false);
        }
      })
      .catch((e) => {
        console.warn("useMembers: initial load failed", e);
        if (alive) setError(true);
      })
      .finally(() => {
        if (alive) setLoading(false);
      });

    // SSE: reconcile the roster on the relevant topics — ONE decision per burst
    // of deltas (a resync fans 12 topics at once, of which this hook listens to
    // four). The light set omits chat/chat_read (T-cf91) so a chat line never
    // re-pulls here at all.
    const unsubscribe = api.subscribeEvents(
      createDeltaSink((batch) => {
        const mine = [...batch.topics].filter((t) => topics.has(t));
        if (mine.length === 0) return;
        const badgeOnly = mine.every((t) => BADGE_ONLY_TOPICS.has(t));
        const touched = badgeOnly
          ? narrowToHeld(batch, (id) => heldRef.current.has(id))
          : null;
        if (touched === null) {
          void full();
          return;
        }
        // 🔴 NOTHING in this burst has the owner at either end ⇒ it cannot move
        // a single badge on this roster ⇒ do not fetch AT ALL (not the list, not
        // the item). See `couldMoveAnOwnerBadge`. This is the agent↔agent case,
        // which is ordinary traffic here: before this line it cost one full
        // `GET /api/members` per message, for a screen that never changed.
        //
        // Whole-burst, not per-delta: a mixed burst (one agent↔agent line AND
        // one line to the owner) still goes through the branches below with ALL
        // its ids, so a real change is never dropped. That can widen k and send
        // a mixed burst down the list path instead of the per-item one — always
        // CORRECT, occasionally not the cheapest, and the safe direction.
        //
        // The `length > 0` guard is fail-safe, not decoration: an empty `deltas`
        // would make `.some()` false and skip. Today that is unreachable (a
        // batch with no delta object is `unnamed`, which returns above), but the
        // failure mode would be a silent stale roster, so it is spelled out.
        if (
          batch.deltas.length > 0 &&
          !batch.deltas.some(couldMoveAnOwnerBadge)
        ) {
          return;
        }
        // Named somebody, none of them ours: a chat line CANNOT add, remove or
        // rename a member (that is the "member" topic), so a conversation with
        // an outsource worker / a released peer changes nothing this roster
        // renders. Re-pulling the company for it was the old behaviour.
        //
        // 🔴 EXACTLY ONE named card takes the per-item path; TWO OR MORE re-pull
        // the list. The crossover is at 2 and is DERIVED, not a tunable knob
        // (measured 2026-08-01, server-side statement counts):
        //   k=1  — per-item 1 GET + 1 ListChat() full scan; list 1 + 1. A TIE on
        //          cost, and the per-item payload is smaller ⇒ per-item wins.
        //   k>=2 — per-item k GETs + k full scans (`unreadCountsForRequest` runs
        //          ListChat() once PER REQUEST); the list is ALWAYS 1 + 1 ⇒ the
        //          list wins, and the gap widens linearly with k.
        // So no k is worse than the pre-T-8115 behaviour: k=1 gains the smaller
        // payload, k>=2 costs exactly what it always did. Raising this threshold
        // re-introduces a k-times amplification of a full-table scan on a ticket
        // whose whole point was to stop the cockpit re-reading too much.
        //
        // k>=2 is NOT a corner case: one chat delta names {id, from, to}
        // (api_chat.go), and the hub delivers EVERY delta to the owner/dashboard
        // connection — so a single agent-to-agent message names TWO held members
        // in ONE frame, with no burst coalescing involved at all.
        //
        // 🔴 **但 k>=2 是常態的「浪費」,不是常態的「需求」——這一段以前只寫了前半,
        // 讀起來像「這條路已經解完了」。** `UnreadCounts`
        // (server/ocserverd/domain.go:411-425)只數 `m.Recipient == reader`,
        // 而這份 roster 的 reader 是 owner,且 owner 不是名冊列 ⇒ heldRef 不含它。
        //   會動 badge ⟹ 有一端是 owner ⟹ k <= 1
        //   k >= 2     ⟹ 兩端都不是 owner ⟹ 動不了任何 badge(語意 no-op)
        // 反過來不成立:k=1 不蘊含「有事做」(`owner → member`、
        // `member ↔ ow-worker` 都是 k=1 且什麼都不該做,今天照樣各打一次 GET)。
        // ⇒ 現況是「2 次沒必要的逐項」換成「1 次沒必要的清單」:每個 k 都嚴格優於
        // 改動前,但**最佳解是 0 個請求**——資訊已在 delta 上,`chat` 判
        // `from !== "owner" && to !== "owner"`、`chat_read` 判 `reader !== "owner"`
        // 就能直接 return(兩個 topic 述詞不同,chat_read 沒有 from/to)。
        // **還沒做,要先取得 owner 核可**(新增一道從不存在的保護,不是順手改)。
        // 🔴 `k > 1 → full()` IS NOW A FAIL-SAFE, NOT THE HOT PATH — do not
        // delete it as dead code. With the skip above, every burst that still
        // reaches here has the owner at one end, and on TODAY'S wire that forces
        // k ≤ 1 (a chat delta names {id, from, to}; if one end is the owner, at
        // most one other name can be a roster member). So k > 1 is currently
        // unreachable in production.
        //
        // But that is a property of the CURRENT wire, not a construction. Add a
        // sixth identity field to `toSseDelta`, let a delta name a third party,
        // or change who the hub delivers to, and k > 1 comes back — at which
        // point this branch is the difference between one list GET and k
        // per-item GETs, each of which costs a full `ListChat()` scan on the
        // server (`unreadCountsForRequest`). Keeping it costs one comparison.
        if (touched.length === 1) void patchOne(touched);
        else if (touched.length > 1) void full();
      })
    );

    return () => {
      alive = false;
      unsubscribe();
    };
  }, [light, topics, adopt]);

  return { members, loading, error, refetch };
}
