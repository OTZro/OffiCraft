package main

// scheduled_message_months_t49e7_test.go — T-49e7 round 2: the MONTH set.
//
// Same rule as its two siblings: every test names the one bug it goes red on.
// The load-bearing ones are the two that pin the asymmetry between an OMITTED
// custom_months (all twelve, so a client written before the field keeps
// working) and an EXPLICIT EMPTY one (422, because "never fires" and "fires
// every month" must not be one keystroke apart). Those two readings sit next to
// each other in the request and are answered oppositely, so each has its own
// test rather than sharing a table.

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

// listSchedules reads the LIST face, which serialises through the same DTO
// builder as the single-schedule responses but is a separate reader — a set
// dropped from one and not the other is exactly the kind of asymmetry a test
// that only ever reads back a create would miss.
func listSchedules(t *testing.T, url, token string) []map[string]any {
	t.Helper()
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("list schedules: got %d", resp.StatusCode)
	}
	var out []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	return out
}

// customMonthlyPayload renders a create body for a `custom` schedule that names
// its months. The sibling helper customSchedulePayload deliberately omits them,
// which is what makes it the fixture for the old-client case.
func customMonthlyPayload(months, days, hours, minutes []int, tz string) string {
	return `{"body":"tick","cadence":"custom","custom_months":` + jsonInts(months) +
		`,"custom_days":` + jsonInts(days) +
		`,"custom_hours":` + jsonInts(hours) +
		`,"custom_minutes":` + jsonInts(minutes) +
		`,"timezone":"` + tz + `"}`
}

// TestCustomMonthsRoundTripEveryDirection walks the month set through every
// read/write direction it has: create over HTTP, the response body, the stored
// row, the list face, a PATCH that changes it, and a PATCH that leaves it
// alone.
//
// 🔴 The last direction is the one worth the test. `custom_months` is the only
// set whose absence carries a meaning, and the meaning it carries on a CREATE
// (all twelve) is not the meaning it carries on a PATCH (unchanged). Confusing
// the two would silently widen a schedule the owner narrowed — a schedule that
// fires MORE often than asked, on a card that still reads correctly, because
// the row itself is what was rewritten.
//
// Red when: the set is dropped by any writer, not emitted by any reader, or an
// omitted field on PATCH is resolved to all twelve instead of left alone.
func TestCustomMonthsRoundTripEveryDirection(t *testing.T) {
	srv, secret, api := scheduledStack(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")
	url := srv.URL + "/api/members/mira/scheduled-messages"

	// Out of order and with a duplicate, so the canonical form is exercised on
	// the way in rather than assumed.
	status, created := doJSON(t, "POST", url, ownerTok,
		customMonthlyPayload([]int{9, 3, 12, 3, 6}, []int{1, 15}, []int{9}, []int{0}, "Asia/Taipei"))
	if status != 200 {
		t.Fatalf("create with months: want 200, got %d %v", status, created)
	}
	id, _ := created["id"].(string)
	wantCreated := []any{3.0, 6.0, 9.0, 12.0}
	if got := created["custom_months"]; !reflect.DeepEqual(got, wantCreated) {
		t.Fatalf("the create response returned custom_months %v, want the canonical %v", got, wantCreated)
	}

	stored, err := api.dal.GetScheduledMessage(id)
	if err != nil || stored == nil {
		t.Fatalf("reload: %v %v", stored, err)
	}
	if want := []int{3, 6, 9, 12}; !reflect.DeepEqual(stored.CustomMonths, want) {
		t.Fatalf("the stored row holds months %v, want %v", stored.CustomMonths, want)
	}

	listed := listSchedules(t, url, ownerTok)
	if len(listed) != 1 {
		t.Fatalf("list returned %d rows, want 1", len(listed))
	}
	if got := listed[0]["custom_months"]; !reflect.DeepEqual(got, wantCreated) {
		t.Fatalf("the list face returned custom_months %v, want %v", got, wantCreated)
	}

	// A PATCH that STATES months replaces them.
	path := url + "/" + id
	status, patched := doJSON(t, "PATCH", path, ownerTok, `{"custom_months":[2,8]}`)
	if status != 200 {
		t.Fatalf("patch months: want 200, got %d %v", status, patched)
	}
	if got, want := patched["custom_months"], []any{2.0, 8.0}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after patching months the response says %v, want %v", got, want)
	}

	// 🔴 A PATCH that does NOT mention months must leave them exactly as they
	// are — absent means unchanged here, never "re-assert all twelve".
	status, quiet := doJSON(t, "PATCH", path, ownerTok, `{"label":"renamed"}`)
	if status != 200 {
		t.Fatalf("patch label: want 200, got %d %v", status, quiet)
	}
	if got, want := quiet["custom_months"], []any{2.0, 8.0}; !reflect.DeepEqual(got, want) {
		t.Fatalf("a PATCH that never mentioned months changed them to %v, want %v left alone. "+
			"An omitted custom_months means all twelve only where a whole custom row has to be "+
			"produced from nothing (a create, or a switch TO custom on a row carrying no months); "+
			"everywhere else it means unchanged, and widening a narrowed schedule is silent",
			got, want)
	}
}

