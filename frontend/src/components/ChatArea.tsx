import {
  Fragment,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { useI18n } from "../i18n";
import type { Member, MemberActivateResult } from "../types";
import type {
  ChatMessage,
  OutsourceWorkerView,
} from "../api/adapter";
import { autosizeTextarea } from "../lib/autosize";
import { getChatDraft, saveChatDraft } from "../lib/chatDraftStore";
import { useChat } from "../hooks/useChat";
import { useWorkerCodenames } from "../hooks/useWorkerCodenames";
import { useOwnerDisplayName } from "../hooks/useOwnerName";
import { formatDayLabel, splitByDay } from "../lib/dateFormat";
import {
  ATTACH_ACCEPT,
  CHAT_MAX_ATTACHMENTS,
  useAttachmentStaging,
} from "../hooks/useAttachmentStaging";
import type { PendingAttachment } from "../hooks/useAttachmentStaging";
import { useWindowActive } from "../hooks/useWindowActive";
import { useIsMobile } from "../hooks/useIsMobile";
import { useKeyedRecord } from "../hooks/useKeyedRecord";
import { useKeyedState } from "../hooks/useKeyedState";
import { enterShouldSend } from "../lib/composerKeys";
import { chatBottomAffordance } from "../lib/chatBottomAffordance";
import { scrollToLatest } from "../lib/scrollToLatest";
import { AttachmentStrip } from "./AttachmentStrip";
import { Avatar } from "./Avatar";
import { avatarKindForMember } from "../lib/avatarKind";
import { ChatGalleryPanel } from "./ChatGalleryPanel";
import { ChatJumpLatestButton } from "./ChatJumpLatestButton";
import { ChatNewMsgPreview } from "./ChatNewMsgPreview";
import { ChatReplyCard } from "./ChatReplyCard";
import { ComposerAttachmentPreview } from "./ComposerAttachmentPreview";
import { Markdown } from "./Markdown";
import { MarkdownPreviewOverlay } from "./MarkdownPreviewOverlay";
import { useQuotedMessageOverlay } from "../hooks/useQuotedMessageOverlay";
import { PresenceBadge } from "./PresenceBadge";
import { CurrentTaskTitle } from "./CurrentTaskTitle";
import {
  BoltIcon,
  ChevronRightIcon,
  CloseIcon,
  ExpandIcon,
  ImageIcon,
  MoonIcon,
  PaperclipIcon,
  ReplyIcon,
  SendIcon,
  TasksIcon,
  UserGearIcon,
} from "./icons";
import { DispatchAlert } from "./DispatchAlert";

// The owner's sender id. The real backend stamps a message's `from` from the
// verified JWT `sub`; the owner token's sub is the fixed owner id ("owner")
// ("owner"), so the owner's own messages arrive with from="owner"
// (NOT "ceo"). The mock stamps the same (MOCK_OWNER_ID) so a message reads as
// "me" (right-aligned, from=你) in BOTH mock and real mode.
const OWNER_ID = "owner";

// ⚠️ `oneLine()` USED TO LIVE HERE AND WAS DELETED 2026-08-21. It collapsed
// newlines and runs of spaces in the 「正在回覆」 banner's excerpt, and its own
// comment said the reason was that "a multi-line excerpt would push the composer
// around as the owner re-aims". That failure could not happen: office.css puts
// `white-space: nowrap` on `.chat__reply-banner__text`, which the body inherits,
// so the browser already collapses every newline to a space and lays the body
// out on one line. (The banner became TWO lines on 2026-08-22 — who on one, the
// excerpt on the next — which changes nothing here: each half is still one line
// box and `nowrap` is still what makes the body's newlines collapse.) Measured in a real Chromium at 390px — banner height
// 34px with a collapsed body and 34px with a deliberately un-collapsed one
// carrying two blank lines and a run of spaces. Mutating the function's body to
// `return body;` left all 2284 frontend tests green: it was the only surviving
// mutant in the whole frontend pass.
//
// So the layout rule has exactly one owner now, and it is the stylesheet.
// `.chat__reply-banner__text { white-space: nowrap }` is load-bearing and has a
// witness: deleting it turns the CT 「正在回覆」 banner test red, and the CT story
// feeds that banner a body WITH A NEWLINE so the collapse itself — not just the
// clipping — is what the one-line assertion measures.
//
// It fed no `title` attribute and nothing else, so nothing else moved with it.

/** A message is INTER-AGENT (agent↔agent) when NEITHER endpoint is the owner:
 * owner↔agent always has the owner as one side; agent↔agent never does. This is
 * the whole test — it needs no role lookup and matches "both sender & recipient
 * are agents, neither is owner". These messages surface in BOTH participants'
 * threads (the backend's `?with=<id>` filter is bidirectional) but render
 * COLLAPSED by default so the owner isn't flooded. */
function isInterAgent(m: ChatMessage): boolean {
  return m.from !== OWNER_ID && m.to !== OWNER_ID;
}

/** A contiguous run of same-kind messages. Consecutive inter-agent messages fold
 * into one collapsible `"inter"` group (identified by its first message id, a
 * stable collapse key); everything else is a `"normal"` run rendered inline. */
type MessageGroup =
  | { kind: "normal"; messages: ChatMessage[] }
  | { kind: "inter"; id: string; messages: ChatMessage[] };

/** Fold the flat oldest→newest stream into contiguous groups, coalescing runs of
 * inter-agent messages so each run becomes ONE collapsible block. Order and
 * membership are preserved exactly — this only partitions, never reorders. */
function groupMessages(messages: ChatMessage[]): MessageGroup[] {
  const groups: MessageGroup[] = [];
  for (const m of messages) {
    const inter = isInterAgent(m);
    const last = groups[groups.length - 1];
    if (inter && last?.kind === "inter") {
      last.messages.push(m);
    } else if (!inter && last?.kind === "normal") {
      last.messages.push(m);
    } else if (inter) {
      groups.push({ kind: "inter", id: m.id, messages: [m] });
    } else {
      groups.push({ kind: "normal", messages: [m] });
    }
  }
  return groups;
}

/** Format an epoch-second ts as a local hh:mm — never fabricate a display string. */
function formatTime(ts: number): string {
  return new Date(ts * 1000).toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
  });
}

/** 🔴 EVERYTHING THIS COMPONENT TRACKS PER CONVERSATION, IN ONE RECORD (T-48,
 * fourth-review rebuild).
 *
 * ChatArea is NOT remounted when the selected member changes (OfficePage
 * renders one instance), so every one of these used to be its own `useRef`
 * plus a line in a hand-written "did the peer change? zero these out" block.
 * That list is exactly the thing four reviews kept finding a hole in — and it
 * really did have three at the time of writing (`loadingOlder`,
 * `pendingLatestScroll`, plus the ones judged harmless). The list is now
 * machine-maintained: `useKeyedRecord` rebuilds this whole record when the
 * peer changes, and because `freshChatSession` is an object literal of the
 * full type, a field added without a reset value does not compile.
 *
 * ⚠️ WHAT DOES NOT BELONG IN HERE: DOM refs (`inputRef`, `messagesRef`, …),
 * anything whose lifetime is the COMPONENT rather than the conversation
 * (`didMountAttachRestoreRef`, `jumpSettleRef`), and anything mirroring live
 * browser state (`isComposingRef`). Each of those is annotated where it is
 * declared. */
type ChatSession = {
  /** ② ENTRY POSITIONING: entering a conversation with unread messages must
   * land on the FIRST unread message, not the bottom. The anchor is derived
   * from `member.unreadCount` (the roster badge count) SNAPSHOT at
   * conversation entry — the race-free source. Since T-48 the LISTING no
   * longer writes a watermark, but the window that opens on it does: the
   * read-receipt effect fires the moment the first page lands and the roster's
   * unreadCount refetches to 0 right after. The clearer moved from the
   * server's side effect to this component's own explicit write; the race did
   * not go away, so neither does the snapshot. unreadCount counts exactly the
   * peer→owner messages above the watermark, so the first unread = the
   * earliest of the LAST `unreadCount` peer→owner messages in the thread. */
  initialUnread: number;
  /** Is the scroll viewport near its bottom? A new incoming message may only
   * pull the view down when it is — if the owner scrolled UP to read history,
   * an arrival must NOT yank them back. */
  nearBottom: boolean;
  /** Ids seen on the previous messages render — the diff basis for "which
   * messages are NEW" (a refetch replaces the whole array, so append detection
   * must go through ids, not length). */
  prevIds: Set<string>;
  /** T-bf82 scrollback: the pre-fetch scroll-geometry snapshot an older-page
   * prepend restores from (null = no older page in flight/pending). */
  prependAnchor: { firstId: string; height: number; top: number } | null;
  /** The UI-side in-flight lock over `useChat`'s own, so repeated scroll
   * events near the top cannot re-snapshot `prependAnchor` mid-flight.
   *
   * 🔴 IT IS IN THIS RECORD BECAUSE IT USED TO BE THE ONE THAT WAS NOT
   * (fourth-review R4-3). As a plain ref it was CROSS-PEER, and the argument
   * for leaving it that way was "the try/finally releases it after one request
   * either way". That is only true of a promise that SETTLES: `api.listChat`
   * has neither a timeout nor an AbortController (http.ts gives a deadline to
   * the SSE probe and to nothing else), so one hung GET froze scrollback in
   * EVERY conversation for the rest of the session, with no spinner and no
   * error. Per conversation, a hung request now strands only the record it was
   * started on — and that record dies with its conversation. */
  loadingOlder: boolean;
  /** One-shot: entry positioning (bottom OR first-unread) ran here. */
  initialPositioned: boolean;
  /** Is the CURRENT unread run (the block below the divider) still OPEN — i.e.
   * the owner has not reached the bottom since the divider anchored? While
   * open, further arrivals belong to the SAME run (the divider stays put).
   * Once closed (bottom reached = everything seen), the next unseen inbound
   * starts a NEW run and RE-ANCHORS the divider — the chip and the divider
   * share ONE "start of the new messages" anchor. */
  unreadRunOpen: boolean;
  /** Entry positioning wants the divider scrolled into view ONCE. A
   * chip-driven divider re-anchor must NOT scroll — the owner is reading
   * history and must never be yanked. */
  entryScrollPending: boolean;
  /** B3 跳到原訊息: the jump target already consumed (one-shot per id — an SSE
   * refetch must never re-scroll). */
  jumpConsumed: string | null;
  /** The target this component has ALREADY spent an anchor-window fetch on
   * (T-48 ③). Separate from `jumpConsumed` on purpose: the fetch is what makes
   * the jump possible, so it happens BEFORE the jump is consumed, and this is
   * what stops the effect firing a second pair of requests on every re-render
   * while the first pair is still in flight. */
  jumpFetched: string | null;
  /** 🔴 THE BUDGET IS NOT THE TRIGGER (T-48, R3-5). `jumpRetry` state exists
   * only to re-run the reactor, so it can never go back down —
   * `setJumpRetry(0)` from an already-0 state re-renders nothing and the retry
   * button would do NOTHING AT ALL. The budget therefore lives here, and the
   * button resets it: a person who asks for another try gets a full one, not
   * the remains of the automatic ones. */
  autoJumpRetries: number;
  /** T-e987 compose seed: the seed value already applied (one-shot per
   * distinct value, so the same taskNo can seed another peer). */
  seedConsumed: string | null;
  /** Set when 回到最新 had to FETCH the live tail first (T-48 ③) — consumed by
   * the settle effect once the replacement thread has rendered.
   *
   * 🔴 IN THIS RECORD, not cross-peer: inherited by the next conversation it
   * would scroll a room the owner just entered AT AN ANCHOR straight to the
   * live tail — this ticket's own failure shape, arriving from the previous
   * conversation's button press. */
  pendingLatestScroll: boolean;
};

/** The empty thread rendered while `messages` still belongs to the room the
 * owner has just left (see `shownMessages`). Module-level so the identity is
 * stable across renders. */
const NO_MESSAGES: ChatMessage[] = [];

function freshChatSession(unreadCount: number): ChatSession {
  return {
    initialUnread: unreadCount,
    nearBottom: true,
    prevIds: new Set(),
    prependAnchor: null,
    loadingOlder: false,
    initialPositioned: false,
    unreadRunOpen: false,
    entryScrollPending: false,
    jumpConsumed: null,
    jumpFetched: null,
    autoJumpRetries: 0,
    seedConsumed: null,
    pendingLatestScroll: false,
  };
}

/** A file that finished reading for a room the owner is no longer looking at —
 * another conversation, or no conversation at all because the page was left. It
 * is invisible either way (the composer renders only the rows stamped with the
 * room on screen); this is purely about not LOSING it, since the staged list is
 * wiped on the next conversation switch and dies outright on an unmount. It goes
 * into ITS OWN room's draft, which is what that room's composer restores from.
 *
 * Dedup is by `key` because the same row may already be in the draft: the
 * persist effect below saves the live staged list on every change, so a file
 * that was staged and then abandoned is in the draft AND in state.
 *
 * 🔴 THE COUNT CAP IS NOT APPLIED HERE, AND THAT IS THE FIX FOR R10-3. The
 * previous shape refused a file when the target draft already held
 * CHAT_MAX_ATTACHMENTS and reported success — the file was destroyed with no
 * notice in either room. The cap is a staging-time rule about ONE message; a
 * file that has already been read is not a candidate to refuse, it is data
 * somebody is holding. So the draft is allowed over the cap and the owner sees
 * every file waiting when they come back, with the over-cap ones there to
 * remove (§3 rule 4: blocking a commit is never a licence to destroy the file).
 *
 * ⚠️ WHAT MAKES THAT SAFE IS THE COMPOSER, NOT THE SERVER (T-48, R11-3). This
 * comment used to say the over-cap send "is refused by the server, visibly".
 * The server does refuse it (400) — but this app has no toast, no error row for
 * a failed send and no `unhandledrejection` reporter, so the refusal reaches
 * nobody: the send button stayed lit and every press did nothing, forever.
 * The composer now refuses the SEND itself, with the same 「最多 N 個檔案」
 * notice staging uses — see `overAttachmentCap` below.
 *
 * 🔴 AND THE DRAFT IS NOT THE ONLY PLACE IT HAS TO REACH (T-48, R11-2). A room
 * the owner has come BACK to already has a live composer, and that composer
 * restored its draft when it mounted — before this file existed. Writing only
 * to the draft left the file invisible AND doomed: the persist effect below
 * saves the composer's own (file-less) list on the next keystroke, over the
 * top. So the arrival is also announced to whichever composer is showing that
 * room right now. */
function keepAttachmentsWithTheirRoom(
  peer: string,
  arriving: PendingAttachment[],
): void {
  const saved = getChatDraft(peer);
  const kept = saved?.attachments ?? [];
  const fresh = arriving.filter((a) => !kept.some((k) => k.key === a.key));
  if (fresh.length > 0) {
    saveChatDraft(peer, {
      text: saved?.text ?? "",
      attachments: [...kept, ...fresh],
      replyTo: saved?.replyTo,
    });
  }
  // Announced whether or not the draft write happened: the draft may already
  // hold the row while the composer on screen does not (it mounted, restored,
  // and the row was written after). `adoptAttachments` dedups by key.
  liveComposers.get(peer)?.(arriving);
}

/** The composer that is showing a given room RIGHT NOW, if one is mounted —
 * keyed by peer id, at most one entry per room because only one `ChatArea`
 * exists at a time. This is the half `chatDraftStore` cannot be: the store is
 * where a file WAITS, and a composer that has already read it is not going to
 * read it again. Registration is the mounted composer's own doing (the effect
 * in `ChatArea` below), so nothing here has to be told when a room is left. */
const liveComposers = new Map<
  string,
  (arriving: PendingAttachment[]) => void
>();

