package main

// scheduled_message_test.go — T-f059 定期訊息 sentinels.
//
// Every test here states the ONE bug it goes red on; a test that cannot name
// its bug is decoration. The load-bearing ones are the last three: the
// cross-month lookback (silent in February and invisible to any fixture built
// from days 1-28), the fire-once invariant (a resend looks EXACTLY like a
// correct delivery), and the outsource recipient (resolveMember excludes
// outsource workers outright, so getting this wrong silently makes every
// `ow-` schedule undeliverable — the same hole webhooks have today).

import (
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

// scheduledStack assembles the full wired stack and hands back the apiServer
// too, so a test can plant rows directly and drive ONE tick without waiting on
// the cadence goroutine.
func scheduledStack(t *testing.T) (*httptest.Server, []byte, *apiServer) {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "scheduled-test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	dal := NewDAL(db)
	if err := seedOutOfBox(dal); err != nil {
		t.Fatalf("seed: %v", err)
	}
	secret := []byte(interopSecret)
	api := newAPIServer(dal, NewHub(), secret, 3600, "../..")
	h, err := buildHandler(specsFor(api), secret, dal.GetMember, nil)
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}
	api.loopback = h
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, secret, api
}

// chatsFrom returns every stored message whose sender is exactly `sender`.
func chatsFrom(t *testing.T, api *apiServer, sender string) []ChatMessage {
	t.Helper()
	all, err := api.dal.ListChat()
	if err != nil {
		t.Fatalf("list chat: %v", err)
	}
	var out []ChatMessage
	for _, m := range all {
		if m.Sender == sender {
			out = append(out, m)
		}
	}
	return out
}

func mustParseSlot(t *testing.T, key string) time.Time {
	t.Helper()
	parsed, err := time.Parse(slotKeyLayout, key)
	if err != nil {
		t.Fatalf("slot key %q does not parse with the canonical layout: %v", key, err)
	}
	return parsed
}

// TestMostRecentSlot pins the arithmetic for all three cadences against known
// inputs, in a NON-UTC zone. Goes red if the wall clock is read in the wrong
// zone, if "today's slot has not arrived yet" fails to fall back a day/week, or
// if the weekday indexing drifts (0 must be Sunday).
func TestMostRecentSlot(t *testing.T) {
	taipei, err := time.LoadLocation("Asia/Taipei")
	if err != nil {
		t.Fatalf("Asia/Taipei will not load — tz data is missing from this binary: %v", err)
	}
	cases := []struct {
		name string
		sm   ScheduledMessage
		now  time.Time
		want string
	}{{
		// 10:30 local, daily 09:00 → today's slot has already elapsed.
		name: "daily after the slot takes today",
		sm:   ScheduledMessage{Cadence: ScheduledMessageCadenceDaily, Hour: 9, Timezone: "Asia/Taipei"},
		now:  time.Date(2026, time.August, 10, 10, 30, 0, 0, taipei),
		want: "2026-08-10T09:00+08:00",
	}, {
		// 08:30 local, daily 09:00 → today's has not arrived; yesterday's is the
		// most recent one.
		name: "daily before the slot falls back to yesterday",
		sm:   ScheduledMessage{Cadence: ScheduledMessageCadenceDaily, Hour: 9, Timezone: "Asia/Taipei"},
		now:  time.Date(2026, time.August, 10, 8, 30, 0, 0, taipei),
		want: "2026-08-09T09:00+08:00",
	}, {
		// 2026-08-10 is a Monday; day_of_week=1 (Monday) at 09:00 has elapsed.
		name: "weekly on the day after the slot takes today",
		sm: ScheduledMessage{Cadence: ScheduledMessageCadenceWeekly, DayOfWeek: 1,
			Hour: 9, Timezone: "Asia/Taipei"},
		now:  time.Date(2026, time.August, 10, 10, 0, 0, 0, taipei),
		want: "2026-08-10T09:00+08:00",
	}, {
		// day_of_week=0 is SUNDAY. From Monday 2026-08-10 the most recent
		// Sunday slot is 2026-08-09.
		name: "weekly zero means sunday",
		sm: ScheduledMessage{Cadence: ScheduledMessageCadenceWeekly, DayOfWeek: 0,
			Hour: 9, Timezone: "Asia/Taipei"},
		now:  time.Date(2026, time.August, 10, 10, 0, 0, 0, taipei),
		want: "2026-08-09T09:00+08:00",
	}, {
		// Monday 09:00 has not arrived at 08:00 → the same weekday a week back.
		name: "weekly before the slot falls back a week",
		sm: ScheduledMessage{Cadence: ScheduledMessageCadenceWeekly, DayOfWeek: 1,
			Hour: 9, Timezone: "Asia/Taipei"},
		now:  time.Date(2026, time.August, 10, 8, 0, 0, 0, taipei),
		want: "2026-08-03T09:00+08:00",
	}, {
		name: "monthly on the day after the slot takes this month",
		sm: ScheduledMessage{Cadence: ScheduledMessageCadenceMonthly, DayOfMonth: 10,
			Hour: 9, Timezone: "Asia/Taipei"},
		now:  time.Date(2026, time.August, 10, 10, 0, 0, 0, taipei),
		want: "2026-08-10T09:00+08:00",
	}, {
		name: "monthly before the day falls back a month",
		sm: ScheduledMessage{Cadence: ScheduledMessageCadenceMonthly, DayOfMonth: 20,
			Hour: 9, Timezone: "Asia/Taipei"},
		now:  time.Date(2026, time.August, 10, 10, 0, 0, 0, taipei),
		want: "2026-07-20T09:00+08:00",
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			slot, ok := mostRecentSlot(tc.sm, tc.now)
			if !ok {
				t.Fatalf("no slot found; want %s", tc.want)
			}
			if got := slotKey(slot); got != tc.want {
				t.Fatalf("slot: want %s, got %s", tc.want, got)
			}
		})
	}
}

