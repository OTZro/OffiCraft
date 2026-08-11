package main

// scheduled_message_custom_t49e7_test.go — T-49e7 `custom` cadence sentinels.
//
// Same rule as its T-f059 sibling: every test states the ONE bug it goes red
// on. The load-bearing ones are the two DST arms. The spring one pins the
// deliberate divergence from the calendar cadences (a skipped reading is
// DROPPED, never searched forward, because searching forward lands on a reading
// already in the set and two deliveries merge into one without a word), and the
// autumn one pins the behaviour that is NOT changed (an ambiguous reading
// resolves to the earlier offset, so the second pass delivers nothing) so that
// it stays a recorded decision rather than an accident somebody later "fixes".

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// customSchedulePayload renders a create body for a `custom` schedule.
func customSchedulePayload(days, hours, minutes []int, tz string) string {
	return fmt.Sprintf(`{"body":"tick","cadence":"custom","custom_days":%s,`+
		`"custom_hours":%s,"custom_minutes":%s,"timezone":%q}`,
		jsonInts(days), jsonInts(hours), jsonInts(minutes), tz)
}

func jsonInts(vals []int) string {
	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[i] = fmt.Sprint(v)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func intRange(lo, hi int) []int {
	var out []int
	for v := lo; v <= hi; v++ {
		out = append(out, v)
	}
	return out
}

// TestEveryCadenceInTheClosedSetProducesASlot is the guard against the way this
// feature fails without saying anything: a cadence the wire accepts but the slot
// arithmetic does not implement.
//
// mostRecentSlot answers "no slot" for an unimplemented cadence, the tick skips
// the row, and a schedule that can NEVER fire looks exactly like one with
// nothing due. Nothing errors, nothing alarms. So the closed set is data
// (scheduledMessageCadences) and this test walks it: every member must have a
// fixture here AND that fixture must produce a real slot.
//
// Red when: a value is added to the closed set without teaching
// schedule_slot.go — the failure names the value.
func TestEveryCadenceInTheClosedSetProducesASlot(t *testing.T) {
	taipei := mustLoadZone(t, "Asia/Taipei")
	now := time.Date(2026, time.August, 10, 10, 30, 0, 0, taipei)
	// One fixture per cadence: a schedule of that cadence for which a slot has
	// certainly already elapsed at `now`.
	fixtures := map[string]ScheduledMessage{
		ScheduledMessageCadenceDaily: {Cadence: ScheduledMessageCadenceDaily,
			Hour: 9, Minute: 0, DayOfMonth: 1, Timezone: "Asia/Taipei"},
		ScheduledMessageCadenceWeekly: {Cadence: ScheduledMessageCadenceWeekly,
			DayOfWeek: 1, Hour: 9, Minute: 0, DayOfMonth: 1, Timezone: "Asia/Taipei"},
		ScheduledMessageCadenceMonthly: {Cadence: ScheduledMessageCadenceMonthly,
			DayOfMonth: 1, Hour: 9, Minute: 0, Timezone: "Asia/Taipei"},
		ScheduledMessageCadenceCustom: {Cadence: ScheduledMessageCadenceCustom,
			CustomDays: intRange(1, 31), CustomHours: intRange(0, 23),
			CustomMinutes: []int{0, 20, 40}, Timezone: "Asia/Taipei"},
	}
	if len(scheduledMessageCadences) == 0 {
		t.Fatal("the closed set is empty — this guard would pass vacuously")
	}
	for _, cadence := range scheduledMessageCadences {
		sm, ok := fixtures[cadence]
		if !ok {
			t.Fatalf("cadence %q is in the closed set but has no fixture here. If it is a NEW "+
				"cadence, mostRecentSlot almost certainly has no branch for it either — and that "+
				"failure is silent: the schedule simply never fires, forever, with nothing to observe. "+
				"Add the branch in schedule_slot.go and a fixture here.", cadence)
		}
		sm.ID = "sch-closed-" + cadence
		slot, ok := mostRecentSlot(sm, now)
		if !ok {
			t.Fatalf("cadence %q produced NO slot — mostRecentSlot has no working branch for it, "+
				"so every schedule of this cadence is unfireable and silent about it", cadence)
		}
		if slot.After(now) {
			t.Fatalf("cadence %q produced %s, which is in the FUTURE", cadence, slotKey(slot))
		}
	}
}

// TestScheduledMessageRowSurvivesTheTableRebuild is the migration sentinel.
// migrations/00052 rebuilds scheduled_message (SQLite cannot drop a CHECK in
// place), copying every row column for column — and a rebuild that drops or
// transposes a column is invisible to any test that only reads back the field
// it just wrote.
//
// Red when: the rebuild's INSERT…SELECT loses a column, or
// scheduledMessageColumns and scanScheduledMessage disagree about the order.
func TestScheduledMessageRowSurvivesTheTableRebuild(t *testing.T) {
	_, _, api := scheduledStack(t)
	rows := []ScheduledMessage{{
		ID: "sch-rt-daily", MemberID: "mira", Label: "daily label", Body: "daily body",
		Cadence: ScheduledMessageCadenceDaily, DayOfWeek: 3, DayOfMonth: 17,
		Hour: 9, Minute: 5, Timezone: "Asia/Taipei", Status: ScheduledMessageStatusEnabled,
		LastFiredSlot: "2026-08-10T09:05+08:00", LastFiredTS: 1786000000.5, CreatedTS: 1785000000.25,
	}, {
		ID: "sch-rt-weekly", MemberID: "mira", Label: "weekly label", Body: "weekly body",
		Cadence: ScheduledMessageCadenceWeekly, DayOfWeek: 0, DayOfMonth: 28,
		Hour: 23, Minute: 59, Timezone: "UTC", Status: ScheduledMessageStatusDisabled,
		LastFiredSlot: "2026-08-09T23:59+00:00", LastFiredTS: 1786000001.5, CreatedTS: 1785000001.25,
	}, {
		ID: "sch-rt-monthly", MemberID: "mira", Label: "monthly label", Body: "monthly body",
		Cadence: ScheduledMessageCadenceMonthly, DayOfWeek: 6, DayOfMonth: 31,
		Hour: 0, Minute: 0, Timezone: "America/New_York", Status: ScheduledMessageStatusEnabled,
		LastFiredSlot: "2026-07-31T00:00-04:00", LastFiredTS: 1786000002.5, CreatedTS: 1785000002.25,
	}}
	for _, want := range rows {
		t.Run(want.Cadence, func(t *testing.T) {
			if err := api.dal.PutScheduledMessage(want); err != nil {
				t.Fatalf("write: %v", err)
			}
			got, err := api.dal.GetScheduledMessage(want.ID)
			if err != nil || got == nil {
				t.Fatalf("read back: %v %v", got, err)
			}
			if !reflect.DeepEqual(*got, want) {
				t.Fatalf("the row did not survive the round trip\n got: %+v\nwant: %+v", *got, want)
			}
		})
	}
}

// TestCustomSetsRoundTripInCanonicalForm pins the storage invariant
// migrations/00052 calls load-bearing: the sets come back sorted and
// deduplicated whatever order they went in as.
//
// Red when: the canonical form is applied somewhere other than the write seam
// (or nowhere). The PATCH re-aim comparison then reads [20,0,40] and [0,20,40]
// as different values, so a cockpit that posts the whole form back re-aims the
// cursor on every save and swallows the crossed delivery.
func TestCustomSetsRoundTripInCanonicalForm(t *testing.T) {
	_, _, api := scheduledStack(t)
	sm := ScheduledMessage{
		ID: "sch-canon", MemberID: "mira", Body: "tick",
		Cadence: ScheduledMessageCadenceCustom, DayOfMonth: 1,
		CustomDays: []int{31, 1, 15, 1}, CustomHours: []int{9, 0, 9},
		CustomMinutes: []int{20, 0, 40},
		Timezone:      "Asia/Taipei", Status: ScheduledMessageStatusEnabled, CreatedTS: nowSecs(),
	}
	if err := api.dal.PutScheduledMessage(sm); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := api.dal.GetScheduledMessage("sch-canon")
	if err != nil || got == nil {
		t.Fatalf("read back: %v %v", got, err)
	}
	for _, tc := range []struct {
		field string
		got   []int
		want  []int
	}{
		{"custom_days", got.CustomDays, []int{1, 15, 31}},
		{"custom_hours", got.CustomHours, []int{0, 9}},
		{"custom_minutes", got.CustomMinutes, []int{0, 20, 40}},
	} {
		if !reflect.DeepEqual(tc.got, tc.want) {
			t.Fatalf("%s read back as %v, want the canonical %v", tc.field, tc.got, tc.want)
		}
	}
	// The round trip is identity on its own output: writing what was read back
	// changes nothing.
	if err := api.dal.PutScheduledMessage(*got); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	again, err := api.dal.GetScheduledMessage("sch-canon")
	if err != nil || again == nil {
		t.Fatalf("second read back: %v %v", again, err)
	}
	if !reflect.DeepEqual(*again, *got) {
		t.Fatalf("the canonical form is not a fixed point:\n%+v\n%+v", *again, *got)
	}
}

// TestCreateCustomScheduleRefusesAnEmptySet pins the refusal migrations/00052
// argues for at length: an empty set must not be read as "all" or as "never",
// because those two sit one keystroke apart and are indistinguishable on screen.
//
// 🔴 Driven over REAL HTTP through the whole router, not against the domain
// function, because "required according to cadence" is not expressible in the
// OpenAPI schema — it lives only in the field descriptions, so no generated
// client will ever enforce it and this server is the only thing that does.
//
// Red when: an empty (or omitted) set for `custom` lands, leaving a schedule
// with a cadence and no times.
func TestCreateCustomScheduleRefusesAnEmptySet(t *testing.T) {
	srv, secret, _ := scheduledStack(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")
	url := srv.URL + "/api/members/mira/scheduled-messages"

	full := `"cadence":"custom","timezone":"UTC","body":"tick"`
	cases := []struct{ name, body string }{
		{"custom_days empty", `{` + full + `,"custom_days":[],"custom_hours":[9],"custom_minutes":[0]}`},
		{"custom_hours empty", `{` + full + `,"custom_days":[1],"custom_hours":[],"custom_minutes":[0]}`},
		{"custom_minutes empty", `{` + full + `,"custom_days":[1],"custom_hours":[9],"custom_minutes":[]}`},
		{"custom_days omitted", `{` + full + `,"custom_hours":[9],"custom_minutes":[0]}`},
		{"custom_hours omitted", `{` + full + `,"custom_days":[1],"custom_minutes":[0]}`},
		{"custom_minutes omitted", `{` + full + `,"custom_days":[1],"custom_hours":[9]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, resp := doJSON(t, "POST", url, ownerTok, tc.body)
			if status != 422 {
				t.Fatalf("want 422, got %d %v — a custom schedule landed with no times", status, resp)
			}
		})
	}

	// Positive control: all three stated is accepted, so the refusals above are
	// not just "custom is rejected outright".
	status, resp := doJSON(t, "POST", url, ownerTok,
		`{`+full+`,"custom_days":[1],"custom_hours":[9],"custom_minutes":[0]}`)
	if status != 200 {
		t.Fatalf("a fully stated custom schedule: want 200, got %d %v", status, resp)
	}
}

// TestCreateCustomScheduleRefusesOutOfRangeSetValues keeps an unusable value out
// of the table rather than letting the slot arithmetic meet it later. The
// boundary is enforced in ValidateScheduledMessageCustomSets (domain.go), called
// from validateScheduledMessage for `custom` only.
//
// Red when: the range check is dropped or the bounds slip — hour 24 and minute
// 60 are the off-by-one a wrapping mental model produces, and day 0 / day 32 are
// the two ends of the 1-31 window.
func TestCreateCustomScheduleRefusesOutOfRangeSetValues(t *testing.T) {
	srv, secret, _ := scheduledStack(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")
	url := srv.URL + "/api/members/mira/scheduled-messages"

	cases := []struct {
		name                 string
		days, hours, minutes []int
	}{
		{"day 0", []int{0}, []int{9}, []int{0}},
		{"day 32", []int{32}, []int{9}, []int{0}},
		{"hour 24", []int{1}, []int{24}, []int{0}},
		{"hour 25", []int{1}, []int{25}, []int{0}},
		{"minute 60", []int{1}, []int{9}, []int{60}},
		{"a legal value beside an illegal one", []int{1, 32}, []int{9}, []int{0}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, resp := doJSON(t, "POST", url, ownerTok,
				customSchedulePayload(tc.days, tc.hours, tc.minutes, "UTC"))
			if status != 422 {
				t.Fatalf("want 422, got %d %v — the value was stored for the scheduler to meet later",
					status, resp)
			}
		})
	}

	// The ends of each window are LEGAL — the sentinel against a well-meaning
	// tighten that would make day 31 or minute 59 unschedulable.
	status, resp := doJSON(t, "POST", url, ownerTok,
		customSchedulePayload([]int{1, 31}, []int{0, 23}, []int{0, 59}, "UTC"))
	if status != 200 {
		t.Fatalf("the boundary values must be accepted, got %d %v", status, resp)
	}
}

// TestScheduledMessageWallClockIsRequiredExceptForCustom pins the conditional
// requirement that came with making hour/minute optional on the wire.
//
// 🔴 Red when: a missing hour is folded to 0. A daily schedule then fires at
// midnight — a time nobody chose — and it looks exactly like one that was asked
// to run at midnight, so nothing anywhere says otherwise.
func TestScheduledMessageWallClockIsRequiredExceptForCustom(t *testing.T) {
	srv, secret, api := scheduledStack(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")
	url := srv.URL + "/api/members/mira/scheduled-messages"

	for _, cadence := range []string{"daily", "weekly", "monthly"} {
		t.Run(cadence+" without hour", func(t *testing.T) {
			status, resp := doJSON(t, "POST", url, ownerTok,
				`{"body":"x","cadence":"`+cadence+`","minute":0,"timezone":"UTC"}`)
			if status != 422 {
				t.Fatalf("want 422, got %d %v", status, resp)
			}
		})
		t.Run(cadence+" without minute", func(t *testing.T) {
			status, resp := doJSON(t, "POST", url, ownerTok,
				`{"body":"x","cadence":"`+cadence+`","hour":9,"timezone":"UTC"}`)
			if status != 422 {
				t.Fatalf("want 422, got %d %v", status, resp)
			}
		})
	}

	// `custom` reads neither, so it may omit both — and what lands is the 0/0
	// default it never looks at.
	status, created := doJSON(t, "POST", url, ownerTok,
		customSchedulePayload([]int{1}, []int{9}, []int{0}, "UTC"))
	if status != 200 {
		t.Fatalf("custom without hour/minute: want 200, got %d %v", status, created)
	}
	id, _ := created["id"].(string)
	stored, err := api.dal.GetScheduledMessage(id)
	if err != nil || stored == nil {
		t.Fatalf("reload: %v %v", stored, err)
	}
	if stored.Hour != 0 || stored.Minute != 0 {
		t.Fatalf("custom stored hour/minute %d/%d, want 0/0", stored.Hour, stored.Minute)
	}

	// 🔴 And leaving `custom` must STATE the wall clock rather than inherit that
	// 0/0 — otherwise the toggle alone silently re-aims the schedule at midnight.
	path := url + "/" + id
	if status, resp := doJSON(t, "PATCH", path, ownerTok, `{"cadence":"daily"}`); status != 422 {
		t.Fatalf("switching away from custom without an hour: want 422, got %d %v", status, resp)
	}
	if status, resp := doJSON(t, "PATCH", path, ownerTok, `{"cadence":"daily","hour":9,"minute":30}`); status != 200 {
		t.Fatalf("switching away from custom WITH an hour: want 200, got %d %v", status, resp)
	}
}

// TestCustomFiresEveryTwentyMinutes is the owner's own case (card
// rc-4acc4013a0ae): custom_minutes {0,20,40} with every hour and every day
// listed. It is the reason this cadence exists, and it is the first schedule in
// the system that fires more than once a day.
//
// Red when: the custom branch returns the day's FIRST reading rather than the
// most recent one, or when consecutive ticks fail to advance (the delivery
// would then happen once and stop, with the cursor parked).
func TestCustomFiresEveryTwentyMinutes(t *testing.T) {
	taipei := mustLoadZone(t, "Asia/Taipei")
	sm := ScheduledMessage{
		ID: "sch-20min", MemberID: "mira", Body: "twenty minutes",
		Cadence:     ScheduledMessageCadenceCustom,
		CustomDays:  intRange(1, 31),
		CustomHours: intRange(0, 23), CustomMinutes: []int{0, 20, 40},
		Timezone: "Asia/Taipei",
	}

	// The arithmetic, at a fixed `now`.
	for _, tc := range []struct{ now, want string }{
		{"2026-08-10T10:25", "2026-08-10T10:20+08:00"},
		{"2026-08-10T10:20", "2026-08-10T10:20+08:00"}, // the reading itself has elapsed
		{"2026-08-10T10:19", "2026-08-10T10:00+08:00"},
		{"2026-08-10T00:05", "2026-08-10T00:00+08:00"},
	} {
		at, err := time.ParseInLocation("2006-01-02T15:04", tc.now, taipei)
		if err != nil {
			t.Fatalf("parse %s: %v", tc.now, err)
		}
		slot, ok := mostRecentSlot(sm, at)
		if !ok {
			t.Fatalf("no slot at %s — a schedule listing every day and every hour always has one", tc.now)
		}
		if got := slotKey(slot); got != tc.want {
			t.Fatalf("at %s the most recent slot is %s, want %s", tc.now, got, tc.want)
		}
	}

	// The day scan really walks BACK a day when today has nothing yet: drop the
	// :00 reading and 00:05 has to reach yesterday's last one.
	noTopOfHour := sm
	noTopOfHour.CustomMinutes = []int{20, 40}
	slot, ok := mostRecentSlot(noTopOfHour, time.Date(2026, time.August, 10, 0, 5, 0, 0, taipei))
	if !ok {
		t.Fatal("no slot just after midnight — the scan did not step back a day")
	}
	if got := slotKey(slot); got != "2026-08-09T23:40+08:00" {
		t.Fatalf("just after midnight the most recent slot is %s, want 2026-08-09T23:40+08:00", got)
	}

	// And three real ticks deliver three STRICTLY INCREASING slots.
	_, _, api := scheduledStack(t)
	start := time.Date(2026, time.August, 10, 10, 25, 0, 0, taipei)
	seedScheduleWithCurrentCursor(t, api, sm, start)
	slots := tickEvery(t, api, sm.ID, start.Add(time.Minute),
		time.Date(2026, time.August, 10, 11, 25, 0, 0, taipei), time.Minute)
	want := []string{"2026-08-10T10:40+08:00", "2026-08-10T11:00+08:00", "2026-08-10T11:20+08:00"}
	if !reflect.DeepEqual(slots, want) {
		t.Fatalf("an hour of ticks delivered %v, want %v", slots, want)
	}
	for i := 1; i < len(slots); i++ {
		if !mustParseSlot(t, slots[i]).After(mustParseSlot(t, slots[i-1])) {
			t.Fatalf("slot %s did not advance past %s — the cursor is not a ratchet", slots[i], slots[i-1])
		}
	}
}

// TestCustomSkipsADateTheMonthDoesNotHave pins the RFC 5545 rule for the set
// form: a month that has no 31st contributes nothing, it is NOT re-aimed at the
// 28th.
//
// Red when: the day scan clamps onto the last day of the month. February would
// then deliver on the 28th — a day of the owner's month that was never chosen —
// and the log would call it a correct delivery.
func TestCustomSkipsADateTheMonthDoesNotHave(t *testing.T) {
	_, _, api := scheduledStack(t)
	sm := ScheduledMessage{
		ID: "sch-31st", MemberID: "mira", Body: "month end",
		Cadence:    ScheduledMessageCadenceCustom,
		CustomDays: []int{31}, CustomHours: []int{9}, CustomMinutes: []int{0},
		Timezone: "UTC",
	}
	// Mid-February: the most recent occurrence is 31 JANUARY. Anything else means
	// the scan either clamped or gave up short.
	at := time.Date(2026, time.February, 15, 12, 0, 0, 0, time.UTC)
	slot, ok := mostRecentSlot(sm, at)
	if !ok {
		t.Fatal("no slot in mid-February — the scan gave up before reaching 31 January")
	}
	if got := slotKey(slot); got != "2026-01-31T09:00+00:00" {
		t.Fatalf("mid-February resolves to %s, want 2026-01-31T09:00+00:00", got)
	}

	// And the whole of February delivers NOTHING, while 31 March does.
	start := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	seedScheduleWithCurrentCursor(t, api, sm, start)
	if slots := tickEvery(t, api, sm.ID, start,
		time.Date(2026, time.February, 28, 23, 0, 0, 0, time.UTC), time.Hour); len(slots) != 0 {
		t.Fatalf("February delivered %v — a month without the listed day must be skipped entirely", slots)
	}
	slots := tickEvery(t, api, sm.ID, time.Date(2026, time.March, 31, 9, 0, 0, 0, time.UTC),
		time.Date(2026, time.March, 31, 12, 0, 0, 0, time.UTC), time.Hour)
	if want := []string{"2026-03-31T09:00+00:00"}; !reflect.DeepEqual(slots, want) {
		t.Fatalf("31 March delivered %v, want %v", slots, want)
	}

	// 🔴 And the absent day costs ONLY ITSELF. Membership is decided date by
	// date, so [1,15,31] in February still fires on the 1st and the 15th — a
	// month-level skip (the phrasing `monthly` uses, where the set is a single
	// day) would take those two down with it, and a schedule that lost most of
	// its month would look like one that was never armed.
	_, _, api2 := scheduledStack(t)
	many := ScheduledMessage{
		ID: "sch-1-15-31", MemberID: "mira", Body: "three days",
		Cadence:    ScheduledMessageCadenceCustom,
		CustomDays: []int{1, 15, 31}, CustomHours: []int{9}, CustomMinutes: []int{0},
		Timezone: "UTC",
	}
	febStart := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	seedScheduleWithCurrentCursor(t, api2, many, febStart)
	febSlots := tickEvery(t, api2, many.ID, febStart,
		time.Date(2026, time.February, 28, 23, 0, 0, 0, time.UTC), time.Hour)
	wantFeb := []string{"2026-02-01T09:00+00:00", "2026-02-15T09:00+00:00"}
	if !reflect.DeepEqual(febSlots, wantFeb) {
		t.Fatalf("February delivered %v, want %v — the missing 31st must cost only itself", febSlots, wantFeb)
	}
}

// TestCustomDropsAWallClockTheZoneSkipped is the DST divergence sentinel, and
// the most important test in this file.
//
// America/New_York springs forward on 2026-03-08: 02:00 becomes 03:00, so no
// reading between 02:00 and 02:59 exists on that date. A `custom` schedule
// naming 02:00 and 02:30 therefore has NOTHING on that date — those readings are
// DROPPED, not searched forward.
//
// 🔴 Red when the calendar cadences' forward search is reused here. Both named
// readings would then resolve to 03:00, producing the SAME slotKey, and the
// cursor would silently merge two intended deliveries into one — which is
// exactly the silent failure this ticket exists to prevent, and which no
// delivery COUNT can distinguish from correct behaviour once it has happened.
// Making both readings fall in the gap is what gives this test its
// discriminating power: 0 deliveries under the rule, 1 under the merge.
func TestCustomDropsAWallClockTheZoneSkipped(t *testing.T) {
	newYork := mustLoadZone(t, "America/New_York")
	sm := ScheduledMessage{
		ID: "sch-spring", MemberID: "mira", Body: "in the gap",
		Cadence:    ScheduledMessageCadenceCustom,
		CustomDays: []int{8}, CustomHours: []int{2}, CustomMinutes: []int{0, 30},
		Timezone: "America/New_York",
	}
	// Late on the transition day, the most recent occurrence is a MONTH earlier.
	at := time.Date(2026, time.March, 8, 23, 0, 0, 0, newYork)
	slot, ok := mostRecentSlot(sm, at)
	if !ok {
		t.Fatal("no slot at all — the scan should still reach 8 February")
	}
	if got := slotKey(slot); got != "2026-02-08T02:30-05:00" {
		t.Fatalf("the transition day resolved to %s; want 2026-02-08T02:30-05:00. "+
			"A March answer means the skipped readings were searched FORWARD onto a "+
			"reading the schedule never named", got)
	}

	_, _, api := scheduledStack(t)
	start := time.Date(2026, time.March, 8, 0, 0, 0, 0, newYork)
	seedScheduleWithCurrentCursor(t, api, sm, start)
	if slots := tickEvery(t, api, sm.ID, start,
		time.Date(2026, time.March, 8, 23, 0, 0, 0, newYork), time.Minute*10); len(slots) != 0 {
		t.Fatalf("the spring-forward day delivered %v — both named readings sit inside "+
			"the deleted hour, so the day must contribute nothing", slots)
	}

	// Positive control: the very next month's 8th, with no transition, delivers
	// both readings — so the zero above is the rule, not a dead schedule.
	slots := tickEvery(t, api, sm.ID, time.Date(2026, time.April, 8, 0, 0, 0, 0, newYork),
		time.Date(2026, time.April, 8, 12, 0, 0, 0, newYork), time.Minute*10)
	want := []string{"2026-04-08T02:00-04:00", "2026-04-08T02:30-04:00"}
	if !reflect.DeepEqual(slots, want) {
		t.Fatalf("an ordinary 8th delivered %v, want %v", slots, want)
	}
}

// TestCustomDeliversAnAmbiguousWallClockOnce pins the AUTUMN behaviour as it
// stands — deliberately, because it is not being changed here.
//
// America/New_York falls back on 2026-11-01: 01:00 happens twice, once at -04:00
// and again at -05:00. time.Date resolves an ambiguous reading to the EARLIER
// offset, always, so the second pass reconstructs the first instant, produces
// the same slotKey, and the cursor refuses it. The reading fires ONCE.
//
// That resolution lives in the shared readBack/time.Date path all four cadences
// use; changing it here would move all four. This test exists so the behaviour
// is a recorded decision — red if anything ever makes the repeated hour deliver
// twice, which in the chat log is indistinguishable from a correct delivery.
func TestCustomDeliversAnAmbiguousWallClockOnce(t *testing.T) {
	newYork := mustLoadZone(t, "America/New_York")
	_, _, api := scheduledStack(t)
	sm := ScheduledMessage{
		ID: "sch-autumn", MemberID: "mira", Body: "the repeated hour",
		Cadence:    ScheduledMessageCadenceCustom,
		CustomDays: []int{1}, CustomHours: []int{1}, CustomMinutes: []int{0},
		Timezone: "America/New_York",
	}
	start := time.Date(2026, time.November, 1, 0, 0, 0, 0, newYork)
	seedScheduleWithCurrentCursor(t, api, sm, start)
	slots := tickEvery(t, api, sm.ID, start,
		time.Date(2026, time.November, 1, 23, 0, 0, 0, newYork), time.Minute*10)
	want := []string{"2026-11-01T01:00-04:00"}
	if !reflect.DeepEqual(slots, want) {
		t.Fatalf("the fall-back day delivered %v, want %v — 01:00 occurs twice and must "+
			"go out once, at the EARLIER offset", slots, want)
	}
}

// TestPatchCustomSetsReAimOnlyOnARealChange is the cursor sentinel for the three
// new fields.
//
// Red when: the sets are compared as SENT rather than in canonical form. The
// cockpit's per-row editor posts the whole form on every save, so a differing
// checkbox order would re-aim the cursor — and a re-aim landing between a slot
// elapsing and the next tick swallows that delivery permanently, with no error,
// no log line, and a card that looks entirely normal.
func TestPatchCustomSetsReAimOnlyOnARealChange(t *testing.T) {
	srv, secret, api := scheduledStack(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")
	status, created := doJSON(t, "POST", srv.URL+"/api/members/mira/scheduled-messages",
		ownerTok, customSchedulePayload(intRange(1, 31), intRange(0, 23), []int{0, 20, 40}, "Asia/Taipei"))
	if status != 200 {
		t.Fatalf("create: %d %v", status, created)
	}
	id, _ := created["id"].(string)
	path := srv.URL + "/api/members/mira/scheduled-messages/" + id

	// The response already carries the canonical order, whatever was sent.
	if got := created["custom_minutes"]; !reflect.DeepEqual(got, []any{0.0, 20.0, 40.0}) {
		t.Fatalf("create echoed custom_minutes as %v, want the canonical [0 20 40]", got)
	}

	plant := func() {
		t.Helper()
		stored, _ := api.dal.GetScheduledMessage(id)
		stored.LastFiredSlot = "1999-01-01T00:00+08:00"
		if err := api.dal.PutScheduledMessage(*stored); err != nil {
			t.Fatalf("plant cursor: %v", err)
		}
	}

	// Same choice, different order — nothing moves.
	plant()
	status, patched := doJSON(t, "PATCH", path, ownerTok, `{"custom_minutes":[20,0,40]}`)
	if status != 200 {
		t.Fatalf("re-sending the same set: %d %v", status, patched)
	}
	if got, _ := patched["last_fired_slot"].(string); got != "1999-01-01T00:00+08:00" {
		t.Fatalf("re-sending [20,0,40] over a stored [0,20,40] moved the cursor to %q — "+
			"saving the form unchanged just swallowed a delivery", got)
	}

	// A genuinely different set re-aims.
	plant()
	status, patched = doJSON(t, "PATCH", path, ownerTok, `{"custom_minutes":[0,30]}`)
	if status != 200 {
		t.Fatalf("changing the set: %d %v", status, patched)
	}
	got, _ := patched["last_fired_slot"].(string)
	if got == "1999-01-01T00:00+08:00" || got == "" {
		t.Fatalf("changing the set left the cursor at %q — the next tick would deliver "+
			"the slot the edit just crossed", got)
	}
	if slot := mustParseSlot(t, got); slot.After(time.Now()) {
		t.Fatalf("the re-aimed cursor %q is in the FUTURE", got)
	}
}

// TestPatchAwayFromCustomKeepsTheStoredSets: flipping the cadence must not
// destroy the owner's choice.
//
// Red when: leaving `custom` clears the three columns. The sets are gone
// irreversibly, as a side effect of a toggle, and switching back presents an
// empty form as though nothing was ever chosen.
func TestPatchAwayFromCustomKeepsTheStoredSets(t *testing.T) {
	srv, secret, api := scheduledStack(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")
	status, created := doJSON(t, "POST", srv.URL+"/api/members/mira/scheduled-messages",
		ownerTok, customSchedulePayload([]int{1, 15}, []int{9}, []int{0, 30}, "UTC"))
	if status != 200 {
		t.Fatalf("create: %d %v", status, created)
	}
	id, _ := created["id"].(string)
	path := srv.URL + "/api/members/mira/scheduled-messages/" + id

	if status, resp := doJSON(t, "PATCH", path, ownerTok,
		`{"cadence":"daily","hour":9,"minute":0}`); status != 200 {
		t.Fatalf("switch away: %d %v", status, resp)
	}
	stored, err := api.dal.GetScheduledMessage(id)
	if err != nil || stored == nil {
		t.Fatalf("reload: %v %v", stored, err)
	}
	if !reflect.DeepEqual(stored.CustomMinutes, []int{0, 30}) {
		t.Fatalf("leaving custom wiped custom_minutes (now %v) — the choice cannot be "+
			"recovered and switching back would present an empty form", stored.CustomMinutes)
	}

	// Switching back needs nothing more than the cadence: the sets are still there.
	if status, resp := doJSON(t, "PATCH", path, ownerTok, `{"cadence":"custom"}`); status != 200 {
		t.Fatalf("switch back: %d %v — the stored sets should satisfy the requirement", status, resp)
	}
}

// TestScheduledMessageDTOAlwaysCarriesTheSets pins the honest-empty wire shape:
// the three fields are present for EVERY cadence.
//
// Red when: they are omitted (or null) for non-custom rows. A reader then cannot
// tell "this schedule has no sets" from "this server does not know about sets",
// which are answers to two different questions.
func TestScheduledMessageDTOAlwaysCarriesTheSets(t *testing.T) {
	srv, secret, _ := scheduledStack(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")
	status, created := doJSON(t, "POST", srv.URL+"/api/members/mira/scheduled-messages",
		ownerTok, `{"body":"x","cadence":"daily","hour":9,"minute":0,"timezone":"UTC"}`)
	if status != 200 {
		t.Fatalf("create: %d %v", status, created)
	}
	for _, field := range []string{"custom_days", "custom_hours", "custom_minutes"} {
		got, present := created[field]
		if !present {
			t.Fatalf("%s is absent from a daily schedule's response", field)
		}
		if !reflect.DeepEqual(got, []any{}) {
			t.Fatalf("%s is %v on a daily schedule, want an empty array", field, got)
		}
	}
}