export function ChatArea({
  member,
  members = [],
  workers = [],
  onOpenDetail,
  onOpenTasks,
  onOpenRoleSettings,
  onWake,
  jumpToMsgId,
  draftSeed,
  headerSub,
  headerTaskTitle,
}: {
  member: Member;
  // The full office roster, used to resolve a message's sender id → display name
  // for INTER-AGENT (agent↔agent) messages, where the sender is neither the owner
  // nor necessarily the window's `member`. Optional (defaults empty) so a caller
  // that only cares about owner↔agent threads need not thread it through.
  members?: Member[];
  // The LIVE outsource workers, the sender-label twin of `members`. This list
  // is LIVE-ONLY by construction — HandleListOutsourceWorkersApiOutsourceWorkersGet
  // skips WorkerStatusReleased — so it is NOT what rescues a released sender;
  // `useWorkerCodenames` below is. Its two jobs here are both about live ids:
  //   1. `nameOf` reaches it only AFTER `members` failed to resolve the id, so
  //      it is the codename source for a caller that passes no roster (the
  //      prop is optional) or whose roster has not loaded yet — without it
  //      such a sender's label degrades to its raw ow- id while the left rail
  //      shows the codename.
  //   2. it is the EXCLUSION SET behind `unknownOwIds`: every ow- participant
  //      NOT in this list is handed to the lazy per-id codename read. Passing
  //      the live list is what keeps that per-id read off the live workers.
  // Optional (defaults empty) for the same reason `members` is.
  workers?: OutsourceWorkerView[];
  // Open the member detail page. Optional: when absent the header is NOT
  // interactive (no cursor/role/tabindex) so we never advertise a dead click.
  onOpenDetail?: () => void;
  // T-dfae 任務圖示: jump to the tasks page filtered to this peer's unfinished
  // tasks. Optional — absent = no button (an outsource peer's tasks are not
  // separable from every other worker's, so the jump would lie).
  onOpenTasks?: () => void;
  // T-dfae 角色設定圖示: jump to this peer's role definition page. Optional —
  // absent = no button (an outsource peer has no role to define).
  onOpenRoleSettings?: () => void;
  // T-94c1 就地喚醒: wake this member from the chat itself (calls activateMember
  // in the parent). Optional — absent = no in-chat wake button (an outsource
  // worker is spawn/task-driven, not activate-woken, so the button would lie);
  // the offline composer then degrades to the plain "go to member panel" bar.
  //
  // 🔴 May resolve with the activate's {@link MemberActivateResult} (T-7fa1).
  // `activationPending: true` = the wake was accepted but NOTHING was
  // dispatched; the wake row must roll back its 「喚醒中…」 and say so, because
  // no lifecycle change is coming to clear it. A caller returning void keeps the
  // old silent behaviour, so the wire-up returns the adapter's result verbatim.
  onWake?: () => void | Promise<MemberActivateResult | void>;
  // Locate + highlight this message once the thread loads. One-shot per id —
  // later SSE refetches never re-scroll.
  //
  // 🔴 THE COCKPIT ROUTES HERE AGAIN (owner 2026-08-29: 「1 跟 2 變回去原本那
  // 樣」). The 請示 page's 跳到原訊息 and the inline task card's 在聊天室回覆
  // both write #office/chat/<id>/msg/<msgId> once more; only the chat bubble's
  // 看原訊息 takes the overlay (hooks/useQuotedMessageOverlay).
  //
  // ⚠️ AND THE KNOWN COST CAME BACK WITH THEM, knowingly: this path can only
  // find a row the thread has already PAINTED. When the target is outside the
  // loaded window the search misses and the reader lands on the newest message
  // with nothing on screen saying so. The owner accepted that trade on
  // 2026-08-29 and parked the fix (「無法跳回去很久以前訊息的問題我們改天再
  // 說」). The "honest miss" below is honest in that it never fabricates a
  // location — it is not honest to the READER, and that gap is deliberate, not
  // an oversight to patch in passing.
  jumpToMsgId?: string;
  // T-e987 compose seed: a one-shot draft prefix (e.g. "[T-7d40] ") the 任務卡
  // 負責人/建立者 label routes here to (#office/chat/<id>/compose/<taskNo>) so
  // the owner starts a message already tagged with the task. Seeds ONLY an
  // empty draft (never clobbers what the owner is typing) and only once per
  // distinct seed value; the owner can freely delete it.
  draftSeed?: string;
  // Header subtitle OVERRIDE. Default (absent) = the shared PresenceBadge —
  // the single member-presence truth. An OUTSOURCE chat (M3 §4.2) passes its
  // own line instead: a worker is anonymous and task-bound, with NO member
  // presence to project — rendering the badge there would fabricate one.
  headerSub?: React.ReactNode;
  // T-3451: the peer's CURRENT task title, shown FULL (no clamp) as a third
  // header line under the sub — owner 圖2: the selected worker's header shows
  // the complete task title, untruncated. An outsource worker's title rides
  // OutsourceWorkerView.taskTitle. Absent/"" ⇒ nothing rendered (a released /
  // taskless peer never grows an empty line here).
  headerTaskTitle?: string;
}) {
  const { t, msg } = useI18n();
  // 🔴 THE PER-CONVERSATION MUTABLE STATE, REBUILT BY MACHINE (T-48). ChatArea
  // is NOT remounted when the selected member changes (OfficePage renders one
  // instance), so this used to be a hand-written list of ~13 assignments that
  // four reviews kept finding holes in. `useKeyedRecord` owns the list now: a
  // switch replaces the whole record, and an async job that captured the old
  // one settles into an orphan instead of clearing the NEW conversation's
  // latch. Adding a field without a reset value does not compile.
  //
  // 🔴 AND IT IS THIS COMPONENT'S VISIT TOKEN (T-48, R6-1). Its identity — not
  // `member.id` — is what every "is this still mine?" question below is asked
  // against: the React-state half (`useKeyedState(session, …)`), the draft swap
  // and the explicit guards on the things no per-key primitive can own (the DOM
  // and the screen). A→B→A hands back a THIRD record, so a late writer from the
  // FIRST visit to A is recognised as late even though the peer id is equal
  // again. See the note at the top of `useKeyedRecord`.
  //
  // The entry unread snapshot lives in it, taken synchronously at the first
  // render for this visit, strictly before any effect runs.
  const session = useKeyedRecord(member.id, () =>
    freshChatSession(member.unreadCount),
  );

  const isOffline = member.status === "offline";
  // T-9c3c (owner 2026-07-24, "有時候離線還是沒辦法發訊息"): a REAL roster member
  // (onWake wired) can ALWAYS be messaged — the server NEVER gates on recipient
  // presence (api_chat.PutChat lands the message regardless, UnreadCounts counts
  // it, the member reads it on next boot). So the composer's ONLY lock reason is
  // "no queue path at all": a synthetic released/removed peer (read-only, T-661b
  // — it must never grow a typable composer or a false "will queue" promise) or
  // an outsource worker; both are deliberately passed NO onWake by OfficePage.
  //
  // This REVERSES T-94c1's extra lock on waking/stopping (owner 2026-07-17),
  // which was the intermittent "sometimes offline can't be messaged" bug: an
  // offline member reads lifecycle `waking` for the wake's configured TTL after ANY
  // wake attempt (the ⚡喚醒 button itself included) and `stopping` while it
  // winds down — both are transient presence states an offline member passes
  // through, and the message the "dying session could miss it" rationale worried
  // about is the SAME message the server was going to queue anyway. Locking on
  // them dropped a message the design says must always send. Presence-driven:
  // `member` comes from the SSE-refetched roster, so a lifecycle flip re-renders
  // without a reload. Reads the five-state `lifecycle`, not the collapsed
  // tri-state `status`.
  const hasQueuePath = !!onWake;
  const composerLocked = !(member.lifecycle === "online" || hasQueuePath);
  // Non-online but messageable (a live member that is offline/stopped/waking/
  // stopping): composer unlocked, with the queue notice + in-place wake row
  // above the input (owner mockup). Online needs neither; a peer with no queue
  // path is locked above and never reaches here.
  const offlineQueue = hasQueuePath && member.lifecycle !== "online";
  // Wake-click instant feedback: the activate POST only writes the wake INTENT;
  // presence flips to waking via SSE shortly after. Optimistically disable the
  // button meanwhile so a double-tap can't fire two activates.
  //
  // 🔴 PER VISIT, BY MACHINE (T-48, R6-1 — the census in latch-inventory §2.4
  // had missed this pair). These two are per-conversation ("A's optimistic
  // notice must not linger on B's now-shared wake row") and used to say so with
  // a plain `useState` plus a hand-written reset effect keyed on `member.id` —
  // the exact shape `useKeyedState` exists to retire, one commit late (an
  // effect runs AFTER the frame that already showed the previous
  // conversation's 「喚醒中…」) and with the same string-guarded async ending
  // that R6-1 walked through: on the second visit to the same peer the reset
  // did not fire and the first visit's verdict wrote straight into it.
  const [wakePending, setWakePending] = useKeyedState(session, false);
  // T-7fa1: the activate reported that nothing was dispatched. Distinct from
  // wakePending — "not waiting, because nothing was sent". Never both true.
  const [wakeUndispatched, setWakeUndispatched] = useKeyedState(session, false);
  // The OTHER thing that clears the optimistic bridge: reality moving on this
  // member. Once presence reflects a fresh lifecycle the local optimism has
  // handed off to the real state (`waking` drives the label below), so a
  // dispatched-but-silently-died wake (waking→offline after the configured
  // waking TTL) clears instead of latching 「喚醒中…」 forever. The peer half of
  // this effect's old dependency list is gone — the record does it, earlier.
  useEffect(() => {
    setWakePending(false);
    setWakeUndispatched(false);
  }, [member.lifecycle]);
  // The wake row's button shows "喚醒中…" while a wake is in flight — either the
  // just-clicked optimism, or the server-confirmed `waking` presence itself.
  const wakeInFlight = wakePending || member.lifecycle === "waking";

  const {
    messages,
    messagesPeer,
    peerLastRead,
    send,
    markRead,
    hasMore,
    loadOlder,
    gapSuspected,
    hasNewer,
    loadNewer,
    loadAround,
    resetToLatest,
    // 🔴 ANCHOR-FIRST ENTRY (T-48, owner ruling). The target is named at
    // SUBSCRIPTION time, so a room entered through 跳到原訊息 / a kept link never
    // loads the live tail first and then throws it away — see useChat's note.
    // The fetch itself still happens below, in the jump reactor, because the
    // viewport, the highlight and the miss notice are this component's business.
  } = useChat(member.id, jumpToMsgId);

  // 🔴 ANOTHER ROOM'S THREAD IS NEVER PAINTED UNDER THIS ROOM'S HEADER (T-48,
  // R11-1). `useChat` swaps `messages` and `messagesPeer` TOGETHER, but one
  // commit AFTER `member` changes — this component is not remounted on a
  // switch, so there is a committed, paintable frame in which the header, the
  // roster selection and the composer are all B while the message list is
  // still A's. Every EFFECT below already refuses to act on that frame
  // (`messagesPeer !== member.id` guards the entry positioning, the scroll
  // reactor, the mark-read and the scrollback); the RENDER did not, so A's
  // message bodies, A's quote rows and — the tenth review's finding — an open
  // document preview belonging to A's file chip were all drawn under B's name.
  //
  // The switch already flashes an empty thread one commit later (useChat's
  // reset), so refusing to draw here shows nothing that was not about to be
  // shown anyway. The guard is a render-time derivation rather than a rule at
  // each of the dozen places that read a message, so a new reader of `messages`
  // cannot forget it.
  //
  // 🔴 IT IS ALSO WHAT KEEPS `messagesRef`'s UNGUARDED READERS HONEST, and that
  // was an ACCIDENT until it was written here (T-48, R12-4). The scroll
  // container is rendered only inside the `shownMessages.length > 0` branch, so
  // on a mismatched frame the element does not exist, `messagesRef.current` is
  // null, and every reader — including the three that do NOT gate on
  // `messagesPeer` (`onMessagesScroll`, which would otherwise `markRead` the
  // PREVIOUS room's newest ts; the entry-scroll effect, which gates on
  // `session.entryScrollPending`; and `jumpToLatest`, which is only reachable
  // from a button drawn in that same branch) — returns at its own `if (!el)`.
  // Anyone moving the container out of this branch, or giving it a placeholder
  // that keeps the ref alive across the switch frame, is re-opening all three.
  const shownMessages = messagesPeer === member.id ? messages : NO_MESSAGES;

  // Released-worker codenames: an ow- participant that is NOT in the live
  // `workers` list (task closed → dropped off) still has a codename on the
  // per-id read — resolve it lazily so the label never degrades to the raw id.
  const unknownOwIds = useMemo(() => {
    const out = new Set<string>();
    for (const m of messages) {
      // 🔴 THE QUOTED SENDER IS A PARTICIPANT TOO. `m.replyToChat.from` is an
      // id this thread RENDERS (`quoteWho = nameOf(quoted.from)`), so leaving it
      // out of this set meant the codename fallback never fired for it: the very
      // same released outsource worker showed a codename on its own row and a
      // raw `ow-…` id when quoted — and the quote row's aria-label read
      // 「引用 ow-8808ccf51794」. One display path, two identities.
      for (const id of [m.from, m.to, m.replyToChat?.from ?? ""]) {
        if (
          id.startsWith("ow-") &&
          id !== member.id &&
          !workers.some((w) => w.id === id)
        ) {
          out.add(id);
        }
      }
    }
    return Array.from(out);
  }, [messages, workers, member.id]);
  const codenames = useWorkerCodenames(unknownOwIds);

  // The owner's own display name, taken from the ONE place the cockpit already
  // resolved it (App's useOwnerName, handed down by OwnerNameProvider). Read
  // through context rather than by mounting the hook again: this component must
  // not fetch while it paints — ChatArea.quote-no-fetch.test.tsx asserts the api
  // client is touched zero times to render a thread.
  const ownerDisplayName = useOwnerDisplayName(t.user);
  // Resolve a participant id → display name: prefer a roster match, else the raw
  // id (never fabricate). The window's own `member` is always resolvable even if
  // it is not in the passed roster.
  const nameOf = (id: string): string => {
    if (id === member.id) return member.name;
    // 🔴 THE OWNER HAS A NAME TOO, AND IT IS THE ONE HE SET. T-4e95 is the first
    // display path that feeds the owner's OWN id into nameOf — replying to your
    // own message names the sender in the composer banner and in the quote row —
    // and without this branch it fell through to the raw id and printed
    // 「正在回覆 owner」.
    //
    // 🔴 `t.user` IS THE THEME'S DEFAULT WORD FOR THE HUMAN, NOT HIS NAME —
    // 「CEO（你）」 as shipped, 「市長（你）」 under the 仙俠 theme — and the
    // nickname he actually set lives server-side behind /api/settings
    // (hooks/useOwnerName). Printing the default here while his own profile pill
    // reads 「韓立（你）」 renders one person under two names on one screen; the
    // owner reported exactly that from the running cockpit. It is a regression
    // of this ticket, not old debt: this branch is what T-4e95 added.
    //
    // `ownerDisplayName` resolves to the stored nickname when there is one and
    // to `t.user` otherwise — INCLUDING when the settings read failed, because a
    // failure must never masquerade as "no name set" (useOwnerName's own rule).
    if (id === OWNER_ID) return ownerDisplayName;
    // Server-authored messages (T-ba04 reassign handover, sender="system") are
    // not a roster member — render the synthetic sender as the localized 「系統」
    // label instead of the raw "system" id.
    if (id === "system") return t.chat.systemSender;
    const rosterName = members.find((m) => m.id === id)?.name;
    if (rosterName !== undefined) return rosterName;
    // Outsource workers live outside the 正職 roster — resolve their codename
    // (the same identity the left rail shows) before giving up on the raw id:
    // live workers from the passed list, released ones from the lazy per-id
    // cache.
    const codename =
      workers.find((w) => w.id === id)?.codename ?? codenames.get(id);
    if (codename !== undefined) return msg.outsourceLabel(codename);
    return id;
  };
  // 「寄件者 → 收件者」 — the ONE spelling of a message's direction in this
  // component. The message rows have written it this way for inter-agent
  // traffic since before T-4e95; the quote row and the composer banner now use
  // the SAME join, so a reader never meets two ways of saying who-to-whom.
  const directionLabel = (from: string, to: string): string =>
    `${nameOf(from)} → ${nameOf(to)}`;
  // The shared 看原訊息 exit. Declared here rather than at the top of the
  // component because it is handed `nameOf`, and `nameOf` needs the roster
  // hooks above it. It is still an unconditional top-level hook call.
  // `session` is this component's visit token (see its declaration): the read
  // behind 看原訊息 belongs to the visit that clicked it, not to whoever is on
  // screen when it lands (T-48, R8-3).
  const quotedMessage = useQuotedMessageOverlay(session, nameOf);
  // Is the owner ACTUALLY looking (window focused + tab visible)? Read side
  // effects (mark-read below) are gated on this: a backgrounded window must
  // never consume unread state (the roster badge has to survive until the
  // owner really comes back and looks).
  const windowActive = useWindowActive();
  // T-8aaa draft survival: seed the text from the per-peer draft store so a
  // 跳頁-then-return (which unmounts/remounts this component) restores what the
  // owner had typed. Lazy-init covers the FIRST mount for the initially-selected
  // peer; a later peer SWITCH (this instance is reused, not remounted) restores
  // in the peer-switch render block below. Staged attachments are restored
  // alongside (they live in useAttachmentStaging, set via its API).
  const [draft, setDraft] = useState(() => getChatDraft(member.id)?.text ?? "");
  // T-4e95 「回覆這則」: the message the composer is currently replying to, or
  // null in the ordinary send state. It rides the DRAFT store, not just this
  // component's state, for the same reason the text does — a 跳頁-and-back that
  // restored the words but silently dropped the reply target would send the
  // message somewhere the owner did not aim it, and look like a normal restore
  // while doing it.
  const [replyToId, setReplyToId] = useState<string | null>(
    () => getChatDraft(member.id)?.replyTo ?? null,
  );
  // The staged attachments (pasted images AND/OR picked/dropped files), held
  // until the message is sent — the SHARED staging state machine
  // (useAttachmentStaging: size/count caps, paste/pick funnels, previews).
  // 🔴 R9-1, AND WHY IT IS NO LONGER A GUARD (owner ruling). This component is
  // NOT remounted on a conversation switch, so a FileReader started in A can
  // complete while B is on screen. The first fix was a commit guard on the
  // visit token; this is the shape the owner asked for instead — each staged
  // file carries the room it was picked for, and the composer renders only the
  // ones stamped with the room on screen. Nothing here has to REMEMBER to ask.
  const {
    pendingAttachments,
    attachError,
    stageFiles,
    onPaste,
    onPickFile,
    removeAttachment,
    clearAttachments,
    adoptAttachments,
    restoreAttachments,
  } = useAttachmentStaging(member.id, keepAttachmentsWithTheirRoom);
  // What the in-cockpit full-view overlay is showing (null = closed). TWO ways
  // in, one surface: an incoming MESSAGE body (the corner 放大閱讀 button — the
  // text is already in hand, so there is nothing to fetch, download or share),
  // or a STAGED image still sitting in the composer (T-f014 — the bytes are in
  // hand as a data: URI, so 下載 is honest but no blob id exists to share). The
  // kind is carried explicitly so no branch has to be guessed from which field
  // happens to be set.
  //
  // ⚠️ THERE USED TO BE A THIRD, `kind: "attachment"` (a STORED blob), and it
  // had no caller (T-48, R11-1). A stored attachment's chip is rendered by
  // `AttachmentStrip`, which mounts its OWN `MarkdownPreviewOverlay` — so the
  // branch was dead code that read like the live path, and the tenth review's
  // measurement of a leaking document preview was filed against this state
  // while the overlay it measured was the strip's. Deleted rather than left as
  // documentation of an intention.
  // 🔴 PER VISIT (T-48, R10-1 — the twelfth instance of this family). This was
  // left as a plain `useState` when its twin `galleryOpen` was keyed, on the
  // written premise that `.md-preview`'s full-screen backdrop blocks every
  // gesture that could change the peer. The tenth review drove it and the
  // premise is false TODAY: the site routes on the hash (`OfficePage`'s
  // `useHashRoute`, whose `route.chatId` IS the selected peer), so the browser's
  // back/forward buttons and any link into another conversation swap `member`
  // without the backdrop being touched. Measured: open A's document preview,
  // switch to B — the header says Bruno while the overlay still shows A's
  // filename and A's content.
  //
  // Keying the overlay is also what makes the 22 unguarded async landing points
  // inside `MarkdownPreviewOverlay` structurally safe: the overlay itself cannot
  // outlive the visit, so none of its writers can either.
  const [mdPreview, setMdPreview] = useKeyedState<
    | { kind: "message"; title: string; source: string }
    | { kind: "staged-image"; title: string; imageSrc: string }
    | null
  >(session, null);
  // 「看原訊息」 — reading that one message and showing it whole is NOT this
  // component's business any more (T-0b78). It lives in
  // hooks/useQuotedMessageOverlay. ⚠️ The quote row on a chat bubble is now the
  // hook's ONLY caller: the 請示 page and the inline task card went back to
  // NAVIGATING (owner 2026-08-29), so do not describe this as a shared exit.
  // The hook is called below, once `nameOf` exists — it titles the overlay with
  // the roster-aware name this window already resolves.
  // M2-3 file & image gallery panel (header icon toggles it).
  //
  // 🔴 PER VISIT, NOT PER OVERLAY (T-48, R9-2). §2.4 used to exempt this
  // alongside `mdPreview` on the grounds that "the overlay covers the page, so
  // the switch gesture is blocked". That is true of `.md-preview`
  // (`position: fixed; inset: 0` + a backdrop) and FALSE of this one:
  // `.chat__gallery` is `position: absolute; right: 0; width: min(340px, 100%)`
  // — a side panel inside the chat column with no backdrop, and the roster is
  // fully clickable beside it. Measured: open A's gallery, click B in the
  // roster, and the header says Bruno while the panel still shows A's files
  // labelled with A's sender name. Closing on a switch also remounts the panel,
  // so its `entries` / `loaded` / `previewKey` start clean for the new room.
  const [galleryOpen, setGalleryOpen] = useKeyedState(session, false);
  // The attachment whose share link was just copied (transient 「已複製」
  // feedback on that one button; null = none).
  // Inter-agent (agent↔agent) groups that the owner has EXPANDED (keyed by the
  // group's first-message id). Collapsed is the default — a group is expanded
  // only once its id lands here, so the owner opts in per block.
  //
  // 🔴 PER VISIT (T-48, R11-9). This was the last plain `useState` in this
  // component that outlived a conversation switch with nothing said about it —
  // not a keyed slot, not adopted by the switch block below, not even a line of
  // comment claiming it was deliberate. It holds MESSAGE IDS OF OTHER ROOMS,
  // and `groupExpanded` asks `has(m.id)` of whatever is on screen now, so the
  // only thing standing between it and a wrongly-expanded block is that message
  // ids happen to be globally unique. That is a property of today's data, not a
  // property of this structure — and the set never shrinks, so it only ever
  // gets more chances to collide. Keying it costs one thing, honestly: a block
  // opened in A is collapsed again on the way back to A, which is what every
  // other per-conversation view state here already does.
  const [expandedGroups, setExpandedGroups] = useKeyedState<Set<string>>(
    session,
    () => new Set(),
  );
  // Expanded 判定 is membership-based (T-bf82 收折 × 分頁): a group counts as
  // expanded when ANY of its message ids is in the set — a history prepend can
  // merge a loaded older run into an existing expanded block, CHANGING the
  // group's first-message id (the collapse key); keying strictly on group.id
  // would silently collapse the block the owner had opened. Toggling open
  // still stores group.id; toggling closed removes EVERY member id so no
  // stale key keeps the merged block open.
  const groupExpanded = (group: { id: string; messages: ChatMessage[] }) =>
    expandedGroups.has(group.id) ||
    group.messages.some((m) => expandedGroups.has(m.id));
  const toggleGroup = (group: { id: string; messages: ChatMessage[] }) =>
    setExpandedGroups((prev) => {
      const next = new Set(prev);
      if (
        next.has(group.id) ||
        group.messages.some((m) => next.has(m.id))
      ) {
        next.delete(group.id);
        for (const m of group.messages) next.delete(m.id);
      } else {
        next.add(group.id);
      }
      return next;
    });
  const inputRef = useRef<HTMLTextAreaElement>(null);
  // Hidden native file input the attach button triggers (the iPhone fix — no
  // Cmd+V needed; tap the paperclip → OS file/photo picker).
  const fileInputRef = useRef<HTMLInputElement>(null);
  // IME composition guard. While a CJK (中/日/韓) candidate is being composed the
  // input fires keydown with keyCode 229 and a final Enter that CONFIRMS the
  // candidate — that Enter must NOT be read as "send". We track composing in a
  // ref (not state) so the keydown handler always sees the live value with no
  // stale-closure lag. onCompositionEnd may fire slightly AFTER the confirming
  // keydown in some browsers, so keydown also checks nativeEvent.isComposing /
  // keyCode 229 as belt-and-braces.
  // ⚠️ NOT in the session record: it mirrors a LIVE DOM event pair
  // (compositionstart/compositionend) rather than anything about the
  // conversation, and clearing it from outside would desync it from the
  // browser's own composition state.
  const isComposingRef = useRef(false);
  // Phone viewport → Enter inserts a newline instead of sending (no physical
  // keyboard, so Shift+Enter is impossible); sending is via the send button.
  const isMobile = useIsMobile();
  // 🔴 OVER THE COUNT CAP THE COMPOSER REFUSES THE SEND ITSELF (T-48, R11-3).
  // A draft is allowed to hold more than CHAT_MAX_ATTACHMENTS — that is R10-3's
  // fix, and files waiting in a draft are somebody's data, not a rule to
  // enforce. Sending them is a different act, and it is the one the cap is
  // about. The server refuses an over-cap send with a 400, but this app has no
  // toast, no error row behind `submit()`'s catch and no global rejection
  // reporter, so the refusal was invisible: the send button stayed lit and
  // every press did nothing at all, with no hint that two files had to go.
  // Refusing here is the same notice staging already raises, on the surface the
  // owner is looking at, BEFORE a message can be lost to a silent 400.
  const overAttachmentCap = pendingAttachments.length > CHAT_MAX_ATTACHMENTS;
  // A message may carry text and/or attachments — sendable when EITHER present.
  const canSend =
    (draft.trim().length > 0 || pendingAttachments.length > 0) &&
    !overAttachmentCap;

  // The composer is a multi-line textarea (desktop: Enter sends, Shift+Enter
  // breaks a line; mobile: Enter breaks a line — see onKeyDown). Auto-grow to
  // the draft on EVERY draft change —
  // typing, the optimistic clear in submit(), and the failure restore all set
  // state, so sizing off the draft (not just typing events) keeps the box
  // honest in each path. CSS max-height caps the growth; past it the textarea
  // scrolls its own overflow so a long draft is always fully reachable.
  useLayoutEffect(() => {
    if (inputRef.current) autosizeTextarea(inputRef.current);
  }, [draft]);

  // Auto-scroll to the newest message (regression #6: the thread never scrolled,
  // so new messages landed below the fold). `messagesRef` is the scroll viewport
  // and `endRef` is a bottom sentinel we scroll into view. We only auto-pull when
  // the user is already near the bottom OR just sent a message — if they scrolled
  // UP to read history, a new incoming message must NOT yank them back down.
  const messagesRef = useRef<HTMLDivElement>(null);
  const endRef = useRef<HTMLDivElement>(null);

  // ===== LINE/FB-style unread jump (M2 batch 19) =====
  //
  // The per-visit session record is declared at the top of this component (it
  // is also the visit token every state and guard below is bound to).
  // Set once per conversation when entry positioning ran: the id of the first
  // unread message. Drives the "以下是未讀訊息" divider (kept for the whole
  // session, like LINE) and the initial scroll target.
  const [firstUnreadId, setFirstUnreadId] = useKeyedState<string | null>(
    session,
    null,
  );
  // ① IS THE NEWEST MESSAGE IN THE VIEWPORT? The round 回到最新訊息 arrow's
  // ONLY condition (owner card rc-72054864ff88) — not "scrolled more than a
  // screen", not "a new message arrived". Measured from the scroll viewport's
  // own geometry in `onMessagesScroll` and wherever this component moves the
  // viewport itself. Starts true: every entry path lands at the bottom or
  // measures honestly before this can be read.
  const [latestInView, setLatestInView] = useKeyedState(session, true);
  // ② THE NEW-MESSAGE PREVIEW STRIP's content — the LATEST unseen inbound
  // message (sender + body), or null when there is nothing waiting.
  //
  // 🔴 THE LATEST, NOT THE FIRST, AND IT IS REPLACED RATHER THAN QUEUED. The
  // pill this replaces said a constant sentence and so had nothing to update;
  // a strip that names a sender and quotes a line must show the CURRENT one,
  // and there must only ever be one of it (owner screenshot). The FIRST unseen
  // message keeps its own job — anchoring the 「以下是未讀訊息」 divider below —
  // which is why the two are tracked separately.
  const [newMsgPreview, setNewMsgPreview] = useKeyedState<{
    id: string;
    from: string;
    body: string;
  } | null>(session, null);
  // 🔴 T-48: the jump target the server has NO RECORD OF ("missing"), and — a
  // DIFFERENT fact that used to be collapsed into it — an anchor fetch that was
  // repeatedly OVERTAKEN by newer loads ("interrupted"). The fallback (open at
  // the bottom) is indistinguishable from a jump that worked, which is the very
  // silence this ticket exists to remove — so the outcome is state, and state is
  // rendered. A `console.warn` is not a user-visible thing.
  const [jumpNotice, setJumpNotice] = useKeyedState<
    null | "missing" | "unreachable" | "interrupted"
  >(session, null);
  // How many times a jump may be re-scheduled after being overtaken. `load()`
  // is held off for the duration of the anchor fetch, so losing the race even
  // once takes a deliberate 回到最新 or a send; three is a ceiling on a loop,
  // not a budget anybody is expected to spend.
  const MAX_JUMP_RETRIES = 3;
  // 🔴 T-48 (F3): the anchor fetch was OVERTAKEN, which is not the same fact as
  // "the message is gone" and must not be reported as one. Bumping this state
  // re-runs the reactor below — a ref alone would not, and the retry would sit
  // there until some unrelated render happened to carry it. BOUNDED: without a
  // ceiling a load that keeps winning the race turns "retry" into an unbounded
  // fetch loop, which is a worse failure than the one being fixed.
  const [jumpRetry, setJumpRetry] = useKeyedState(session, 0);
  // The transient highlight on the row a jump located (cleared after the flash).
  const [highlightMsgId, setHighlightMsgId] = useKeyedState<string | null>(
    session,
    null,
  );

  // 🔴 AND THE REACT-STATE HALF IS MACHINE-MAINTAINED TOO (T-48, R5-1). This
  // block used to carry six hand-written `setX(null)` lines beside the draft
  // swap — the same hand-written list `useKeyedRecord` had just removed from
  // the ref half, with the same two holes: a seventh per-conversation state
  // added without a seventh reset line compiles and goes green, and a setter
  // captured by an in-flight job belonging to the PREVIOUS conversation still
  // wrote into the CURRENT one (R5-1: an anchor fetch that ended `unreachable`
  // after the owner had moved on pasted the old room's failure banner, retry
  // button and all, onto the new one). Those six now declare themselves with
  // `useKeyedState(member.id, …)`: the reset IS the initial value, and the
  // setter is bound to the key it was taken for.
  //
  // What is genuinely left here is what no per-key primitive can do on its
  // own: the composer is not RESET on a switch, it is swapped to the new
  // peer's SAVED draft, which has to be read from storage and pushed through
  // the staging API. `visitRef` is the visit mirror for this block and is
  // therefore the one thing that cannot live in the record it mirrors; it is
  // also why the block does not run on first mount (the draft restore below
  // would double-apply on top of the mount-time one).
  //
  // 🔴 IT MIRRORS THE RECORD, NOT `member.id` (T-48, R6-1). This used to be
  // `peerIdRef`, a string, and every async guard below then asked "is this
  // still the same PERSON?" — which answers YES on the second visit to the
  // same person, letting the first visit's late failure banner, its
  // `scrollIntoView` and its wake verdict all land on a screen that is not
  // theirs. The record's identity answers the question the guards actually
  // mean, and answers it for the state half at the same time.
  const visitRef = useRef(session);
  if (visitRef.current !== session) {
    visitRef.current = session;
    // T-8aaa: swap the composer to the NEW peer's saved draft. Render-phase
    // state adjustment (same pattern as the resets above) so the committed
    // render already carries the new peer's text+attachments — no stale frame
    // and no cross-peer mis-persist by the save effect below. Attachments come
    // back through `restoreAttachments`, whose functional set applies the
    // snapshot unless rows for the room being ENTERED are already staged.
    //
    // 🔴 THERE IS DELIBERATELY NO `clearAttachments()` HERE (T-48, R12-1). It
    // used to lead this block, back when the staged list was not stamped with
    // its room and a switch really did have to wipe it. Now every row and every
    // notice says whose it is, so the only thing the call still did was destroy
    // this room's `attachError` — DURING RENDER, before the room it belonged to
    // could paint it. That is R11-4's bug, and R11-4's own fix (holding a notice
    // whose target is not the current one) could not reach it: on the switch
    // BACK INTO A the current target IS A, so the notice A raised died on entry
    // instead of on exit — invisible either way. Removing the call is what lets
    // 「圖片太大」 raised in A survive to be read in A.
    //
    // Nothing else needed it: the OLD room's rows are drained to their own draft
    // by the staging hook's foreign-landing effect, and the room being entered
    // has no staged rows of its own to dedupe against (that same effect emptied
    // them when it was left).
    const restored = getChatDraft(member.id);
    setDraft(restored?.text ?? "");
    // 🔴 THE TARGET MUST SWAP WITH THE PEER, and since 2026-08-21 the reason is
    // the OPPOSITE of the one that used to be written here. The server had a
    // `sameChatConversation` check and refused a cross-conversation `reply_to`
    // with a 400, so forgetting this line was noisy, visible, and left the draft
    // intact. That check is GONE (owner ruling: quoting sideways into another
    // conversation is the use case). Forgetting this line now SENDS SUCCESSFULLY
    // — a message to the new peer carrying a quote row built from the old
    // conversation, which the server faithfully assembles and shows the
    // recipient. The guard got MORE load-bearing when the refusal went away, not
    // less: do not remove it on the belief that the server still catches this.
    setReplyToId(restored?.replyTo ?? null);
    if (restored && restored.attachments.length > 0) {
      restoreAttachments(restored.attachments);
    }
  }

  // T-8aaa: FIRST-mount attachment restore. The text is lazy-initialized above,
  // but staged attachments live in useAttachmentStaging (starts empty) and have
  // no external lazy init — replay the saved list once, before paint, so a
  // remount shows the images immediately. A peer SWITCH is handled in the block
  // above; this one-shot covers only the initial peer.
  // ⚠️ NOT in the session record: its lifetime is the COMPONENT, not the
  // conversation. In the record it would reset on every switch and replay the
  // mount restore on top of the switch block's own restore, staging the
  // attachments twice.
  const didMountAttachRestoreRef = useRef(false);
  useLayoutEffect(() => {
    if (didMountAttachRestoreRef.current) return;
    didMountAttachRestoreRef.current = true;
    const restored = getChatDraft(member.id);
    if (restored && restored.attachments.length > 0) {
      restoreAttachments(restored.attachments);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // T-8aaa: persist the live draft (text + staged attachments) to the per-peer
  // store on every change, so an unmount (跳頁) leaves the latest draft behind.
  // Because the peer-switch block adjusts draft+attachments during render, the
  // committed values are always consistent with `member.id` here — no stale
  // window. An empty draft deletes the entry (saveChatDraft), giving the
  // "送出 / 手動清空後歸零" behavior for free.
  useEffect(() => {
    saveChatDraft(member.id, {
      text: draft,
      attachments: pendingAttachments,
      replyTo: replyToId ?? undefined,
    });
  }, [member.id, draft, pendingAttachments, replyToId]);

  // 🔴 A FILE CAN FINISH READING WHILE NO COMPOSER EXISTS FOR ITS ROOM, AND THE
  // ONE THAT EXISTS NEXT HAS ALREADY READ THE DRAFT (T-48, R11-2). Pick a big
  // file in A, leave the page, come back to A: the previous `ChatArea` is gone,
  // so its staging hook hands the finished read to `keepAttachmentsWithTheirRoom`
  // — which files it in A's draft. But THIS instance restored A's draft when it
  // mounted, seconds earlier and empty, and nothing re-reads a draft after that.
  // The file was therefore invisible here and doomed: the persist effect above
  // writes this composer's own list back over A's draft on the next keystroke.
  // Measured before this: the file was in the draft, absent from the screen, and
  // gone from both after one keypress.
  //
  // So the store is where a file WAITS and this registry is how it ARRIVES. The
  // callback is looked up through a ref because it is redeclared every render
  // while the registration is per-room; `adoptAttachments` appends and dedups by
  // key, so an arrival that the mount-time restore already covered is a no-op.
  const adoptRef = useRef(adoptAttachments);
  adoptRef.current = adoptAttachments;
  useEffect(() => {
    const adopt = (arriving: PendingAttachment[]) => adoptRef.current(arriving);
    liveComposers.set(member.id, adopt);
    return () => {
      // Identity-checked: on a switch the NEW room's registration may already
      // have replaced this entry, and deleting by key alone would unregister a
      // composer that is very much alive.
      if (liveComposers.get(member.id) === adopt) {
        liveComposers.delete(member.id);
      }
    };
  }, [member.id]);

  // T-e987 compose seed: prefill the composer once with "[<taskNo>] " when the
  // 任務卡 label routes here, but only into an EMPTY draft (never overwrite
  // what the owner is mid-typing). One-shot per distinct seed value.
  useEffect(() => {
    if (!draftSeed) return;
    if (session.seedConsumed === draftSeed) return;
    session.seedConsumed = draftSeed;
    setDraft((cur) => (cur ? cur : draftSeed));
  }, [draftSeed]);

  // ===== Scrollback — 往上捲載入更多 (T-bf82) =====
  //
  // Scrolling near the TOP of the thread loads one older history page and
  // PREPENDS it. The viewport must not jump: we snapshot the scroll geometry
  // (+ the current first message id) before the fetch, and the layout effect
  // below compensates scrollTop by the height the prepend added — before
  // paint, so the owner keeps reading the same row. The anchor's firstId also
  // tells "a prepend really landed" apart from an unrelated (appended) update.
  const NEAR_TOP_PX = 120;

  async function loadOlderAnchored() {
    if (session.loadingOlder || !hasMore) return;
    const el = messagesRef.current;
    if (!el || messages.length === 0 || messagesPeer !== member.id) return;
    session.loadingOlder = true;
    session.prependAnchor = {
      firstId: messages[0].id,
      height: el.scrollHeight,
      top: el.scrollTop,
    };
    try {
      await loadOlder();
    } finally {
      session.loadingOlder = false;
    }
  }

  // Prepend scroll compensation + session-tracker bookkeeping. useLayoutEffect
  // (not useEffect) so the scrollTop fix lands BEFORE paint — no visible jump.
  // Runs before the scroll-position reactor below (layout effects precede
  // passive effects in a commit), so registering the prepended ids into
  // session.prevIds here keeps the reactor's "fresh message" diff honest: loaded
  // HISTORY is not fresh — it must never arm the new-message chip nor
  // re-anchor the unread divider.
  useLayoutEffect(() => {
    const anchor = session.prependAnchor;
    if (!anchor) return;
    if (messagesPeer !== member.id || messages.length === 0) return;
    const idx = messages.findIndex((m) => m.id === anchor.firstId);
    if (idx <= 0) {
      // idx === 0: nothing prepended (yet) — an unrelated append committed
      // while the older page is in flight; keep waiting on the anchor.
      // idx === -1: the anchor row vanished (peer data reset) — drop it.
      if (idx === -1) session.prependAnchor = null;
      return;
    }
    session.prependAnchor = null;
    for (let i = 0; i < idx; i++) session.prevIds.add(messages[i].id);
    const el = messagesRef.current;
    if (el) el.scrollTop = anchor.top + (el.scrollHeight - anchor.height);
    // The one-shot entry positioning (session.initialPositioned) already ran for
    // this conversation — a prepend must never re-run it, and it doesn't:
    // the latch stays untouched here.
  }, [messages, messagesPeer, member.id]);

  // Threshold (px) within which the viewport counts as "at the bottom" for
  // auto-follow and the read watermark.
  const NEAR_BOTTOM_PX = 80;
  // ① A MUCH TIGHTER SLACK, and it is not the same number for a reason: this
  // one answers "is the NEWEST MESSAGE on screen", which is what the arrow is
  // for, while 80px answers "is the owner still following along". Reusing the
  // 80 would hide the arrow with the newest message's bottom 80px cut off. The
  // 4px covers sub-pixel scroll positions only.
  const AT_LATEST_PX = 4;
  function onMessagesScroll() {
    const el = messagesRef.current;
    if (!el) return;
    // Near the TOP → pull one older page (no-op while one is in flight or
    // when the history is exhausted — hasMore=false renders the
    // "已到最早訊息" marker instead).
    if (el.scrollTop < NEAR_TOP_PX && hasMore) {
      void loadOlderAnchored();
    }
    const distance = el.scrollHeight - el.scrollTop - el.clientHeight;
    const nowNearBottom = distance <= NEAR_BOTTOM_PX;
    // Near the BOTTOM of an ANCHOR WINDOW → pull one page FORWARDS (T-48 ③).
    // The exact mirror of the top branch above, and the reason the jump can
    // afford to fetch only two pages: the owner walks out of the window in the
    // direction they are already scrolling, one page at a time, instead of the
    // jump having to guess how much history to drag along. No scroll
    // compensation is needed here — an append grows the box BELOW the viewport,
    // so the row being read does not move. `hasNewer` flips false when the walk
    // reaches the live tail, and the ordinary newest-window refresh resumes.
    if (nowNearBottom && hasNewer) {
      void loadNewer();
    }
    // ① The arrow's condition, and it is a DIFFERENT question from
    // `nowNearBottom`: auto-follow may forgive 80px, but the owner asked for
    // the arrow whenever the newest message is not in the viewport. The newest
    // row is the last content in this box, so "any content still below the
    // fold" IS "the newest row is cut off" — up to the 12px flex gap that
    // follows it, which the slack absorbs.
    setLatestInView(distance <= AT_LATEST_PX);
    // Crossing into the bottom band = the owner has now read to the latest → mark
    // the newest message read (monotonic server-side; safe to fire repeatedly).
    //
    // 🔴 `mayMarkRead` because in an ANCHOR WINDOW the bottom of the BOX is not
    // the bottom of the THREAD — see where it is derived. The forward walk
    // above (`loadNewer`) is what eventually reaches the live tail, and this
    // resumes there.
    if (nowNearBottom && !session.nearBottom && newestTs > 0 && mayMarkRead) {
      void markRead(newestTs);
    }
    // Reaching the bottom means the preview strip's message has been seen →
    // drop it (no-op when already null), and the current unread run is CLOSED —
    // the next unseen inbound starts a new run (divider re-anchors).
    if (nowNearBottom) {
      setNewMsgPreview(null);
      session.unreadRunOpen = false;
    }
    session.nearBottom = nowNearBottom;
  }

  // The newest message ts in the thread — the watermark the owner marks read up
  // to (0 when empty).
  const newestTs = messages.length > 0 ? messages[messages.length - 1].ts : 0;

  // 🔴 T-48 — MAY THE OWNER'S READ WATERMARK BE MOVED RIGHT NOW? Two ways it
  // must not be, and both are the same mistake: stamping "seen" on messages
  // nobody has looked at (owner ruling — mark-read is 「我看過了」, not 「我跳過
  // 來過」).
  //
  //   • `hasNewer` — the thread is an ANCHOR WINDOW from the middle of the
  //     history. `newestTs` is that window's last row and everything between it
  //     and the live tail is unfetched, unseen material.
  //   • a jump still PENDING — arriving through 跳到原訊息 / a kept link mounts
  //     the thread on the NEWEST window first, and the anchor fetch replaces it
  //     a moment later. That first window is on screen for no time at all and
  //     the reader is on their way somewhere else entirely; marking it read
  //     would consume the whole unread run before the jump has even landed,
  //     which is worse than the anchor-window case, not milder.
  //
  // Nothing is lost by waiting — the watermark is monotonic. Walking forward,
  // the 回到最新 arrow, and a jump that finishes (landed OR missed — both consume
  // the latch) each end the block, which is exactly when the owner really is
  // looking at the latest.
  const jumpPending =
    jumpToMsgId !== undefined && session.jumpConsumed !== jumpToMsgId;
  const mayMarkRead = !hasNewer && !jumpPending;

  // 🔴 THE RETRY THE READER CAN ACTUALLY PRESS (T-48). A failed read is the one
  // ending of a jump that is worth trying again, and an ending with no way to
  // try again is just a politer dead end — the same shape as F3, where the
  // fetch latch was spent and nothing could ask for a second attempt.
  //
  // Why a BUTTON and not the silent re-schedule the superseded path uses first:
  // that path retries because something else demonstrably moved (a newer load
  // committed), so the next attempt has a reason to go differently. A dropped
  // connection has no such signal — an automatic retry would fire straight back
  // into the same failure, and a loop of them is exactly what the retry cap
  // exists to prevent. The person watching knows when the office is back; the
  // button hands them that decision.
  //
  // 🔴 AND IT IS THE ONLY WAY BACK FROM *interrupted* TOO (R3-5). That notice
  // used to read 「再點一次連結可以重試」 while the jump latch was already spent
  // and the hash had not changed — no `hashchange`, no re-render, the reactor's
  // top guard returning immediately. The sentence asked the reader to do
  // something that could not work, which is precisely the class of silent lie
  // this ticket exists to delete. Both endings that a retry can change now get
  // the same button.
  //
  // ⚠️ THREE latches are released, and that is the whole of it: `session.jumpFetched`
  // alone would leave the jump CONSUMED (the reactor's top guard returns early
  // and nothing happens), `session.jumpConsumed` alone would leave the fetch marked
  // as already spent, and leaving the auto-retry budget spent would make the
  // button a one-shot on a path whose whole failure mode is losing races.
  function retryJump() {
    if (jumpToMsgId === undefined) return;
    session.jumpFetched = null;
    session.jumpConsumed = null;
    session.autoJumpRetries = 0;
    setJumpNotice(null);
    setJumpRetry((n) => n + 1);
  }

  // ===== T-4e95 quote resolution =====
  //
  // 🔴 THERE IS NO RESOLUTION ANY MORE, AND THAT IS THE WHOLE REDESIGN (owner
  // ruling, 2026-08-21). A reply arrives carrying `replyToChat` — the quoted
  // sender and a server-shortened line of what they said — built by the server
  // on every read, without exception. The row reads that field and stops.
  //
  // What used to be here: the wire carried the quoted ID alone, so this
  // component looked the target up in the loaded window and, failing that, went
  // and fetched it (useQuotedMessages, now deleted). That fetch could fail; a
  // failure was drawn as a placeholder that was sometimes a lie; the lie was
  // repaid on the next inbound SSE event. Each of those was a branch, and all of
  // them paint the SAME PIXELS whether they are right or wrong — which is why
  // the bugs that lived in them survived twenty rounds of review. Do not
  // reintroduce a lookup here however cheap it looks: the value of this shape is
  // not that it is fast, it is that it has exactly one behaviour.
  //
  // `messageById` survives for ONE job, and it is not the quote text: whether
  // the COMPOSER's banner can NAME the person being answered, which is a
  // question about what is loaded right now and can only be answered here.
  //
  // 🔴 IT NO LONGER GATES THE QUOTE ROW'S CONTROL. It did — the row offered the
  // control only when the target was in this map, and back then it was labelled
  // 「跳到原訊息」 because it scrolled rather than opened. The same owner ruling
  // that deleted the resolution deleted that gate too: the control is offered on
  // every reply, is labelled 「看原訊息」, and reads its one message back on click
  // (`quotedMessage.open`, hooks/useQuotedMessageOverlay). The render condition is `m.replyTo && quoted`; it
  // does not consult `messages`.
  const messageById = useMemo(
    () => new Map(messages.map((m) => [m.id, m])),
    [messages],
  );
  // The message the COMPOSER is aiming at, resolved from the loaded window
  // ALONE — no fetch, no fallback, no third state. The owner picks this target
  // by clicking a row that is on screen, so it is in the window by construction.
  // The one path that can miss is a draft restored from an earlier session whose
  // target has since scrolled out; that renders as the same fixed sentence every
  // other unshowable quote does, and SENDING IT STILL WORKS — the server
  // resolves the id, and the sent row comes back with its quote attached.
  const replyQuote = replyToId ? messageById.get(replyToId) : undefined;

  // 🔴 THERE IS NO `locateMessage` ANY MORE, AND THE OTHER JUMP IS NOT IT.
  // The quote row used to scroll the thread to the quoted row when that row
  // happened to be loaded, and show no control when it was not. Owner ruling
  // 2026-08-21 replaced that with 「撈那一則、跳 modal」
  // (hooks/useQuotedMessageOverlay — this row is its only caller since the two
  // cards went back to navigating on 2026-08-29): one behaviour for every reply
  // in THIS thread, no window-dependent affordance, and
  // no scroll — which also retired the "the jump moves the viewport but not the
  // FOCUS, so a keyboard user pressing it saw nothing happen" defect, because
  // there is nothing left to scroll.
  //
  // ⚠️ WHAT SURVIVES, AND MUST: the `jumpToMsgId` reactor below. That is the
  // hash-route jump (#office/chat/<id>/msg/<msgId>), a different entry point
  // with a different job — it lands the thread on a named row on ENTRY — and it
  // owns `highlightMsgId` and the `--located` flash. It never called
  // locateMessage. Deleting one did not touch the other, and
  // `ChatArea.unread-jump.test.tsx` plus the reply-card jump tests pin it.

  // The hash-route jump — declared BEFORE the entry-positioning reactor below so
  // the jump consumes entry positioning first (the divider/bottom scroll must
  // not fight the located message). One-shot per target id; a target outside the
  // loaded recent window falls back to the plain land-at-bottom (honest miss —
  // the thread still opens, and nothing pretends the target was found).
  //
  // ⚠️ Its callers are the 請示 page's 跳到原訊息, the inline task card's
  // 在聊天室回覆, and any URL somebody kept (bookmark, pasted link, restored
  // tab) — see the prop's note for the miss this can still land on.
  useEffect(() => {
    if (!jumpToMsgId) return;
    // 🔴 AN EMPTY THREAD IS THE NORMAL ENTRY NOW, NOT A REASON TO WAIT (T-48).
    // With the anchor named at subscription time useChat fetches NOTHING on
    // entry, so waiting for `messages.length > 0` here would wait forever and
    // the room would never fill. An empty thread has nothing in the DOM, which
    // sends this straight down the fetch branch below — which is the point:
    // the FIRST request the room makes is the window around the target.
    if (messagesPeer !== member.id) return;
    if (session.jumpConsumed === jumpToMsgId) return;
    // Raw interpolation matches the chip-jump selector above — message ids
    // are server-minted (`c-<hex>`), never arbitrary strings.
    const el = messagesRef.current?.querySelector(
      `[data-msg-id="${jumpToMsgId}"]`,
    );
    // 🔴 A TARGET OUTSIDE THE LOADED WINDOW IS NOW FETCHED, NOT GUESSED AT
    // (T-48 ③, owner: 「跳到原訊息…都可以正確定位到該訊息」). This branch used
    // to be `endRef.scrollIntoView()` — the thread opened at the bottom and
    // NOTHING said the target had not been found, which is indistinguishable
    // from a successful jump to a recent message. `loadAround` pages OUTWARDS
    // from the id (one window each way, never the whole history); when it
    // lands, `messages` changes and this effect runs again with the row in the
    // DOM. The one-shot latch is NOT consumed yet — consuming it here would
    // eat the jump the fetch is about to make possible.
    if (!el) {
      if (session.jumpFetched !== jumpToMsgId) {
        session.jumpFetched = jumpToMsgId;
        // The jump owns the viewport FROM THE MOMENT IT STARTS FETCHING, not
        // from the moment it lands. Without these three the thread spends the
        // in-flight window doing its ordinary entry positioning — landing at
        // the bottom, i.e. the exact wrong place, and then being scrolled again
        // when the anchor window arrives. Registering the current ids as
        // already-seen also keeps that in-flight commit from mistaking the
        // thread it is replacing for a burst of new arrivals.
        session.initialPositioned = true;
        session.prevIds = new Set(messages.map((m) => m.id));
        session.nearBottom = false;
        const firedFor = session;
        void loadAround(jumpToMsgId).then((outcome) => {
          // 🔴 THE OWNER MAY HAVE LEFT WHILE THE PAIR WAS IN THE AIR (T-48,
          // R5-1). Two of the three endings below reach the OUTSIDE world —
          // `setJumpNotice` paints a banner and `endRef.scrollIntoView()` moves
          // a viewport — and neither is addressed to a conversation, they are
          // addressed to whatever room is on screen. Without this line a 502 on
          // A's anchor pair, answered after the owner clicked B in the roster,
          // hung A's 「讀不到那則訊息」 banner in B's room (with a retry button
          // that does nothing, because B has no jump target) and scrolled B to
          // the bottom — which, if B was itself entered AT AN ANCHOR, is the
          // exact intermediate frame this ticket exists to delete, arriving
          // from the previous conversation.
          //
          // The record writes below need no guard for the same reason the
          // latches do not: `session` is the one this render captured, so a
          // late write lands in an orphan nobody reads. The guard is here for
          // what a captured record cannot cover — the DOM and the screen.
          //
          // 🔴 IT COMPARES RECORDS, NOT PEER IDS (R6-1). Asking "is B on screen
          // now?" let the SAME conversation's next visit through: enter A at an
          // anchor, the pair 502s in the air, switch to B and back to A, and
          // this callback painted the first visit's 「讀不到那則訊息」 banner
          // (retry button and all — dead, because this visit has no jump
          // target) onto the second visit, and scrolled it to the bottom.
          if (visitRef.current !== firedFor) return;
          if (outcome === "found") return;
          if (
            outcome === "superseded" &&
            session.autoJumpRetries < MAX_JUMP_RETRIES
          ) {
            // 🔴 NOT A MISS (T-48, F3). Another load committed on top of ours,
            // so our window was dropped to keep the thread in order — the
            // message is still there. Saying 「找不到那則訊息」 here accused the
            // server of losing a message it still has, and because the fetch
            // latch had already been spent there was no retry and no button to
            // ask for one. Re-arm and go round again; if the owner has mean-
            // while asked for the live tail (回到最新 spends the jump latch),
            // the guard at the top of this effect ends it instead.
            session.autoJumpRetries += 1;
            session.jumpFetched = null;
            setJumpRetry((n) => n + 1);
            return;
          }
          // Genuinely unreachable (the id names nothing, the id belongs to
          // ANOTHER conversation — both windows answer 200 + empty — or the
          // request failed). Fall back to the bottom — the thread still opens —
          // and SAY SO ON SCREEN. The console line stays for the developer; the
          // notice is what stops the fallback reading as a successful jump.
          console.warn(
            `ChatArea: jump target ${jumpToMsgId} could not be located`,
            outcome,
          );
          // Three different facts, three different sentences. Collapsing any two
          // of them is the defect this ticket exists to remove, one layer up:
          // 「已經被清掉了」 tells a reader whose message is behind a 502 to stop
          // trying.
          setJumpNotice(
            outcome === "superseded"
              ? "interrupted"
              : outcome === "unreachable"
                ? "unreachable"
                : "missing",
          );
          session.jumpConsumed = jumpToMsgId;
          session.nearBottom = true;
          // ⚠️ ANCHOR-FIRST ENTRY LEAVES THE ROOM EMPTY UNTIL SOMEBODY FILLS IT
          // (T-48). On this path nobody has: useChat skipped its entry load
          // because an anchor was named, and the anchor is not there. "Fall
          // back to the bottom" therefore has to fetch the bottom first, or the
          // owner is left staring at an empty conversation with a notice on it.
          // Only when the thread really is empty — a miss with history already
          // loaded still just lands where it always did.
          if (messages.length === 0) void resetToLatest();
          endRef.current?.scrollIntoView();
        });
      }
      return;
    }
    session.jumpConsumed = jumpToMsgId;
    setJumpNotice(null);
    // The jump owns the initial viewport — mark entry positioning done.
    session.initialPositioned = true;
    session.prevIds = new Set(messages.map((m) => m.id));
    {
      el.scrollIntoView({ block: "center" });
      // Located mid-thread → not at the bottom; a later arrival must not yank.
      session.nearBottom = false;
      // …and the newest message is somewhere below, so the arrow belongs here.
      setLatestInView(false);
      setHighlightMsgId(jumpToMsgId);
      // Async content above the target (images decoding to their real height,
      // inline reply cards refetching) reflows AFTER this paint-time scroll and
      // shoves the centered row off-screen — worst on short mobile viewports.
      // A ResizeObserver on the scroll viewport never fires (its own box is
      // clamped by flex + overflow); watch the in-flow content rows, whose
      // height actually grows, and re-center until the highlight window closes.
      const scroller = messagesRef.current;
      if (scroller) {
        const ro = new ResizeObserver(() =>
          el.scrollIntoView({ block: "center" }),
        );
        for (const row of Array.from(scroller.children)) ro.observe(row);
        const settle = window.setTimeout(() => ro.disconnect(), 2600);
        return () => {
          window.clearTimeout(settle);
          ro.disconnect();
        };
      }
    }
  }, [
    jumpToMsgId,
    messages,
    messagesPeer,
    member.id,
    loadAround,
    resetToLatest,
    jumpRetry,
  ]);

  // The jump highlight is a transient flash — clear it after the CSS pulse so
  // the row returns to the normal thread look.
  useEffect(() => {
    if (!highlightMsgId) return;
    const timer = window.setTimeout(() => setHighlightMsgId(null), 2600);
    return () => window.clearTimeout(timer);
  }, [highlightMsgId]);

  // The ONE scroll-position reactor. First load of a conversation → entry
  // positioning (② first unread when entered with a badge, else the existing
  // land-at-bottom). Subsequent updates → the existing auto-follow when near
  // the bottom, else (scrolled up) arm the ① new-message chip on the first
  // fresh inbound message.
  useEffect(() => {
    // STALE-PEER GUARD (divider-latch fix): on a peer switch this effect fires
    // for the render where `member.id` is already the NEW peer but `messages`
    // is still the PREVIOUS peer's thread — useChat clears the thread in its
    // own effect, ONE COMMIT LATER. Latching entry positioning on that stale
    // commit consumed the one-shot (session.initialPositioned) against the wrong
    // thread, so the "以下是未讀訊息" divider never rendered when entering an
    // unread room FROM a non-empty thread. `messagesPeer` is set TOGETHER with
    // `messages` (single state in useChat), so it is the honest owner of the
    // array — do nothing until the thread really belongs to this peer.
    if (messagesPeer !== member.id) return;
    if (messages.length === 0) return;
    if (!session.initialPositioned) {
      session.initialPositioned = true;
      session.prevIds = new Set(messages.map((m) => m.id));
      const count = session.initialUnread;
      // Unread = peer→owner only (matches the server's unread_counts rule:
      // recipient == reader; inter-agent traffic never counts).
      const inbound =
        count > 0
          ? messages.filter((m) => m.from === member.id && m.to === OWNER_ID)
          : [];
      const first = inbound.slice(-count)[0];
      if (first) {
        // Positioning happens in the firstUnreadId effect below, AFTER the
        // divider renders (it is the scroll target). Until the measurement
        // there says otherwise, we are NOT at the bottom.
        session.nearBottom = false;
        session.unreadRunOpen = true;
        session.entryScrollPending = true;
        setFirstUnreadId(first.id);
      } else {
        endRef.current?.scrollIntoView();
      }
      return;
    }
    const prev = session.prevIds;
    const fresh = messages.filter((m) => !prev.has(m.id));
    session.prevIds = new Set(messages.map((m) => m.id));
    if (session.nearBottom) {
      endRef.current?.scrollIntoView();
      // Following the bottom = everything is being seen; any strip up is stale
      // (e.g. the owner just sent a reply, which force-follows), the newest
      // message is on screen, and the unread run — if one was open — is being
      // read right now.
      setNewMsgPreview(null);
      setLatestInView(true);
      session.unreadRunOpen = false;
      return;
    }
    // Scrolled up + new messages addressed to the owner. Two different anchors
    // come out of the SAME arrival batch and they are deliberately different
    // ends of it:
    //   • the preview strip shows the LAST one — it is a preview of what just
    //     came in, and it replaces whatever it was showing (never stacks);
    //   • the 「以下是未讀訊息」 divider anchors at the FIRST one — it marks
    //     where the unread block STARTS, and it is what stays behind after the
    //     jump lands the reader at the end of that block.
    const inboundNew = fresh.filter(
      (m) => m.to === OWNER_ID && m.from !== OWNER_ID,
    );
    if (inboundNew.length > 0) {
      const latest = inboundNew[inboundNew.length - 1];
      setNewMsgPreview({ id: latest.id, from: latest.from, body: latest.body });
      // Appended below the fold ⇒ the newest message is, by construction, not
      // in the viewport. Say so without waiting for a scroll event that may
      // never come (the owner is reading, not scrolling).
      setLatestInView(false);
      // Strip/divider alignment (owner bug): if no unread run is open (the
      // owner had seen everything up to now), this first unseen inbound STARTS
      // one → anchor the divider here. If a run is already open (e.g. the entry
      // divider's tail was never read down to), the arrival extends the SAME
      // run — the divider stays put.
      if (!session.unreadRunOpen) {
        session.unreadRunOpen = true;
        setFirstUnreadId(inboundNew[0].id);
      }
    }
  }, [messages, messagesPeer, member.id]);

  // ② entry scroll: once the unread divider is in the DOM, pin it to the top of
  // the viewport, then measure honestly whether that landed us at the bottom
  // anyway (short thread) so auto-follow keeps working there.
  useEffect(() => {
    if (!firstUnreadId) return;
    // ONLY the entry positioning scrolls here. A chip-driven divider re-anchor
    // (in-conversation arrival while scrolled up) must not move the viewport —
    // the owner is reading history; the chip is their opt-in jump.
    if (!session.entryScrollPending) return;
    session.entryScrollPending = false;
    const box = messagesRef.current;
    if (!box) return;
    const divider =
      box.querySelector(".chat__unread-divider") ??
      box.querySelector(`[data-msg-id="${firstUnreadId}"]`);
    // The divider is the actual unread boundary.  Keeping older context above
    // it can push the first unread row outside a compact chat viewport.
    divider?.scrollIntoView({ block: "start" });
    const distance = box.scrollHeight - box.scrollTop - box.clientHeight;
    session.nearBottom = distance <= NEAR_BOTTOM_PX;
    // Landing on the divider usually leaves the newest message below the fold —
    // that is the whole point of landing there — so the arrow must be able to
    // come up immediately, without waiting for the owner to scroll first.
    setLatestInView(distance <= AT_LATEST_PX);
    // NOTE: the run deliberately stays OPEN even when a short thread lands at
    // the bottom here — every real "the owner saw it" path (a bottom-crossing
    // scroll, or an at-bottom auto-follow) closes it; closing on this
    // layout-dependent measurement would misfire under test/jsdom geometry.
  }, [firstUnreadId]);

  // ③ THE ONE JUMP BEHIND BOTH BOTTOM AFFORDANCES: go to the LATEST message.
  //
  // 🔴 IT USED TO GO TO THE FIRST UNSEEN ONE, and that was the bug (reproduced
  // in the isolated environment: ten messages injected, the jump landed on
  // message 1 with five still below the fold). The first-unseen position is
  // still marked — by the 「以下是未讀訊息」 divider, which stays where it is —
  // so nothing was lost by moving the landing to the end of the block.
  //
  // The landing is CORRECTED after the layout settles (lib/scrollToLatest):
  // images above the target decode to their real height after this frame and
  // shove the row straight back out of view.
  // ⚠️ NOT in the session record, and the reason is narrower than it looks
  // (R5-5 corrected a wrong one that used to stand here). Nothing cancels this
  // on a conversation switch — the peer block above has never called it — so
  // "keeping the handle buys us a cancel on switch" was simply false. The real
  // reason is the UNMOUNT cleanup below: in the record, that cleanup would read
  // the NEW conversation's `null` and leave the previous one's ResizeObserver
  // and 2.6s timer alive past unmount. The lifetime that matters here is the
  // COMPONENT's, not the conversation's.
  // (What a switch leaves running is harmless: `scrollToLatest` captured a row
  // that is detached by then, so `scrollIntoView` is a no-op and the observed
  // children are gone. The two places that start a new correction cancel the
  // previous one first, which is what actually keeps them from stacking.)
  const jumpSettleRef = useRef<(() => void) | null>(null);
  function jumpToLatest() {
    const el = messagesRef.current;
    if (!el) return;
    // The strip's message is exactly what we are going to look at.
    setNewMsgPreview(null);
    setLatestInView(true);
    session.nearBottom = true;
    session.unreadRunOpen = false;
    // 🔴 THE ARROW / THE PREVIEW STRIP ENDS AN IN-FLIGHT JUMP (T-48). Both mean
    // "take me to the newest message", said by the owner, and they are the one
    // thing allowed to overtake the anchor fetch. Spending the jump latch here
    // is what makes the overtake DELIBERATE rather than a race: `loadAround`
    // comes back "superseded", the reactor's own top guard ends it without a
    // retry and without a notice, and `mayMarkRead` opens because the owner
    // really is on their way to the live tail.
    if (jumpToMsgId !== undefined) session.jumpConsumed = jumpToMsgId;
    // 🔴 SCROLLING IS NOT ENOUGH WHEN THE THREAD IS AN ANCHOR WINDOW (T-48 ③).
    // `scrollToLatest` lands on the last row IN THE DOM; after a jump to an old
    // message that row is nowhere near the newest one, so the arrow would move
    // the viewport and leave the owner still in the past — a fresh instance of
    // exactly the lie this ticket exists to remove. Fetch the live window first
    // and scroll when it lands.
    if (hasNewer) {
      session.pendingLatestScroll = true;
      void resetToLatest();
      return;
    }
    jumpSettleRef.current?.();
    jumpSettleRef.current = scrollToLatest(el);
  }
  // Declared AFTER the scroll-position reactor above so it runs last in the
  // commit: the reactor's own at-bottom auto-follow does a plain
  // `scrollIntoView`, and this replaces it with the settling landing.
  useEffect(() => {
    if (!session.pendingLatestScroll) return;
    if (messagesPeer !== member.id || messages.length === 0) return;
    session.pendingLatestScroll = false;
    const el = messagesRef.current;
    if (!el) return;
    jumpSettleRef.current?.();
    jumpSettleRef.current = scrollToLatest(el);
    setLatestInView(true);
    session.nearBottom = true;
  }, [messages, messagesPeer, member.id]);
  // A pending correction must not outlive the conversation it was aiming at.
  useEffect(() => () => jumpSettleRef.current?.(), []);

  // ①② WHICH bottom affordance is on screen — at most ONE, ever (owner: the
  // preview strip 讓位 rule). Derived in one place so the exclusion is a single
  // fact rather than two booleans that have to agree; see the module's note for
  // why writing `!latestInView` inline is the mistake this shape prevents.
  const bottomAffordance = chatBottomAffordance({
    latestInView,
    hasNewMsgPreview: newMsgPreview !== null,
    windowHasNewer: hasNewer,
  });

  // OWNER read receipt: entering the conversation (or a new message landing while
  // the owner is at the bottom) means the owner has SEEN up to the newest message
  // → mark it read. markRead is monotonic server-side (a stale ts is a no-op), so
  // firing on every settle is safe. If the owner has scrolled UP to read history
  // we still mark read: the newest message is loaded and being viewed on entry.
  //
  // Gated THREE ways:
  //   • `windowActive` — "seen" requires the owner to actually be looking. A
  //     message landing while the window is backgrounded must NOT be consumed;
  //     the flip back to active re-runs this effect, so everything accumulated
  //     is marked read exactly when the owner returns.
  //   • `messagesPeer === member.id` — on a peer switch `newestTs` still comes
  //     from the PREVIOUS peer's thread for one commit; firing then would stamp
  //     the NEW peer's watermark with the OLD thread's timestamp.
  //   • 🔴 `mayMarkRead` — the thread must be the LIVE TAIL and no jump may be
  //     in flight (T-48; see where it is derived for both halves and why).
  useEffect(() => {
    if (!windowActive) return;
    if (messagesPeer !== member.id) return;
    if (!mayMarkRead) return;
    if (newestTs > 0) void markRead(newestTs);
  }, [newestTs, markRead, windowActive, messagesPeer, member.id, mayMarkRead]);

  // Esc handling for the full-view overlay lives inside MarkdownPreviewOverlay.

  // Drag-drop: dropping files anywhere on the chat window stages them —
  // unless the composer is LOCKED (M2-4: an offline member can't receive a
  // reply, so nothing may be staged while locked; paste/pick are already
  // unreachable because the locked composer renders no input at all).
  function onDragOver(e: React.DragEvent<HTMLDivElement>) {
    if (composerLocked) return;
    if (e.dataTransfer.types.includes("Files")) e.preventDefault();
  }
  function onDrop(e: React.DragEvent<HTMLDivElement>) {
    if (composerLocked) return;
    const files = Array.from(e.dataTransfer.files ?? []);
    if (files.length === 0) return;
    e.preventDefault();
    stageFiles(files);
  }

  async function submit() {
    if (!canSend) return;
    // Sending my own message always scrolls to the bottom, even if I had scrolled
    // up to read history — my just-sent message should be visible.
    session.nearBottom = true;
    // Snapshot the composer, then OPTIMISTICALLY clear it BEFORE the server
    // round-trip. `send()` awaits the POST + a refetch (seconds); if we only
    // cleared after that await, the draft stays populated meanwhile and a second
    // Enter re-fires submit() on the SAME draft → a duplicate send. Clearing up
    // front makes canSend false immediately, so the repeat Enter is a no-op. On
    // failure we restore the snapshot below so the user's message is never
    // silently swallowed.
    const draftSnapshot = draft;
    const attachmentsSnapshot = pendingAttachments;
    const replyToSnapshot = replyToId;
    // 🔴 WHICH ROOM THIS SEND BELONGS TO. The restore below runs after an await,
    // and the owner can switch peers during it — this component is REUSED across
    // peers, so the restore landed in whoever was on screen when the failure came
    // back, and the save effect then persisted it into THAT peer's draft. The
    // reply target makes it worse than untidy, and worse than it used to be: the
    // server's `sameChatConversation` refusal was deleted on 2026-08-21, so a
    // target from another room is no longer 400'd — it is accepted, and the new
    // room's message goes out quoting a sentence from a conversation it has
    // nothing to do with, which the recipient then reads as context. The failure
    // mode flipped from a visible refusal to a silent mis-send.
    const sendPeer = member.id;
    // The draft above is filed under the PEER (storage is peer-keyed); putting
    // it back on SCREEN is a question about the visit — a re-entry to the same
    // peer has already restored that draft from storage for itself.
    const sendVisit = session;
    // ALL staged attachments ride the SAME message, in staged order.
    const attachments = attachmentsSnapshot.map((a) => ({
      dataB64: a.dataUri,
      // Omit an empty filename so the backend applies its default (pasted
      // images); a real picked filename passes through.
      ...(a.filename ? { filename: a.filename } : {}),
      mime: a.mime,
    }));
    setDraft("");
    clearAttachments();
    // Cleared with the rest of the composer: a reply target that survived its
    // own send would silently attach itself to the NEXT message too.
    setReplyToId(null);
    try {
      await send(
        draftSnapshot,
        attachments.length > 0 ? attachments : undefined,
        replyToSnapshot ?? undefined,
      );
    } catch (e) {
      console.warn("ChatArea: send failed, restoring composer", e);
      // 🔴 RESTORE INTO THE ROOM IT WAS TYPED IN — NOT the one on screen, and
      // NOT nowhere. The first version of this guard was a bare `return` on the
      // reasoning that "that room's draft still holds it". IT DOES NOT: the
      // optimistic clear at the top of submit() runs while `member.id` is still
      // the sending room, so the save effect calls saveChatDraft(sendPeer, {all
      // empty}) — and an all-empty draft is DELETED, not stored. So the bare
      // return traded "restored into the wrong room" (ugly, visible, and the
      // words are still on screen) for "text, attachments AND reply target
      // silently gone for good, with only a console.warn". That is worse, and
      // it is exactly what the guard was added to prevent.
      //
      // Writing to the store rather than to state also covers the case where
      // this component is gone entirely (跳頁 mid-flight): setState on an
      // unmounted component discards the content just as quietly.
      //
      // FIELD BY FIELD, which is the rule the state restores below already use:
      // fill only what the room does not already hold. The first version of
      // this write was all-or-nothing on the whole draft, and a reviewer found
      // the gap that opens: go back to that room, stage one image and type
      // nothing, and the room is no longer "empty" — so the whole write was
      // skipped and the failed message's TEXT and reply target went with it.
      //
      // What this still cannot save: if the owner has retyped in that room,
      // their words win and the failed message's words are gone. Two texts
      // cannot occupy one composer, and theirs is the one they can see. Said
      // out loud rather than left to be discovered.
      const stored = getChatDraft(sendPeer);
      saveChatDraft(sendPeer, {
        text: stored && stored.text ? stored.text : draftSnapshot,
        attachments:
          stored && stored.attachments.length > 0
            ? stored.attachments
            : attachmentsSnapshot,
        replyTo:
          stored && stored.replyTo
            ? stored.replyTo
            : (replyToSnapshot ?? undefined),
      });
      // Not this room any more → the words are back where they were typed, and
      // putting them on screen here would put one room's words, and one room's
      // reply target, into another.
      if (visitRef.current !== sendVisit) return;
      // Restore the user's unsent content so it isn't silently lost. Only put
      // back what the user hasn't already retyped/restaged — if they started a
      // new draft while the send was in flight, don't clobber it.
      setDraft((cur) => (cur ? cur : draftSnapshot));
      restoreAttachments(attachmentsSnapshot);
      // Same rule as the text: put the target back only if the owner has not
      // already aimed at something else while the send was in flight.
      setReplyToId((cur) => (cur ? cur : replyToSnapshot));
    }
  }

  function onKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    // The send decision (IME gate, mobile-newline, Shift+Enter) is the shared
    // enterShouldSend rule so all three composers stay in lockstep. When it
    // returns false on a mobile Enter we deliberately do NOT preventDefault, so
    // the textarea inserts a native newline.
    if (enterShouldSend(e, { isMobile, composing: isComposingRef.current })) {
      e.preventDefault();
      void submit();
    }
  }

  // Render ONE message row (the LINE-style outgoing/incoming bubble). Extracted so
  // both the normal stream and an expanded inter-agent group render identically.
  // Incoming rows label the bubble with the message's TRUE sender (`nameOf(m.from)`)
  // — critical for inter-agent messages, where the sender is not the window's peer.
  function renderMessage(m: ChatMessage) {
    const mine = m.from === OWNER_ID;
    // Sender label. When the RECIPIENT is not the owner (an inter-agent message,
    // either direction: Mira→Kye or Kye→Mira) the sender name alone is ambiguous
    // — members message DIFFERENT agents, so the label spells out the direction:
    // "Mira → Kye". A message addressed to the owner keeps the plain sender name
    // (the recipient is implicit — it's this thread's owner side). Names resolve
    // through the roster (`nameOf`), falling back to the raw id — never blank.
    const senderLabel =
      m.to !== OWNER_ID ? directionLabel(m.from, m.to) : nameOf(m.from);
    // Per-message read state (LINE-style): every own message the peer's real
    // last-read watermark covers shows its own "已讀". Honest — driven only by a
    // recorded watermark, never fabricated.
    // The watermark is asked for THIS room by name — there is no bare number to
    // read (see useChat's PeerLastRead). A watermark still in flight for another
    // room answers 0, which draws no tick, rather than lighting one off somebody
    // else's reading (R8-2).
    const read = mine && peerLastRead.tsFor(member.id) >= m.ts;
    // ONE bubble per message (owner feedback): text and attachments share the
    // SAME bubble container — text on top, attachments stacked below — one
    // rounded surface, one background, so a text+attachment message reads as a
    // single message instead of two disconnected blocks. An attachment-only
    // message is the same single bubble (just no text block). The side meta
    // (已讀/time) hangs off this whole bubble via chat__msg-line below.
    //
    // B3: a message carrying a reply-card link renders the CARD as its bubble
    // (spec §3: 請示以卡片形式直接出現在訊息串中，無額外橫幅) — the card
    // itself fetches its full shape and owns the answer / 重新決定 flow.
    // T-4e95 ① the QUOTE LINE — a thin row above the bubble saying which
    // message this one answers. It reads `m.replyToChat` and NOTHING ELSE: the
    // server built that snapshot on this very read, so there is nothing to look
    // up and nothing that can still be pending.
    //
    const quoted = m.replyToChat ?? null;
    // 🔴 WHO SAID IT **AND WHO THEY SAID IT TO**. `from` alone reads as though
    // the quoted line had been said in this thread, and since 2026-08-21 that is
    // exactly the case it gets wrong: the owner may quote a line two OTHER
    // members exchanged in order to step into it, and the quote row then named a
    // sender while silently implying the wrong listener.
    //
    // The recipient is the QUOTED MESSAGE's own (`quoted.to`, server-projected
    // on this very read) — NEVER this window's peer, which is the plausible
    // wrong answer and is wrong precisely when the quote crosses conversations.
    //
    // There is no third rendering when a name does not resolve: `nameOf` already
    // falls back to the raw id, so both halves always have characters to print.
    const quoteWho = quoted ? directionLabel(quoted.from, quoted.to) : "";
    // 🔴 TWO OUTCOMES, NO THIRD. Either the server sent the snapshot or the
    // original is gone — there is no "not yet", because nothing is in flight.
    // The gone sentence is FIXED: not retried, not refreshed, and not revisited
    // when the next event lands.
    //
    // `quoted.content` may legitimately be "" (the original carried only
    // attachments). That renders as a named quote with an empty line, which is
    // the truth; it must NOT be folded into the gone sentence, because "there
    // was nothing to quote" and "there is nothing to quote FROM" are different
    // facts about the conversation.
    const quoteText = quoted ? quoted.content : t.chat.replyQuoteGone;
    const quoteLine = !m.replyTo ? null : (
      /* 🔴 THE ONE THING THIS ROW EXISTS TO SAY HAS TO REACH THE A11Y TREE TOO.
       * Measured in a real browser on a real <ChatArea>: as a bare <div> this
       * row linearised into the reply as "Mira. Mira. 他說的. 跳到原訊息.
       * 我說的" — role null, no name. (That transcript is verbatim from the
       * measurement; the button said 「跳到原訊息」 then and says 「看原訊息」
       * now. The shape is the point, not the string.) A screen-reader user
       * could not tell
       * which sentence is the quotation and which one this person is saying
       * now, which is the whole feature. `.chat__msg-quote` is the only place
       * in this frontend that embeds someone else's sentence inside another
       * person's message, so the gap is this feature's own, not the app's.
       *
       * role="blockquote" + aria-label, NOT a visually-hidden prefix: this repo
       * has no sr-only utility (MemberCard.presence-a11y.test.tsx says so in as
       * many words), and inventing one for a single row would be a new global
       * primitive smuggled in under a quote line. The label names the quoted
       * sender when we have resolved one and stays generic when we have not —
       * the same "no quote, no name" rule the banner and `quoteWho` already
       * follow. */
      <div
        className="chat__msg-quote"
        data-testid="msg-quote"
        role="blockquote"
        aria-label={
          quoteWho ? t.chat.replyQuoteRoleWho(quoteWho) : t.chat.replyQuoteRole
        }
      >
        {/* Decorative twin of the label above it: the row already SAYS it is a
         * quote through its aria-label, so an unnamed <img> node in the tree
         * beside it is pure noise. Only this one is hidden — the rest of the
         * app's icons are a separate, pre-existing question. */}
        {/* 🔴 LINE 1 — WHO SAID IT TO WHOM, and the control. Owner ruling
         * 2026-08-22 (「換成兩行？ 一行是誰跟誰說話 一行是內容？」): the row is
         * two lines, so 「→ 收件者」 and the quoted sentence stop competing for
         * the same horizontal space. Before that ruling they shared one line and
         * the recipient half was pure loss for the excerpt: measured in the
         * running app at vw=721 (pane 347px), English, a 5-character sender —
         * `.chat__msg-quote__who` 101px, `.chat__msg-quote__body` 18px, 3 of 61
         * characters left, and 0 on the CI runner's fonts. The addressee is not
         * optional (it is what the field exists for), so the line is what gave. */}
        <div className="chat__msg-quote__head">
          <ReplyIcon
            size={11}
            className="chat__msg-quote__icon"
            aria-hidden="true"
          />
          {quoteWho && <span className="chat__msg-quote__who">{quoteWho}</span>}
          {/* The control is its own element, the way 查看任務詳情 is on a
           * task-derived ask (ReplyCardTaskRef) — owner 2026-08-20.
           *
           * 🔴 IT IS OFFERED FOR EVERY REPLY, UNCONDITIONALLY (owner ruling
           * 2026-08-21: 「全部統一就撈那一則顯示出來就好」). It used to appear only
           * when the quoted row was already in the loaded window, on the argument
           * that an affordance which scrolls nowhere is worse than none — true of
           * a control that SCROLLS. This one does not scroll: it reads that one
           * message back from the server and opens it in the full-view overlay,
           * so it works identically for a quote from ten seconds ago and one from
           * ten thousand messages ago. The window-membership question is gone, and
           * with it the row's only piece of local, disagreeable state.
           *
           * The row still shows the SERVER's 60-rune excerpt; the overlay shows
           * the whole body. Nothing here re-cuts anything.
           *
           * ⚠️ ONE CONDITION SURVIVES, AND IT IS NOT THE WINDOW ONE. `quoted` —
           * the server's snapshot — must be present. When it is absent this row is
           * printing 「這則訊息已不存在」, which is the server's own answer from
           * THIS read: the original is gone. Offering a control there would be
           * offering to open a message we have just told the reader does not
           * exist, and pressing it could only ever produce 「拿不到這則訊息」 one
           * line below. That is not the window check coming back through a side
           * door — the window is never consulted — it is the row declining to
           * contradict itself.
           *
           * 🔴 THE LABEL IS ITS OWN ELEMENT so it can be the thing that
           * DISAPPEARS. It is not trimmed and it never was made trimmable in the
           * end: nothing in office.css can ellipsise it — the button is
           * `flex: none` with `white-space: nowrap`, and the label's ONLY rule is
           * `display: none` inside `@container chat-pane (max-width: 520px)`. So
           * on a narrow pane the whole label goes and the arrow is what is left;
           * on a wide one the label renders whole. Whole or absent, never cut.
           *
           * The control used to keep its intrinsic width on the reasoning that a
           * cut 跳到原訊息 helps nobody — true of the Chinese control, which was
           * 69px. The English one at the time was "Go to the original message" at
           * 154px (both figures are the WHOLE BUTTON: label + 2px gap + 12px
           * chevron). Today the labels read 「看原訊息」 / "View the original
           * message" — `d7752781` renamed them with the behaviour — and the
           * English control measures ~151px, so the pressure is unchanged. A
           * control that cannot give way does not stay politely inside the
           * bubble: it
           * runs past the edge and under the corner buttons, which are absolutely
           * positioned and therefore painted on top of it. Measured against the
           * running app: it fails at the narrow end in BOTH languages, and again
           * just past the two-column breakpoint on an INCOMING bubble WITH A BODY
           * (`!mine && m.body` → `--acts2`) — that one reserves 56px of corner
           * where your own reserves 32. Two conditions, not one: `!mine &&
           * m.body` is what widens the corner, and `m.replyTo` is what puts a
           * quote row there to overflow. The worst case is both together. Two earlier versions of this note quoted exact ranges;
           * both were wrong, because the range moves with the bubble kind, the
           * language and the display name. The guard holds the numbers.
           * Dropping the whole label and keeping its arrow beats a control hidden
           * under another control. */}
          {m.replyTo && quoted && (
            <button
              type="button"
              className="chat__msg-quote__jump"
              data-testid="msg-quote-jump"
              /* 🔴 THE NAME CANNOT RIDE ON THE VISIBLE LABEL. That label is the
               * first thing this row gives up when the PANE runs short (see the
               * note above), and it does not shrink on the way out — below 520px
               * of pane it is `display: none` outright. A name riding on it would
               * not degrade, it would VANISH, leaving a button whose only content
               * is a decorative chevron and whose accessible name is the empty
               * string. Naming the control explicitly is what keeps it named in
               * the half of the width range where the label is not rendered at
               * all. */
              aria-label={t.chat.replyQuoteJump}
              title={t.chat.replyQuoteJump}
              onClick={() => void quotedMessage.open(m.replyTo as string)}
            >
              <span className="chat__msg-quote__jump-label">
                {t.chat.replyQuoteJump}
              </span>
              <ChevronRightIcon
                size={12}
                className="chat__msg-quote__jump-chevron"
              />
            </button>
          )}
          {/* The failure sentence comes from the hook, so it lands beside the
           * button that was pressed, and NEVER over the quote line, whose
           * sentence is a claim about whether the original EXISTS. (It used to
           * be shared with the 請示 page and the inline task card; those two
           * navigate again since 2026-08-29 and have no fetch to fail.) */}
          {quotedMessage.failureNotice(m.replyTo as string)}
        </div>
        {/* 🔴 LINE 2 — THE SENTENCE, WITH THE WHOLE ROW TO ITSELF. This is the
         * only thing on the row that says WHAT is being answered, and since the
         * two-line split nothing above it can take its width: it starts at the
         * row's left edge and runs to the right edge, still clipped to one line
         * (a quotation is not this message's to grow). */}
        <span className="chat__msg-quote__body" title={quoteText}>
          {quoteText}
        </span>
      </div>
    );

    // T-4e95 ② the REPLY ENTRY. Owner 2026-08-20 moved it INTO the bubble's
    // corner, beside 放大閱讀: out on the row it read as something belonging to
    // the thread rather than to this message.
    //
    // The reason it started on the row is still true and had to be solved
    // rather than argued with — 放大閱讀's corner exists ONLY on incoming text
    // bubbles, and the AC is 每一則. So the corner is now a SHARED ACTION SLOT
    // that every bubble reserves (own messages and attachment-only bubbles
    // included), holding one or two controls.
    //
    // 🔴 THE ONE SHAPE THAT KEEPS THE ROW ENTRY is a reply-card message: its
    // bubble is replaced by <ChatReplyCard>, a full-width surface with its own
    // header controls, and hanging a floating action over it would collide with
    // them. Stated here rather than silently: card rows are the exception.
    //
    // 🔴 OFFERED ON EVERY ROW IN THE WINDOW, INCLUDING THE ONES THE OWNER IS
    // NOT A PARTY TO. This used to be gated behind a `replyable` flag —
    // {owner, peer} rows only — because the server refused a reply_to that
    // crossed conversations, so an entry on an inter-agent row would have 400'd
    // on every press. The owner removed that refusal on 2026-08-21 FOR THIS
    // EXACT CASE: 「引用另外兩個人對話裡的一句話來介入詢問」. With the gate gone
    // the entry works there — the reply is addressed to this thread's peer as
    // always, and it quotes the line the owner pointed at. Keeping the flag
    // would have left the owner's ruling unreachable from the product.
    const replyEntry = (
      <button
        type="button"
        className="chat__msg-reply"
        aria-label={t.chat.replyAction}
        title={t.chat.replyAction}
        onClick={() => {
          setReplyToId(m.id);
          inputRef.current?.focus();
        }}
      >
        <ReplyIcon size={13} />
      </button>
    );

    const content = m.replyCardId ? (
      <ChatReplyCard
        replyCardId={m.replyCardId}
        fallbackSummary={m.body}
        initialStatus={m.replyCardStatus}
      />
    ) : (
      <div
        className={
          "chat__msg-bubble" +
          // The corner ACTION SLOT reserves its own width so a hover can never
          // reflow the text under it. Two controls need more room than one, and
          // 放大閱讀 is the one that comes and goes.
          (!mine && m.body
            ? " chat__msg-bubble--expandable chat__msg-bubble--acts2"
            : " chat__msg-bubble--acts1")
        }
      >
        {/* The bubble's corner actions (T-4e95). ONE slot, so the two controls
         * cannot drift apart into two corners:
         *   • 回覆這則 — on EVERY bubble in the window, both directions, and
         *     including the inter-agent rows the owner is not a party to. That
         *     last part is new (2026-08-21): the server used to refuse a
         *     reply_to that crossed conversations so the entry was withheld
         *     there, and the owner removed that refusal precisely so the owner
         *     could quote a line out of two other people's thread and step in.
         *   • 放大閱讀 — reopens THIS message body in the shared full-view
         *     overlay. Only on INCOMING messages with text: an agent answer is
         *     the long-form side of the thread (the owner's own line is what
         *     they just typed), and an attachment-only bubble has no body to lay
         *     out — the file chip already carries its own 預覽 action. */}
        <div className="chat__msg-actions">
          {replyEntry}
          {!mine && m.body && (
            <button
              type="button"
              className="chat__msg-expand"
              aria-label={t.chat.expandMessage}
              title={t.chat.expandMessage}
              onClick={() =>
                setMdPreview({
                  kind: "message",
                  title: senderLabel,
                  source: m.body,
                })
              }
            >
              <ExpandIcon size={12} />
            </button>
          )}
        </div>
        {quoteLine}
        {/* T-84c8: the message body is the purest owner/agent free text in the
         * app (and, via webhooks, can carry text from an EXTERNAL system), so
         * it renders through the shared XSS-safe `Markdown` — same posture as
         * the reply-card body, which already renders this very field's
         * fallback as markdown. `breaks` keeps Enter meaning "new line", the
         * way the bubble's pre-wrap did before. */}
        {m.body && (
          <Markdown
            source={m.body}
            className="chat__msg-text doc-md"
            breaks
          />
        )}
        {/* Stored attachments — one click target, opening the shared popup. */}
        <AttachmentStrip
          attachments={m.attachments}
          className="chat__msg-attachments"
          itemClassName="chat__msg-attachment"
          imageClassName="chat__msg-image chat__msg-image--clickable"
        />
      </div>
    );
    return (
      <Fragment key={m.id}>
        {/* ② the "以下是未讀訊息" divider — a thin low-emphasis rule above the
         * first message that was unread at conversation entry. It renders for
         * the whole session (like LINE) even after the watermark clears. */}
        {m.id === firstUnreadId && (
          <div
            className="chat__unread-divider"
            role="separator"
            aria-label={t.chat.unreadBelow}
          >
            <span>{t.chat.unreadBelow}</span>
          </div>
        )}
        <div
          className={
            `chat__msg${mine ? " chat__msg--me" : ""}` +
            (m.replyCardId ? " chat__msg--card" : "") +
            (m.id === highlightMsgId ? " chat__msg--located" : "")
          }
          data-msg-id={m.id}
        >
          {mine ? (
          // LINE-style outgoing: a bottom-aligned meta column to the LEFT of the
          // bubble, stacking "已讀" (when read) above the send time.
          <div className="chat__msg-line">
            <div className="chat__msg-sidemeta">
              {read && <span className="chat__msg-read">{t.chat.read}</span>}
              <span className="chat__msg-time">{formatTime(m.ts)}</span>
            </div>
            <div className="chat__msg-content">
              {m.replyCardId && quoteLine}
              {content}
            </div>
            {m.replyCardId && replyEntry}
          </div>
        ) : (
          // LINE-style incoming: mirror of the outgoing row. The name label above
          // the bubble is `senderLabel` — the message's TRUE sender, plus the
          // recipient ("A → B") when the message is inter-agent; the send time
          // moves to a bottom-aligned meta column on the bubble's RIGHT edge.
          <>
            <div className="chat__msg-meta">
              <span className="chat__msg-name">{senderLabel}</span>
            </div>
            <div className="chat__msg-line">
              <div className="chat__msg-content">
                {m.replyCardId && quoteLine}
                {content}
              </div>
              <div className="chat__msg-sidemeta">
                <span className="chat__msg-time">{formatTime(m.ts)}</span>
              </div>
              {m.replyCardId && replyEntry}
            </div>
          </>
          )}
        </div>
      </Fragment>
    );
  }

  // Render one collapsible INTER-AGENT block. Collapsed (default): a single
  // toggle row announcing "N messages between agents · expand". Expanded: the
  // toggle stays as a collapse affordance, followed by the real message rows.
  function renderInterAgentGroup(group: {
    id: string;
    messages: ChatMessage[];
  }) {
    const expanded = groupExpanded(group);
    return (
      <div
        key={`inter-${group.id}`}
        className={`chat__inter${expanded ? " chat__inter--expanded" : ""}`}
      >
        <button
          type="button"
          className="chat__inter-toggle"
          aria-expanded={expanded}
          onClick={() => toggleGroup(group)}
        >
          <ChevronRightIcon
            size={13}
            className={`chat__inter-caret${
              expanded ? " chat__inter-caret--open" : ""
            }`}
          />
          <span>
            {expanded
              ? t.chat.interAgentCollapse
              : msg.chatInterAgentExpand(group.messages.length)}
          </span>
        </button>
        {expanded && (
          <div className="chat__inter-body">
            {group.messages.map((m) => renderMessage(m))}
          </div>
        )}
      </div>
    );
  }

  return (
    // Drag-drop staging surface: dropping files anywhere over the chat window
    // stages them as attachments (no-op while the composer is locked — the
    // handlers gate on composerLocked themselves).
    <div className="chat" onDragOver={onDragOver} onDrop={onDrop}>
      <header
        className={`chat__header${onOpenDetail ? " chat__header--clickable" : ""}`}
        {...(onOpenDetail
          ? {
              role: "button",
              tabIndex: 0,
              onClick: onOpenDetail,
              onKeyDown: (e: React.KeyboardEvent<HTMLElement>) => {
                if (e.key === "Enter" || e.key === " ") {
                  e.preventDefault();
                  onOpenDetail();
                }
              },
            }
          : {})}
      >
        {/* T-3738 / T-ea81: the header avatar's kind follows the peer's REAL
         * role — an outsource peer (ow- id) shows the theme's 外包 image, an
         * assistant the 助理 image, a 正職 peer the member image. Rendering
         * member for an outsource peer fabricated a 正職 identity. */}
        <Avatar size={38} kind={avatarKindForMember(member)} src={member.avatarUrl} />
        <div className="chat__header-text">
          {/* Name only — no chevron/caret glyph (owner feedback: the "Mira ›"
           * arrow was noise). The header itself stays the clickable detail
           * entry (chat__header--clickable above); its hover/focus affordance
           * carries the click hint now. */}
          <div className="chat__header-name">
            <span>{member.name}</span>
          </div>
          {/* Single presence truth: the SHARED PresenceBadge (lifecycle dot +
           * role) — same component as the roster card / monitor row / detail
           * panel. No self-drawn `role · lastSeen` (that was a second presence
           * source + the "online yet Never online" dishonesty). */}
          <div className="chat__header-sub">
            {headerSub ?? <PresenceBadge member={member} />}
          </div>
          {/* T-3451: the peer's CURRENT task title, FULL (no clamp) — owner 圖2.
           * Rendered only when present (a taskless / released peer grows no
           * empty line here; showEmpty=false). */}
          {headerTaskTitle ? (
            <div className="chat__header-task">
              <CurrentTaskTitle
                title={headerTaskTitle}
                clamp={false}
                showEmpty={false}
                testid="chat-header-task-title"
              />
            </div>
          ) : null}
        </div>
        {/* T-dfae (owner 2026-07-17, 紅框 on this corner): two jump buttons
         * beside the gallery toggle. Both are OPTIONAL — the caller wires them
         * only where the jump is real (a roster member). An outsource / released
         * peer gets NEITHER: it has no role to define, and its tasks are not
         * separable (every worker collapses to the single "outsource" executor
         * key, so a task jump would show OTHER workers' tasks too). Same
         * no-dead-click rule as onOpenDetail above — we do not advertise a jump
         * that would lie. Own classes, NOT chat__gallery-toggle: that class is a
         * querySelector handle in ChatArea.gallery.test.tsx and a second element
         * wearing it would be silently picked up instead. */}
        {onOpenTasks && (
          <button
            type="button"
            className="chat__header-action"
            aria-label={t.chat.tasksLink}
            title={t.chat.tasksLink}
            data-testid="chat-header-tasks"
            onClick={(e) => {
              e.stopPropagation();
              onOpenTasks();
            }}
            onKeyDown={(e) => e.stopPropagation()}
          >
            <TasksIcon size={17} />
          </button>
        )}
        {onOpenRoleSettings && (
          <button
            type="button"
            className="chat__header-action"
            aria-label={t.chat.roleSettingsLink}
            title={t.chat.roleSettingsLink}
            data-testid="chat-header-role-settings"
            onClick={(e) => {
              e.stopPropagation();
              onOpenRoleSettings();
            }}
            onKeyDown={(e) => e.stopPropagation()}
          >
            <UserGearIcon size={17} />
          </button>
        )}
        {/* M2-3: the conversation's file & image gallery toggle. The header
         * itself may be clickable (open detail) — stopPropagation keeps the
         * gallery click/keys from bubbling into that. */}
        <button
          type="button"
          className="chat__gallery-toggle"
          aria-label={t.chat.galleryLabel}
          title={t.chat.galleryLabel}
          aria-expanded={galleryOpen}
          onClick={(e) => {
            e.stopPropagation();
            setGalleryOpen((v) => !v);
          }}
          onKeyDown={(e) => e.stopPropagation()}
        >
          <ImageIcon size={17} />
        </button>
      </header>

      {galleryOpen && (
        <ChatGalleryPanel
          member={member}
          resolveSender={nameOf}
          onClose={() => setGalleryOpen(false)}
        />
      )}

      <div className="chat__body">
        {shownMessages.length > 0 ? (
          <>
            <div
              className="chat__messages"
              ref={messagesRef}
              onScroll={onMessagesScroll}
            >
              {/* 🔴 T-b0bb: THE GAP NOTICE COMES FIRST, AND IT SUPPRESSES
               * "已到最早訊息".
               *
               * `hasMore` answers one narrow question — "might there be more
               * history ABOVE the loaded window?" — and "已到最早訊息" is its
               * honest negative answer. But a reader does not read it that
               * narrowly: beside a thread with a hole punched in its MIDDLE it
               * reads as "you have the whole conversation", which is false.
               *
               * Measured on the pre-fix code: after a 40-message burst and a
               * full walk backwards, the thread was missing 10 rows in the
               * middle and `hasMore` was false — i.e. the UI actively declared
               * completeness over a hole. That is the exact shape this pair of
               * branches exists to prevent, so they are mutually exclusive by
               * construction rather than by CSS or ordering. */}
              {gapSuspected ? (
                <div className="chat__gap-notice" role="status">
                  <span>{t.chat.gapSuspected}</span>
                </div>
              ) : (
                !hasMore && (
                  <div className="chat__history-start" role="note">
                    <span>{t.chat.historyStart}</span>
                  </div>
                )
              )}
              {/* LINE/Slack-style day grouping: the stream splits at every
               * local-midnight crossing; each day renders a centered date
               * pill (今天/昨天/date) that is ALSO the scrolling floating
               * header — `position: sticky` inside its day-group wrapper
               * pins the pill to the viewport top while its day scrolls
               * through, and the group's end pushes it off naturally (no JS
               * scroll tracking). The label is judged against the render
               * clock; per-message times keep their existing hh:mm format. */}
              {splitByDay(shownMessages).map((day) => {
                const dayLabel = formatDayLabel(
                  day.dayTs,
                  Date.now() / 1000,
                  t.chat,
                );
                return (
                  <div key={day.dayTs} className="chat__day-group">
                    <div
                      className="chat__day-divider"
                      role="separator"
                      aria-label={dayLabel}
                    >
                      <span className="chat__day-pill">{dayLabel}</span>
                    </div>
                    {groupMessages(day.items).map((group) =>
                      group.kind === "inter"
                        ? renderInterAgentGroup(group)
                        : group.messages.map((m) => renderMessage(m)),
                    )}
                  </div>
                );
              })}
              {/* Bottom sentinel — scrolled into view to follow new messages. */}
              <div ref={endRef} className="chat__scroll-anchor" aria-hidden />
            </div>
            {/* ① the round 回到最新訊息 arrow, bottom-right of the pane and
             * therefore directly above the composer. It is NOT rendered
             * whenever the preview strip is (see bottomAffordance). */}
            {bottomAffordance === "arrow" && (
              <ChatJumpLatestButton onClick={jumpToLatest} />
            )}
          </>
        ) : isOffline ? (
          <div className="chat__offline">
            <span className="chat__offline-icon">
              <MoonIcon size={26} />
            </span>
            <div className="chat__offline-title">
              {msg.chatOfflineTitle(member.name)}
            </div>
            {/* T-94c1: offline/stopped can now be messaged (queues until wake),
             * so the hint no longer says "喚醒後才能開始對話" (which contradicted
             * the unlocked composer below). The wake entry + queue notice live on
             * the composer's wake row now, not on this card. */}
            <div className="chat__offline-hint">
              {offlineQueue
                ? msg.chatOfflineQueueHint(member.name)
                : t.chat.offlineHint}
            </div>
          </div>
        ) : (
          <div className="chat__empty">
            <span>{t.chat.emptyRange}</span>
          </div>
        )}
      </div>

      <footer className="chat__composer">
        {/* 🔴 T-48: the jump could not find its target. Pinned above the
         * composer rather than dropped into the stream, because the fallback
         * has just scrolled the thread to the BOTTOM — a notice placed in the
         * stream would land wherever the missing message would have been, i.e.
         * off screen, which is another way of not saying it. It outlives the
         * fallback scroll on purpose (the reader has to be able to look up and
         * find out why they are where they are) and is cleared by the x, by a
         * peer switch, or by a jump that later succeeds. */}
        {jumpNotice && (
          <div className="chat__jump-miss" role="status">
            <span>
              {jumpNotice === "interrupted"
                ? t.chat.jumpTargetInterrupted
                : jumpNotice === "unreachable"
                  ? t.chat.jumpTargetUnreachable
                  : t.chat.jumpTargetMissing}
            </span>
            {/* The two endings a retry can change get the button; 「找不到」
             * does not, because the server has answered and the answer will
             * not differ. See retryJump for why *interrupted* is one of them
             * (its old copy pointed at a link that could not re-fire). */}
            {jumpNotice !== "missing" && (
              <button
                type="button"
                className="chat__jump-miss__retry"
                data-testid="jump-miss-retry"
                onClick={retryJump}
              >
                {t.chat.jumpTargetRetry}
              </button>
            )}
            <button
              type="button"
              className="chat__jump-miss__x"
              aria-label={t.chat.jumpTargetMissingDismiss}
              title={t.chat.jumpTargetMissingDismiss}
              onClick={() => setJumpNotice(null)}
            >
              ×
            </button>
          </div>
        )}
        {/* ② the new-message preview strip. FIRST child of the composer, so it
         * sits above the 「正在回覆」 banner (owner's requirement) and above the
         * wake row and the attachment previews as well — and it is outside the
         * locked/unlocked fork on purpose: a read-only peer's thread still
         * receives messages, and the owner still has to be told.
         *
         * The whole strip is one jump target; the x drops it without moving the
         * viewport, after which the round arrow takes its place (the newest
         * message is still not on screen — dismissing a preview is not reading
         * it). */}
        {bottomAffordance === "preview" && newMsgPreview && (
          <ChatNewMsgPreview
            who={nameOf(newMsgPreview.from)}
            body={newMsgPreview.body}
            onJump={jumpToLatest}
            onDismiss={() => setNewMsgPreview(null)}
          />
        )}
        {composerLocked ? (
          /* T-9c3c: the composer locks ONLY for a peer with NO queue path — a
           * synthetic released/removed peer (read-only, T-661b) or an outsource
           * worker; OfficePage wires neither onWake nor a queue promise for
           * them. A live member always has a queue path (onWake), so it never
           * reaches here. A plain, non-clickable notice: there is nothing to
           * wake and no live detail panel to open for these peers. */
          <div className="chat__composer-locked" role="status">
            {msg.chatComposerOffline(member.name)}
          </div>
        ) : (
          <>
            {/* Wake row: shown for a live member in ANY non-online state
             * (offline/stopped/waking/stopping, T-9c3c) — an honest "your
             * message will queue" notice plus an in-place ⚡喚醒 button (calls
             * activateMember via onWake). Sits ABOVE the composer so the input
             * row stays full-width (owner mockup). The button is wired only when
             * the caller passes onWake (a member, not an outsource worker). */}
            {offlineQueue && (
              <div className="chat__wake-row">
                <span className="chat__wake-row__hint">
                  <MoonIcon size={14} />
                  {msg.chatWakeQueueHint(member.name)}
                </span>
                {onWake && (
                  <button
                    type="button"
                    className="chat__wake-btn"
                    onClick={() => {
                      setWakePending(true);
                      setWakeUndispatched(false);
                      // 🔴 WHOSE wake this is (review r2 SHOULD-1). The
                      // peer-keyed reset effect above is a reset, not a CANCEL:
                      // an activate still in flight when the owner switches
                      // peers resolves AFTER the reset and writes A's verdict
                      // into a room that is already B's. `visitRef` is the
                      // render-time mirror of the CURRENT visit (R6-1: the peer
                      // id said yes to the same peer's next visit too).
                      const firedFor = session;
                      // Revert the optimistic pending if the activate POST
                      // rejects (else the button sticks on "喚醒中…" forever) —
                      // same discipline as MemberDetailPanel's wake. The success
                      // path hands off to the real `waking` presence, and the
                      // lifecycle-keyed reset effect clears the optimism.
                      //
                      // 🔴 T-7fa1: a resolved activate is NOT proof a START went
                      // out. Reading activation_pending is what stops this button
                      // from sitting on 「喚醒中…」 for a wake nobody sent.
                      Promise.resolve(onWake())
                        .then((result) => {
                          if (visitRef.current !== firedFor) return;
                          if (result?.activationPending) {
                            setWakePending(false);
                            setWakeUndispatched(true);
                          }
                        })
                        .catch(() => {
                          if (visitRef.current !== firedFor) return;
                          setWakePending(false);
                        });
                    }}
                    disabled={wakeInFlight}
                  >
                    <BoltIcon size={15} />
                    <span>
                      {wakeInFlight ? t.chat.wakePending : t.chat.wakeButton}
                    </span>
                  </button>
                )}
              </div>
            )}
            {/* T-7fa1: the in-chat wake has its OWN optimistic state, so it needs
                its own outcome — the same notice the detail panel raises. */}
            {offlineQueue && wakeUndispatched && (
              <DispatchAlert kind="wake" testId="chat-wake-undispatched" />
            )}
            {(pendingAttachments.length > 0 || attachError) && (
              <ComposerAttachmentPreview
                pendingAttachments={pendingAttachments}
                attachError={
                  attachError ??
                  (overAttachmentCap
                    ? t.chat.attachTooMany(CHAT_MAX_ATTACHMENTS)
                    : null)
                }
                onRemove={removeAttachment}
                onOpenImage={(att) =>
                  setMdPreview({
                    kind: "staged-image",
                    title: att.filename || t.chat.pastedImageAlt,
                    imageSrc: att.dataUri,
                  })
                }
              />
            )}
            {/* T-4e95 ③ 「正在回覆」 — the LINE-style banner ABOVE the input
             * row, naming who is being answered and quoting a slice of what
             * they said, with an x that returns the composer to the ordinary
             * send state.
             *
             * 🔴 THE x CLEARS THE TARGET AND NOTHING ELSE. Half-typed text and
             * staged attachments stay exactly as they are — cancelling a reply
             * is not cancelling the message, and a composer that emptied itself
             * here would lose work the owner never asked to throw away. */}
            {replyToId && (
              <div className="chat__reply-banner" data-testid="chat-reply-banner">
                <ReplyIcon size={13} className="chat__reply-banner__icon" />
                {/* 🔴 DO NOT NAME SOMEONE WE HAVE NOT RESOLVED. This used to
                  * fall back to the peer's name whenever the quote had not come
                  * back, which is a claim, not a placeholder: the target is by
                  * construction one of TWO people (this conversation has only
                  * two), so the fallback was a coin flip printed as a fact.
                  *
                  * There used to be a THIRD state here — a 「…」 meaning "the
                  * by-id read has not landed yet". It went with the read: nothing
                  * is in flight any more, so a spinner would never resolve.
                  *
                  * 🔴 AND IT IS NOT THE QUOTE ROW'S SENTENCE EITHER. `replyQuote`
                  * comes from `messageById` — the LOADED WINDOW — so an unresolved
                  * target here does NOT mean the message is gone. Scroll back, aim
                  * at an old row, switch peers and come back to a freshly-loaded
                  * newest page: the target is still there, the send still succeeds,
                  * and the quote comes back whole on the reply's own row. Printing
                  * 「這則訊息已不存在」 in that state is a falsifiable claim about
                  * the world made from a fact about this browser's scroll position.
                  * `replyingToEarlier` is state-independent and stays true in both
                  * cases. */}
                <span className="chat__reply-banner__text">
                  <span className="chat__reply-banner__who">
                    {/* The same 「寄件者 → 收件者」 the quote row draws, off
                      * the LOADED message this banner resolves (see above) —
                      * so aiming at a line from another conversation says whose
                      * line it was, here as well as on the sent row. */}
                    {replyQuote
                      ? t.chat.replyingTo(
                          directionLabel(replyQuote.from, replyQuote.to),
                        )
                      : t.chat.replyingToEarlier}
                  </span>
                  <span className="chat__reply-banner__body">
                    {/* Raw, not pre-collapsed: `white-space: nowrap` on the
                      * parent is what makes this one line — see the note where
                      * `oneLine()` used to be. */}
                    {replyQuote ? replyQuote.body : ""}
                  </span>
                </span>
                <button
                  type="button"
                  className="chat__reply-banner__x"
                  aria-label={t.chat.replyCancel}
                  title={t.chat.replyCancel}
                  onClick={() => {
                    setReplyToId(null);
                    // 🔴 GIVE THE FOCUS BACK. This button is about to unmount
                    // itself, and a focused element that leaves the document
                    // hands focus to <body> — so a keyboard user who cancels
                    // one reply is thrown to the top of the page and has to Tab
                    // back through the whole thread to reach the composer. The
                    // reply ENTRY already does this on the way in; the way out
                    // was missing.
                    inputRef.current?.focus();
                  }}
                >
                  <CloseIcon size={14} />
                </button>
              </div>
            )}
            <div className="chat__composer-row">
              {/* Hidden native file input the attach button triggers. */}
              <input
                ref={fileInputRef}
                className="chat__file-input"
                type="file"
                accept={ATTACH_ACCEPT}
                multiple
                onChange={onPickFile}
                hidden
              />
              <button
                type="button"
                className="chat__attach"
                aria-label={t.chat.attachLabel}
                title={t.chat.attachLabel}
                onClick={() => fileInputRef.current?.click()}
              >
                <PaperclipIcon size={18} />
              </button>
              {/* Multi-line composer. Desktop: Enter sends, Shift+Enter breaks a
               * line. Mobile: Enter breaks a line and the send button sends
               * (onKeyDown → enterShouldSend; when it doesn't send it lets the
               * keydown fall through to the textarea's native newline). Height
               * follows the draft via the autosize layout-effect above. */}
              <textarea
                ref={inputRef}
                className="chat__input"
                rows={1}
                value={draft}
                onChange={(e) => setDraft(e.target.value)}
                onCompositionStart={() => {
                  isComposingRef.current = true;
                }}
                onCompositionEnd={(e) => {
                  isComposingRef.current = false;
                  // compositionend delivers the final committed text; sync the draft
                  // so the last composed chunk is never dropped (React's controlled
                  // onChange during composition is unreliable across browsers).
                  setDraft(e.currentTarget.value);
                }}
                onKeyDown={onKeyDown}
                onPaste={onPaste}
                placeholder={t.chat.inputPlaceholder(member.name)}
              />
              <button
                type="button"
                className="chat__send"
                aria-label={t.chat.send}
                onClick={() => void submit()}
                disabled={!canSend}
              >
                <SendIcon size={16} />
              </button>
            </div>
          </>
        )}
      </footer>

      {/* The full-view overlay this component opens (T-f014) — a 放大閱讀
       * message rides the body text this component already holds; a staged
       * composer image rides its data: URI. A STORED attachment is not here:
       * its chip is rendered by `AttachmentStrip`, which owns that overlay. */}
      {mdPreview &&
        (mdPreview.kind === "staged-image" ? (
          <MarkdownPreviewOverlay
            title={mdPreview.title}
            imageSrc={mdPreview.imageSrc}
            onClose={() => setMdPreview(null)}
          />
        ) : (
          <MarkdownPreviewOverlay
            title={mdPreview.title}
            source={mdPreview.source}
            onClose={() => setMdPreview(null)}
          />
        ))}
      {/* The 看原訊息 overlay is the shared exit's own — same surface, one
       * owner for the read behind it (hooks/useQuotedMessageOverlay). */}
      {quotedMessage.overlay}
    </div>
  );
}