// TestCustomMonthsOmittedMeansEveryMonth is the backward-compatibility
// sentinel, and it is the reason the month set is optional at all.
//
// A client written before this field never sends it, and its schedules already
// meant "every month" — that is what a day × hour × minute intersection with no
// month filter does. Refusing such a create, or landing it with no months,
// would stop every one of those schedules dead: no error, no log line, a card
// that looks entirely normal.
//
// Red when: an omitted custom_months lands as an empty set (the schedule then
// never fires) or is refused (the old client breaks outright).
func TestCustomMonthsOmittedMeansEveryMonth(t *testing.T) {
	srv, secret, api := scheduledStack(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")
	url := srv.URL + "/api/members/mira/scheduled-messages"

	// customSchedulePayload is the pre-round-2 body, verbatim: no month field.
	status, created := doJSON(t, "POST", url, ownerTok,
		customSchedulePayload([]int{1, 15}, []int{9}, []int{0}, "UTC"))
	if status != 200 {
		t.Fatalf("a create with no custom_months: want 200, got %d %v — "+
			"every client written before the field sends exactly this body", status, created)
	}
	id, _ := created["id"].(string)
	stored, err := api.dal.GetScheduledMessage(id)
	if err != nil || stored == nil {
		t.Fatalf("reload: %v %v", stored, err)
	}
	if want := intRange(1, 12); !reflect.DeepEqual(stored.CustomMonths, want) {
		t.Fatalf("an omitted custom_months stored %v, want every month %v. An empty set here is "+
			"a schedule that never fires again, and nothing anywhere would say so",
			stored.CustomMonths, want)
	}

	// The behaviour, not just the column: it really does fire in a month it was
	// never told about.
	slot, ok := mostRecentSlot(*stored, time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC))
	if !ok || slotKey(slot) != "2026-07-15T09:00+00:00" {
		t.Fatalf("a schedule created without months produced %q (ok=%v) in July, want 2026-07-15T09:00+00:00",
			slotKey(slot), ok)
	}

	// 🔴 The same rule on the OTHER request that must produce a whole custom row
	// from nothing: a PATCH switching a never-custom schedule TO `custom`. An
	// old client does that too, and it names the three sets it knows about.
	status, daily := doJSON(t, "POST", url, ownerTok,
		`{"body":"x","cadence":"daily","hour":9,"minute":0,"timezone":"UTC"}`)
	if status != 200 {
		t.Fatalf("seed daily: %d %v", status, daily)
	}
	dailyID, _ := daily["id"].(string)
	status, switched := doJSON(t, "PATCH", url+"/"+dailyID, ownerTok,
		`{"cadence":"custom","custom_days":[1],"custom_hours":[9],"custom_minutes":[0]}`)
	if status != 200 {
		t.Fatalf("switching to custom without months: want 200, got %d %v", status, switched)
	}
	if got, want := switched["custom_months"], []any{
		1.0, 2.0, 3.0, 4.0, 5.0, 6.0, 7.0, 8.0, 9.0, 10.0, 11.0, 12.0,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("switching to custom without months landed %v, want every month", got)
	}
}

