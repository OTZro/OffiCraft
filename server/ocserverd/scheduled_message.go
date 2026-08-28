package main

// scheduled_message.go — T-f059 定期訊息: the clock that drives the schedules,
// and the delivery that rides the ORDINARY chat path.
//
// Shape follows the five existing cadences (reconcile.go / auto_update.go):
// sleep-then-tick, no time.Ticker, no context cancel; the goroutine mount does
// nothing but loop, and ONE tick is a plain exported-to-tests function taking
// `now` so the tests never wait on a clock.
//
// The delivery half is api_webhooks.go's /in with the trigger swapped: the same
// synthesised chat_message, the same hub fan-out, the same audience. No new
// event type, no new SSE topic, no new mailbox — owner 2026-08-10, 「不能跟現有
// 聊天機制一樣就好嘛」.

import (
	"fmt"
	"os"
	"time"
)

// scheduledMessageCadence is the tick interval. The finest schedulable grain is
// a minute, so a 60s cadence can never be the reason a slot is late by more than
// its own period — and since the fire/skip test is slot-identity, not elapsed
// time, a tick that arrives late still delivers exactly once.
const scheduledMessageCadence = time.Minute

func schedLog(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[scheduled] "+format+"\n", args...)
}

// startScheduledMessageCadence mounts the always-on tick loop. First tick fires
// one full period after start (sleep-then-tick, matching every sibling cadence).
func (s *apiServer) startScheduledMessageCadence(period time.Duration) {
	go func() {
		for {
			time.Sleep(period)
			s.runScheduledMessageTick(nowSecs())
		}
	}()
	schedLog("cadence started (period=%gs)", period.Seconds())
}

// runScheduledMessageTick is ONE pass over every armed schedule — the unit the
// tests drive directly.
//
// Per row: compute the most recently elapsed slot, name it, and deliver only
// when that name differs from the one already recorded. Every per-row fault is
// contained to its own row: one schedule pointing at a departed member must not
// stop the other schedules from firing.
func (s *apiServer) runScheduledMessageTick(now float64) {
	rows, err := s.dal.ListAllEnabledScheduledMessages()
	if err != nil {
		schedLog("tick: listing schedules failed: %v", err)
		return
	}
	at := time.Unix(int64(now), 0)
	for _, sm := range rows {
		// 🔴 The SAME rule the write seam applies, applied again here, and it is
		// not redundant: mostRecentSlot resolves the zone with time.LoadLocation,
		// which happily accepts `Local` (the HOST's zone) and `` (UTC) — the
		// deployment-dependent answers this feature exists to refuse. A row can
		// still carry one by predating the rule or by being written straight into
		// the database, and the tick is the last place anything is looking.
		// Skipped, NOT retried against UTC: delivering at a guessed hour looks
		// exactly like delivering correctly. The log line is the only trace such
		// a row leaves.
		if err := ValidateScheduledMessageTimezone(sm.Timezone); err != nil {
			schedLog("skip %s: %v — no fallback zone is applied", describeSchedule(sm), err)
			continue
		}
		slot, ok := mostRecentSlot(sm, at)
		if !ok {
			continue // no slot has elapsed yet (e.g. a monthly day no recent month has)
		}
		key := slotKey(slot)
		if !slotIsAfterCursor(slot, sm.LastFiredSlot) {
			continue // already delivered, or older than what was — see slotIsAfterCursor
		}
		if err := s.deliverScheduledMessage(sm, key, now); err != nil {
			schedLog("skip %s: delivery failed: %v", describeSchedule(sm), err)
			continue
		}
		if err := s.dal.MarkScheduledMessageFired(sm.ID, key, now); err != nil {
			// The message DID go out; only the cursor is behind, so the next
			// tick would send it again. Loud, because a duplicate is otherwise
			// indistinguishable from a correct delivery.
			schedLog("delivered %s slot %s but the cursor did NOT advance (%v) — the next tick will resend", describeSchedule(sm), key, err)
		}
	}
}

// deliverScheduledMessage synthesises the ONE chat_message a due slot produces
// and fans the same "chat" delta every other message rides.
//
// 🔴 Recipient resolution goes through resolveChatRecipient, NOT resolveMember.
// A scheduled message IS a chat message, so it takes chat's recipient rule:
// assistants and outsource workers alike. Reaching for resolveMember would put
// the reachability of every `ow-` worker at the mercy of one memberScope
// argument, and a delivery loop is the worst place to discover a wrong one.
//
// A recipient that is gone or removed returns errNotFound. That row is skipped
// WITHOUT advancing its cursor: the cursor means "this slot was delivered", and
// writing it for an undelivered slot would launder a failure into a fact. The
// visible cost is one log line per tick while a dead schedule survives its
// member, which is a fair description of what is actually wrong.
func (s *apiServer) deliverScheduledMessage(sm ScheduledMessage, slot string, now float64) error {
	recipient, err := s.resolveChatRecipient(sm.MemberID)
	if err != nil {
		return err
	}
	msg := ChatMessage{
		ID:        "c-" + newHexID(12),
		Sender:    "sched:" + sm.ID,
		Recipient: recipient,
		Body:      sm.Body,
		TS:        now,
		Meta: map[string]any{
			"scheduled": map[string]any{
				"schedule_id": sm.ID,
				"label":       sm.Label,
				"slot":        slot,
			},
		},
	}
	if err := s.dal.PutChat(msg); err != nil {
		return err
	}
	// Same convenience payload and audience as every chat delta (spec/sse.md
	// §2.2): both participants plus the owner.
	s.hub.Publish("chat", "patch", "chat", wireOwnerID+"::"+msg.ID,
		map[string]any{"id": msg.ID, "from": msg.Sender, "to": msg.Recipient},
		audienceMembers(msg.Sender, msg.Recipient), triggerServer)
	return nil
}