// TestMostRecentSlotIsComputedInTheSchedulesOwnZone is the anti-tautology
// guard for the table above: every assertion there would still pass if the
// implementation ignored the Timezone field and computed in UTC, because the
// dates happen to agree. Here the SAME schedule in two zones must produce two
// DIFFERENT slots — which is only true if the zone is genuinely read.
//
// Red when: LoadLocation's result is dropped, or an unloadable zone falls back
// to UTC/Local instead of refusing.
func TestMostRecentSlotIsComputedInTheSchedulesOwnZone(t *testing.T) {
	// 2026-08-10 01:00 UTC is already 09:00 in Taipei: the Taipei schedule's
	// 09:00 slot is TODAY's, the UTC one's is YESTERDAY's.
	now := time.Date(2026, time.August, 10, 1, 0, 0, 0, time.UTC)
	base := ScheduledMessage{Cadence: ScheduledMessageCadenceDaily, Hour: 9}

	taipei := base
	taipei.Timezone = "Asia/Taipei"
	tpeSlot, ok := mostRecentSlot(taipei, now)
	if !ok {
		t.Fatal("Asia/Taipei schedule produced no slot")
	}
	utc := base
	utc.Timezone = "UTC"
	utcSlot, ok := mostRecentSlot(utc, now)
	if !ok {
		t.Fatal("UTC schedule produced no slot")
	}
	if slotKey(tpeSlot) == slotKey(utcSlot) {
		t.Fatalf("Asia/Taipei and UTC produced the SAME slot (%s) — the schedule's "+
			"timezone is not being read", slotKey(tpeSlot))
	}
	if got := slotKey(tpeSlot); got != "2026-08-10T09:00+08:00" {
		t.Fatalf("Asia/Taipei slot: want 2026-08-10T09:00+08:00, got %s", got)
	}
	if got := slotKey(utcSlot); got != "2026-08-09T09:00+00:00" {
		t.Fatalf("UTC slot: want 2026-08-09T09:00+00:00, got %s", got)
	}

	// An unloadable zone is refused, NOT silently computed somewhere else.
	broken := base
	broken.Timezone = "Mars/Olympus_Mons"
	if _, ok := mostRecentSlot(broken, now); ok {
		t.Fatal("an unloadable timezone produced a slot — a fallback zone was applied somewhere")
	}
}