// TestCustomMonthsRefusesAnExplicitEmptySet is the other half of the pair
// above, and the two must not be collapsed: an ABSENT field and an EMPTY array
// are different requests and get opposite answers.
//
// 🔴 The refusal is the rule migrations/00052 argues for and this field does not
// get to opt out of: a schedule that never fires and one that fires every month
// must not be one keystroke apart. The omitted case is answered by the field
// being absent, which is a shape a slipped keystroke cannot produce.
//
// Red when: [] is quietly read as "all twelve" (the owner asked for silence and
// got a message every month) or as "never" (a schedule that is armed, enabled,
// and structurally dead).
func TestCustomMonthsRefusesAnExplicitEmptySet(t *testing.T) {
	srv, secret, api := scheduledStack(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")
	url := srv.URL + "/api/members/mira/scheduled-messages"

	status, resp := doJSON(t, "POST", url, ownerTok,
		customMonthlyPayload([]int{}, []int{1}, []int{9}, []int{0}, "UTC"))
	if status != 422 {
		t.Fatalf("create with custom_months []: want 422, got %d %v", status, resp)
	}

	// And on the edit face, where the same keystroke is available.
	status, created := doJSON(t, "POST", url, ownerTok,
		customMonthlyPayload([]int{3}, []int{1}, []int{9}, []int{0}, "UTC"))
	if status != 200 {
		t.Fatalf("seed: %d %v", status, created)
	}
	id, _ := created["id"].(string)
	if status, resp := doJSON(t, "PATCH", url+"/"+id, ownerTok, `{"custom_months":[]}`); status != 422 {
		t.Fatalf("patch custom_months to []: want 422, got %d %v", status, resp)
	}
	stored, err := api.dal.GetScheduledMessage(id)
	if err != nil || stored == nil {
		t.Fatalf("reload: %v %v", stored, err)
	}
	if want := []int{3}; !reflect.DeepEqual(stored.CustomMonths, want) {
		t.Fatalf("the refused PATCH left months as %v, want the stored %v untouched", stored.CustomMonths, want)
	}
}

// TestCustomMonthsRefusesOutOfRangeValues keeps an unschedulable month out of
// the table rather than letting the slot arithmetic meet it later, where it is
// simply a condition that never holds.
//
// Red when: the 1-12 window slips. 0 and 13 are the two ends a zero-based or
// wrapping mental model produces, and month 12 is the boundary a well-meaning
// tighten would make unschedulable.
func TestCustomMonthsRefusesOutOfRangeValues(t *testing.T) {
	srv, secret, _ := scheduledStack(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")
	url := srv.URL + "/api/members/mira/scheduled-messages"

	for _, tc := range []struct {
		name   string
		months []int
	}{
		{"month 0", []int{0}},
		{"month 13", []int{13}},
		{"a legal month beside an illegal one", []int{6, 13}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, resp := doJSON(t, "POST", url, ownerTok,
				customMonthlyPayload(tc.months, []int{1}, []int{9}, []int{0}, "UTC"))
			if status != 422 {
				t.Fatalf("want 422, got %d %v — the value was stored for the scheduler to meet later",
					status, resp)
			}
		})
	}

	status, resp := doJSON(t, "POST", url, ownerTok,
		customMonthlyPayload([]int{1, 12}, []int{1}, []int{9}, []int{0}, "UTC"))
	if status != 200 {
		t.Fatalf("the ends of the window must be accepted, got %d %v", status, resp)
	}
}

// TestCustomMonthsAndDaysThatNeverMeetAreRefused closes the second door onto
// the same room the empty-set 422 guards: a schedule that is structurally
// incapable of ever firing.
//
// Round 1 could not build one — with only a day set, any non-empty choice of
// days eventually meets a month that has it. The MONTH set is what makes an
// empty intersection expressible, and every value in it is individually legal,
// so nothing downstream ever complains: the cockpit renders 每年 2 月 · 每月 31
// 號, every word of which is true, and not one message is ever delivered.
//
// 🔴 The line is drawn at "no (month, day) pair is possible in ANY year", which
// is what keeps months {2} × days {29} — a deliberate leap-year schedule the
// spec spells out — alive. That positive sample is asserted here rather than
// left implicit: a well-meaning tighten to 28 would kill it silently.
//
// Red when: an unschedulable month × day pair is accepted (the owner arms a
// schedule that can never speak), or the February ceiling slips to 28 (the
// leap-year schedule becomes unexpressible).
func TestCustomMonthsAndDaysThatNeverMeetAreRefused(t *testing.T) {
	srv, secret, api := scheduledStack(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")
	url := srv.URL + "/api/members/mira/scheduled-messages"

	for _, tc := range []struct {
		name         string
		months, days []int
	}{
		{"February and the 30th", []int{2}, []int{30}},
		{"February and the 31st", []int{2}, []int{31}},
		{"the four short months and the 31st", []int{4, 6, 9, 11}, []int{31}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, resp := doJSON(t, "POST", url, ownerTok,
				customMonthlyPayload(tc.months, tc.days, []int{9}, []int{0}, "UTC"))
			if status != 422 {
				t.Fatalf("want 422, got %d %v — this schedule passes validation and delivers nothing, ever",
					status, resp)
			}
			envelope, _ := resp["error"].(map[string]any)
			detail, _ := envelope["message"].(string)
			for _, want := range []string{"custom_months", "custom_days", "never occur together"} {
				if !strings.Contains(detail, want) {
					t.Fatalf("the refusal must say why it is impossible; %q is missing from %q", want, detail)
				}
			}
		})
	}

	// The edit face too: the same impossible pair must not be reachable by
	// patching a schedule that was legal when it was created.
	status, created := doJSON(t, "POST", url, ownerTok,
		customMonthlyPayload([]int{1, 2}, []int{31}, []int{9}, []int{0}, "UTC"))
	if status != 200 {
		t.Fatalf("seed (January has a 31st, so this pair is fine): %d %v", status, created)
	}
	id, _ := created["id"].(string)
	if status, resp := doJSON(t, "PATCH", url+"/"+id, ownerTok, `{"custom_months":[2]}`); status != 422 {
		t.Fatalf("patching the months down to February alone: want 422, got %d %v", status, resp)
	}
	stored, err := api.dal.GetScheduledMessage(id)
	if err != nil || stored == nil {
		t.Fatalf("reload: %v %v", stored, err)
	}
	if want := []int{1, 2}; !reflect.DeepEqual(stored.CustomMonths, want) {
		t.Fatalf("the refused PATCH left months as %v, want the stored %v untouched", stored.CustomMonths, want)
	}

	// 🔴 The positive sample. February with the 29th fires in leap years only,
	// and that is a schedule someone meant to write.
	status, leap := doJSON(t, "POST", url, ownerTok,
		customMonthlyPayload([]int{2}, []int{29}, []int{9}, []int{0}, "UTC"))
	if status != 200 {
		t.Fatalf("months {2} × days {29} is a deliberate leap-year schedule and must stay legal; got %d %v",
			status, leap)
	}
}

// TestCustomSkipsAMonthItWasNotGivenIsTheWholeMonth pins the month condition
// itself: an unselected month contributes NO readings at all, however many days
// and hours the other sets name.
//
// 🔴 Driven over a whole quarter of ticks rather than one call to
// mostRecentSlot, because the failure this guards is not "the wrong slot" but
// "a delivery that should not have happened". A schedule that fires in a month
// the owner excluded is indistinguishable, in the chat log, from one that was
// asked to.
//
// Red when: the month set is ignored, or read as a filter on something other
// than the calendar date being examined.
func TestCustomSkipsAMonthItWasNotGivenIsTheWholeMonth(t *testing.T) {
	_, _, api := scheduledStack(t)
	// Every day, every hour, on the hour — so if months were ignored this
	// schedule would deliver hundreds of times in the excluded month, not once.
	sm := ScheduledMessage{
		ID: "sch-q", MemberID: "mira", Body: "quarterly",
		Cadence:       ScheduledMessageCadenceCustom,
		CustomMonths:  []int{3, 6},
		CustomDays:    intRange(1, 31),
		CustomHours:   intRange(0, 23),
		CustomMinutes: []int{0},
		Timezone:      "UTC",
	}
	// Start inside March (a selected month) and run into April (excluded).
	start := time.Date(2026, time.March, 30, 0, 0, 0, 0, time.UTC)
	seedScheduleWithCurrentCursor(t, api, sm, start)
	slots := tickEvery(t, api, sm.ID, start,
		time.Date(2026, time.April, 3, 0, 0, 0, 0, time.UTC), 30*time.Minute)

	if len(slots) == 0 {
		t.Fatal("no deliveries at all — the fixture proves nothing about the excluded month " +
			"if the schedule never fired in the included one either")
	}
	for _, key := range slots {
		at, err := time.Parse(slotKeyLayout, key)
		if err != nil {
			t.Fatalf("undecodable slot %q: %v", key, err)
		}
		if at.Month() != time.March {
			t.Fatalf("delivered %s, in %s — a month this schedule excludes. Every reading in an "+
				"unselected month must be absent, not merely rare", key, at.Month())
		}
	}
	// The last delivery is March's last reading: the month ends the run rather
	// than the run ending early for some unrelated reason.
	if want := "2026-03-31T23:00+00:00"; slots[len(slots)-1] != want {
		t.Fatalf("the last delivery was %q, want %q — March's final reading", slots[len(slots)-1], want)
	}
}

// TestCustomCrossesAMonthBoundaryToTheRightSlot pins the walk-back across a
// month edge: on the first day of an EXCLUDED month, the most recent slot is
// the last reading of the previous INCLUDED one.
//
// 🔴 This is where a month test placed on `now` rather than on the DATE BEING
// EXAMINED goes wrong, and it goes wrong silently: the walk-back would refuse
// every candidate date because the current month is not in the set, report "no
// slot", and the schedule would simply stop delivering the moment an excluded
// month began — including the delivery that was still owed from the month
// before.
//
// Red when: the membership test reads the clock's month instead of the
// candidate date's.
func TestCustomCrossesAMonthBoundaryToTheRightSlot(t *testing.T) {
	sm := ScheduledMessage{
		ID: "sch-boundary", MemberID: "mira", Body: "edge",
		Cadence:       ScheduledMessageCadenceCustom,
		CustomMonths:  []int{1, 3},
		CustomDays:    []int{31},
		CustomHours:   []int{9},
		CustomMinutes: []int{0},
		Timezone:      "UTC",
	}
	for _, tc := range []struct{ name, now, want string }{
		// Standing in February — an excluded month with no 31st either — the
		// answer is 31 January, a month back.
		{"in an excluded month", "2026-02-14T12:00:00Z", "2026-01-31T09:00+00:00"},
		// The first instant of the excluded month still owes January's slot.
		{"the instant the excluded month began", "2026-02-01T00:00:00Z", "2026-01-31T09:00+00:00"},
		// Inside an included month, before its own reading: still January's.
		{"in an included month before its reading", "2026-03-31T08:59:00Z", "2026-01-31T09:00+00:00"},
		// And one minute later it is March's.
		{"in an included month after its reading", "2026-03-31T09:00:00Z", "2026-03-31T09:00+00:00"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now, err := time.Parse(time.RFC3339, tc.now)
			if err != nil {
				t.Fatalf("bad fixture: %v", err)
			}
			slot, ok := mostRecentSlot(sm, now)
			if !ok {
				t.Fatalf("no slot at %s — a schedule standing in a month it excludes still owes "+
					"the last reading of the month it does not", tc.now)
			}
			if got := slotKey(slot); got != tc.want {
				t.Fatalf("at %s the most recent slot is %q, want %q", tc.now, got, tc.want)
			}
		})
	}
}

// TestCustomMonthsComposeWithFebruaryAndLeapDays pins how the month set
// composes with the day set at the shortest month, including 29 February.
//
// 🔴 The rule is the SAME day-by-day one custom_days already follows and this
// test exists to stop it being special-cased: a listed day the month does not
// contain is dropped for that day only and never clamped. So months {2} × days
// {29} is a schedule that fires in leap years and is simply absent in the other
// three — NOT one that slides onto 28 February, which would deliver on a date
// the owner did not choose, in three years out of four, silently.
//
// Red when: February's missing days are clamped, or a month with no legal day
// at all is treated as an error rather than as an absence.
func TestCustomMonthsComposeWithFebruaryAndLeapDays(t *testing.T) {
	leapDay := ScheduledMessage{
		ID: "sch-leap", MemberID: "mira", Body: "leap",
		Cadence:       ScheduledMessageCadenceCustom,
		CustomMonths:  []int{2},
		CustomDays:    []int{29},
		CustomHours:   []int{9},
		CustomMinutes: []int{0},
		Timezone:      "UTC",
	}
	// 2028 is the next leap year after 2026; 2026 and 2027 are not.
	if slot, ok := mostRecentSlot(leapDay, time.Date(2028, time.February, 29, 12, 0, 0, 0, time.UTC)); !ok ||
		slotKey(slot) != "2028-02-29T09:00+00:00" {
		t.Fatalf("on 29 February 2028 the slot is %q (ok=%v), want 2028-02-29T09:00+00:00",
			slotKey(slot), ok)
	}
	// 🔴 In a NON-leap February there is no such date, so that February
	// contributes NOTHING and 28 February is emphatically not the answer. What
	// stands instead is the previous LEAP year's occurrence, which really did
	// happen and is still the most recent one.
	//
	// ⚠️ THE SECOND HALF OF THAT USED TO READ "there is no slot at all", and
	// that was a property of the retired 70-day lookback rather than of this
	// rule: the window could not reach 2024 from 2027, so it answered "no slot"
	// for an occurrence that genuinely elapsed — which is exactly the
	// non-monotonicity mostRecentCustomSlot replaced. The invariant this test
	// is really for is unchanged and is asserted below: never the 28th.
	slot, ok := mostRecentSlot(leapDay, time.Date(2027, time.February, 28, 23, 59, 0, 0, time.UTC))
	if !ok || slotKey(slot) != "2024-02-29T09:00+00:00" {
		t.Fatalf("in a non-leap February the slot is %q (ok=%v), want the previous leap day "+
			"2024-02-29T09:00+00:00 — an occurrence that happened does not stop having happened", slotKey(slot), ok)
	}
	if ok && slot.Day() == 28 {
		t.Fatalf("a non-leap February produced %q — the 29th must be an occurrence that does not "+
			"happen, never clamped onto the 28th", slotKey(slot))
	}

	// The other half: a February schedule whose days DO exist there fires
	// normally, so the absence above is about the date and not about February.
	febEnds := leapDay
	febEnds.ID = "sch-feb"
	febEnds.CustomDays = []int{1, 28, 29, 31}
	for _, tc := range []struct{ name, now, want string }{
		{"non-leap February stops at the 28th", "2027-03-01T00:00:00Z", "2027-02-28T09:00+00:00"},
		{"leap February reaches the 29th", "2028-03-01T00:00:00Z", "2028-02-29T09:00+00:00"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now, _ := time.Parse(time.RFC3339, tc.now)
			slot, ok := mostRecentSlot(febEnds, now)
			if !ok || slotKey(slot) != tc.want {
				t.Fatalf("at %s the slot is %q (ok=%v), want %q. The 31st is a day February never "+
					"has and it must cost only itself, not the month's other listed days",
					tc.now, slotKey(slot), ok, tc.want)
			}
		})
	}
}