// TestMostRecentSlotSkipsMonthsWithoutTheDay is the cross-month lookback pin
// (the design's 🔴). day_of_month=31 in mid-February: February has no 31st, so
// per RFC 5545 that occurrence is dropped from the recurrence set entirely and
// the most recent slot is 31 JANUARY.
//
// Red when: the search looks back only one month (returns "no slot" and the
// schedule silently never fires — nothing alarms), or when the invalid date is
// clamped to the end of February, or when time.Date's rollover to 3 March is
// accepted as a real slot.
func TestMostRecentSlotSkipsMonthsWithoutTheDay(t *testing.T) {
	taipei, err := time.LoadLocation("Asia/Taipei")
	if err != nil {
		t.Fatalf("Asia/Taipei will not load: %v", err)
	}
	sm := ScheduledMessage{Cadence: ScheduledMessageCadenceMonthly, DayOfMonth: 31,
		Hour: 9, Timezone: "Asia/Taipei"}
	now := time.Date(2026, time.February, 15, 12, 0, 0, 0, taipei)

	slot, ok := mostRecentSlot(sm, now)
	if !ok {
		t.Fatal("no slot found for day_of_month=31 in mid-February — the lookback " +
			"stopped at one month, so this schedule would never fire and nothing would say so")
	}
	if got := slotKey(slot); got != "2026-01-31T09:00+08:00" {
		t.Fatalf("want the 31 January slot (2026-01-31T09:00+08:00), got %s — "+
			"February was clamped or rolled over instead of skipped", got)
	}

	// The 29th in a non-leap February is the same shape one month narrower:
	// 2026 is not a leap year, so 29 January is the answer.
	sm.DayOfMonth = 29
	slot, ok = mostRecentSlot(sm, now)
	if !ok {
		t.Fatal("no slot found for day_of_month=29 in mid-February 2026 (not a leap year)")
	}
	if got := slotKey(slot); got != "2026-01-29T09:00+08:00" {
		t.Fatalf("want 2026-01-29T09:00+08:00, got %s", got)
	}

	// And the rollover really is refused at the source, not merely stepped over.
	if _, exists := monthlySlot(2026, time.February, sm, taipei); exists {
		t.Fatal("29 February 2026 was accepted as a real date — time.Date's " +
			"normalisation is not being checked")
	}
}

// TestRunScheduledMessageTickFiresEachSlotOnce is the restart-does-not-resend
// invariant. Two ticks over the same elapsed slot must produce exactly ONE
// message; a third, after the schedule is re-aimed at a slot that has not been
// delivered, produces one more.
//
// Red when: the cursor is not written, is written as a timestamp rather than a
// slot identifier, or is compared with anything other than string equality.
// This is the test that has to exist because a duplicate delivery is
// indistinguishable, in the chat log, from a correct one.
func TestRunScheduledMessageTickFiresEachSlotOnce(t *testing.T) {
	_, _, api := scheduledStack(t)
	// Daily 00:00 UTC: whatever "now" is, a slot has always already elapsed.
	sm := ScheduledMessage{
		ID: "sch-once", MemberID: "mira", Label: "daily standup",
		Body: "time for the standup", Cadence: ScheduledMessageCadenceDaily,
		DayOfMonth: 1, Timezone: "UTC", Status: ScheduledMessageStatusEnabled,
		CreatedTS: nowSecs(),
	}
	if err := api.dal.PutScheduledMessage(sm); err != nil {
		t.Fatalf("seed schedule: %v", err)
	}

	api.runScheduledMessageTick(nowSecs())
	api.runScheduledMessageTick(nowSecs())

	msgs := chatsFrom(t, api, "sched:sch-once")
	if len(msgs) != 1 {
		t.Fatalf("two ticks over the SAME slot delivered %d messages; want exactly 1", len(msgs))
	}
	if msgs[0].Recipient != "mira" || msgs[0].Body != "time for the standup" {
		t.Fatalf("unexpected delivery: %+v", msgs[0])
	}
	meta, _ := msgs[0].Meta["scheduled"].(map[string]any)
	if meta == nil {
		t.Fatalf("delivered message carries no meta.scheduled: %+v", msgs[0].Meta)
	}
	if meta["schedule_id"] != "sch-once" || meta["label"] != "daily standup" {
		t.Fatalf("meta.scheduled does not identify the schedule: %+v", meta)
	}
	fired, _ := meta["slot"].(string)
	stored, err := api.dal.GetScheduledMessage("sch-once")
	if err != nil || stored == nil {
		t.Fatalf("reload schedule: %v %v", stored, err)
	}
	if stored.LastFiredSlot != fired || fired == "" {
		t.Fatalf("cursor %q does not match the delivered slot %q", stored.LastFiredSlot, fired)
	}
	if stored.LastFiredTS == 0 {
		t.Fatal("last_fired_ts stayed 0 after a real delivery")
	}
	// The cursor is a SLOT identifier, not a clock reading.
	if _, err := time.Parse(slotKeyLayout, stored.LastFiredSlot); err != nil {
		t.Fatalf("last_fired_slot %q is not a slot identifier: %v", stored.LastFiredSlot, err)
	}

	// Re-aim at a slot that has NOT been delivered → the next tick fires once.
	stored.LastFiredSlot = "1999-01-01T00:00+00:00"
	if err := api.dal.PutScheduledMessage(*stored); err != nil {
		t.Fatalf("re-aim: %v", err)
	}
	api.runScheduledMessageTick(nowSecs())
	if n := len(chatsFrom(t, api, "sched:sch-once")); n != 2 {
		t.Fatalf("an undelivered slot produced %d total messages; want 2", n)
	}
}

// TestCreateScheduledMessageDoesNotFireOnTheFirstTick pins the third acceptance
// condition, on the REAL create path: the cursor is seeded at creation, so a
// schedule created at 10:00 for daily 09:00 does not deliver today's 09:00.
//
// Red when: last_fired_slot is left empty at creation — the very next tick then
// sees a slot that differs from "" and delivers immediately.
func TestCreateScheduledMessageDoesNotFireOnTheFirstTick(t *testing.T) {
	srv, secret, api := scheduledStack(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")

	status, created := doJSON(t, "POST", srv.URL+"/api/members/mira/scheduled-messages",
		ownerTok, `{"label":"morning ping","body":"good morning","cadence":"daily",`+
			`"hour":9,"minute":0,"timezone":"Asia/Taipei"}`)
	if status != 200 {
		t.Fatalf("create: want 200, got %d %v", status, created)
	}
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("create returned no id: %v", created)
	}
	if cursor, _ := created["last_fired_slot"].(string); cursor == "" {
		t.Fatalf("a fresh schedule has an EMPTY delivery cursor (%v) — the next "+
			"tick will deliver a slot that elapsed before the schedule existed", created)
	}

	api.runScheduledMessageTick(nowSecs())
	if msgs := chatsFrom(t, api, "sched:"+id); len(msgs) != 0 {
		t.Fatalf("the first tick after creation delivered %d message(s); want 0", len(msgs))
	}
}