// TestPatchingMonthsReAimsTheCursorOnlyWhenTheyChange is the months half of the
// re-aim rule every other timing field already follows.
//
// Narrowing the months MOVES the schedule, so the cursor must move with it —
// otherwise the edit can retroactively fire a slot it crossed. Re-sending the
// SAME months in a different order moves nothing, which is what stops a cockpit
// that posts the whole form back on every save from re-aiming on every press;
// a re-aim landing between a slot elapsing and the next tick swallows that
// delivery permanently, with no error and no log line.
//
// Red when: months are left out of the re-aim comparison (an edit fires a slot
// it crossed), or compared as SENT rather than in canonical form (every no-op
// save swallows a delivery).
func TestPatchingMonthsReAimsTheCursorOnlyWhenTheyChange(t *testing.T) {
	srv, secret, api := scheduledStack(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")
	status, created := doJSON(t, "POST", srv.URL+"/api/members/mira/scheduled-messages",
		ownerTok, customMonthlyPayload([]int{3, 6, 9}, []int{1}, []int{9}, []int{0}, "Asia/Taipei"))
	if status != 200 {
		t.Fatalf("create: %d %v", status, created)
	}
	id, _ := created["id"].(string)
	path := srv.URL + "/api/members/mira/scheduled-messages/" + id

	const planted = "2020-01-01T09:00+08:00"
	plant := func() {
		t.Helper()
		stored, _ := api.dal.GetScheduledMessage(id)
		stored.LastFiredSlot = planted
		if err := api.dal.PutScheduledMessage(*stored); err != nil {
			t.Fatalf("plant cursor: %v", err)
		}
	}

	// Same choice, different order and with a duplicate: nothing moves.
	plant()
	status, same := doJSON(t, "PATCH", path, ownerTok, `{"custom_months":[9,3,6,9]}`)
	if status != 200 {
		t.Fatalf("re-send the same months: %d %v", status, same)
	}
	if got, _ := same["last_fired_slot"].(string); got != planted {
		t.Fatalf("re-sending the same months in a different order moved the cursor to %q. "+
			"A cockpit that posts the whole form back would swallow a delivery on every save", got)
	}

	// A genuinely different choice re-aims.
	plant()
	status, changed := doJSON(t, "PATCH", path, ownerTok, `{"custom_months":[3,6]}`)
	if status != 200 {
		t.Fatalf("narrow the months: %d %v", status, changed)
	}
	if got, _ := changed["last_fired_slot"].(string); got == planted {
		t.Fatalf("narrowing the months left the cursor at %q — an edit that moves the schedule "+
			"must move the cursor, or it can retroactively fire the slot it crossed", got)
	}
}