// TestScheduledMessageDeliversToAnOutsourceWorker is the resolveMember guard.
// resolveMember (api_helpers.go) refuses Kind == KindOutsource outright, which
// is exactly why a webhook cannot be bound to an `ow-` worker today; the design
// requires scheduled messages to use CHAT's recipient rule instead.
//
// Red when: delivery (or the CRUD) resolves the recipient with resolveMember —
// the worker becomes a 404 on create and an undeliverable row in the tick, and
// the only symptom is a schedule that never fires.
func TestScheduledMessageDeliversToAnOutsourceWorker(t *testing.T) {
	srv, secret, api := scheduledStack(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")
	worker := Member{ID: "ow-contractor", Name: "O-77", Kind: KindOutsource,
		RosterStatus: RosterStatusActive}
	if err := api.dal.PutMember(worker); err != nil {
		t.Fatalf("seed worker: %v", err)
	}

	// The CRUD accepts an ow- target (resolveMember would 404 here).
	status, created := doJSON(t, "POST",
		srv.URL+"/api/members/ow-contractor/scheduled-messages", ownerTok,
		`{"label":"worker check","body":"status check","cadence":"daily",`+
			`"hour":0,"minute":0,"timezone":"UTC"}`)
	if status != 200 {
		t.Fatalf("create on an outsource worker: want 200, got %d %v — "+
			"the recipient is being resolved with resolveMember, which excludes ow- workers",
			status, created)
	}
	id, _ := created["id"].(string)

	// And the tick really delivers to it: re-aim the cursor at a slot that has
	// not been delivered, then run one tick.
	stored, err := api.dal.GetScheduledMessage(id)
	if err != nil || stored == nil {
		t.Fatalf("reload schedule: %v %v", stored, err)
	}
	stored.LastFiredSlot = "1999-01-01T00:00+00:00"
	if err := api.dal.PutScheduledMessage(*stored); err != nil {
		t.Fatalf("re-aim: %v", err)
	}
	api.runScheduledMessageTick(nowSecs())

	msgs := chatsFrom(t, api, "sched:"+id)
	if len(msgs) != 1 {
		t.Fatalf("delivery to an outsource worker produced %d message(s); want 1", len(msgs))
	}
	if msgs[0].Recipient != "ow-contractor" {
		t.Fatalf("delivered to %q, want ow-contractor", msgs[0].Recipient)
	}
	if msgs[0].Sender != "sched:"+id {
		t.Fatalf("synthetic sender is %q, want sched:%s", msgs[0].Sender, id)
	}
}

// TestScheduledMessageValidationRefusesUnusableSchedules pins the write-seam
// refusals, one per invariant. The timezone leg is the one that matters most:
// a name the tz database cannot resolve MUST fail here, while somebody is
// looking, because a schedule that silently runs in the wrong zone delivers on
// time-looking messages at the wrong hour and nothing ever alarms.
func TestScheduledMessageValidationRefusesUnusableSchedules(t *testing.T) {
	srv, secret, _ := scheduledStack(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")
	base := `"body":"x","cadence":"daily","hour":9,"minute":0,"timezone":"Asia/Taipei"`
	cases := []struct{ name, body string }{
		{"unknown cadence", `{"body":"x","cadence":"hourly","hour":9,"minute":0,"timezone":"UTC"}`},
		{"blank body", `{"body":"   ","cadence":"daily","hour":9,"minute":0,"timezone":"UTC"}`},
		{"hour out of range", `{"body":"x","cadence":"daily","hour":24,"minute":0,"timezone":"UTC"}`},
		{"minute out of range", `{"body":"x","cadence":"daily","hour":9,"minute":60,"timezone":"UTC"}`},
		{"day_of_week out of range", `{` + base + `,"day_of_week":7}`},
		{"day_of_month zero", `{` + base + `,"day_of_month":0}`},
		{"day_of_month past 31", `{` + base + `,"day_of_month":32}`},
		{"unknown timezone", `{"body":"x","cadence":"daily","hour":9,"minute":0,"timezone":"Mars/Olympus_Mons"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, resp := doJSON(t, "POST",
				srv.URL+"/api/members/mira/scheduled-messages", ownerTok, tc.body)
			if status != 422 {
				t.Fatalf("want 422, got %d %v", status, resp)
			}
		})
	}

	// day_of_month 31 is ACCEPTED (owner ruling rc-aeef15360ab5: RFC 5545, the
	// range is not capped at 28) — the sentinel against a well-meaning tighten.
	status, resp := doJSON(t, "POST", srv.URL+"/api/members/mira/scheduled-messages",
		ownerTok, `{"body":"x","cadence":"monthly","day_of_month":31,"hour":9,"minute":0,"timezone":"UTC"}`)
	if status != 200 {
		t.Fatalf("day_of_month=31 must be accepted (RFC 5545 ruling), got %d %v", status, resp)
	}
}

// TestUpdateScheduledMessageReAimsTheCursorOnlyWhenReAimed pins the PATCH
// contract the frozen spec states: editing a cadence/slot field moves the
// cursor to the slot current now (so the edit never fires the slot it crossed),
// while editing label/body/status leaves the cursor exactly where it was.
//
// Red when: the cursor is reset on EVERY patch (a disable/enable round-trip
// would then swallow the next delivery), or never (re-aiming a schedule
// backwards fires retroactively on the next tick).
func TestUpdateScheduledMessageReAimsTheCursorOnlyWhenReAimed(t *testing.T) {
	srv, secret, api := scheduledStack(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")
	status, created := doJSON(t, "POST", srv.URL+"/api/members/mira/scheduled-messages",
		ownerTok, `{"body":"ping","cadence":"daily","hour":9,"minute":0,"timezone":"Asia/Taipei"}`)
	if status != 200 {
		t.Fatalf("create: %d %v", status, created)
	}
	id, _ := created["id"].(string)
	path := srv.URL + "/api/members/mira/scheduled-messages/" + id

	// Plant a distinctive stale cursor so a move is measurable either way.
	stored, _ := api.dal.GetScheduledMessage(id)
	stored.LastFiredSlot = "1999-01-01T00:00+08:00"
	if err := api.dal.PutScheduledMessage(*stored); err != nil {
		t.Fatalf("plant cursor: %v", err)
	}

	status, patched := doJSON(t, "PATCH", path, ownerTok, `{"status":"disabled","label":"renamed"}`)
	if status != 200 {
		t.Fatalf("status/label patch: %d %v", status, patched)
	}
	if got, _ := patched["last_fired_slot"].(string); got != "1999-01-01T00:00+08:00" {
		t.Fatalf("a status/label edit moved the cursor to %q — disabling and "+
			"re-enabling would silently swallow a delivery", got)
	}

	status, patched = doJSON(t, "PATCH", path, ownerTok, `{"hour":8}`)
	if status != 200 {
		t.Fatalf("hour patch: %d %v", status, patched)
	}
	got, _ := patched["last_fired_slot"].(string)
	if got == "1999-01-01T00:00+08:00" || got == "" {
		t.Fatalf("re-aiming the slot left the cursor at %q — the next tick would "+
			"deliver the slot the edit just crossed", got)
	}
	if slot := mustParseSlot(t, got); slot.After(time.Now()) {
		t.Fatalf("the re-aimed cursor %q is in the FUTURE — it must be the slot "+
			"most recently elapsed", got)
	}

	// An unloadable timezone is refused on PATCH too, not just on create.
	if status, resp := doJSON(t, "PATCH", path, ownerTok, `{"timezone":"Mars/Olympus_Mons"}`); status != 422 {
		t.Fatalf("patching to an unknown timezone: want 422, got %d %v", status, resp)
	}
}
