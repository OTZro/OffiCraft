package main

// scheduled_message_tzsweep_t49e7_test.go — T-49e7 timezone sweeper.
//
// The sentinels next door each pin ONE zone and ONE constructed instant. That
// is the right shape for a decision that was argued about, and the wrong shape
// for the question this file asks: does the slot arithmetic hold across EVERY
// zone tzdata ships, at the readings those zones actually delete or repeat?
// Nobody can enumerate those by hand — Antarctica/Casey's three-hour jump and
// America/Nuuk's 23:00 → 00:00 were both found by looking, not by guessing.
//
// So this is a sweeper: all IANA zones × a large number of instants, checked
// against invariants rather than expected values. An invariant needs no oracle
// (an oracle is a second implementation, and a second implementation reproduces
// the first one's bug), and it names the failure in the vocabulary of the rule
// it broke.
//
// 🔴 A sweeper that finds nothing and a sweeper that looks at nothing are the
// same picture. So the sampling scale is asserted (zones, instants, samples all
// have floors), every invariant checker states what it looked at, and
// TestTZSweepIsAliveOnAPlantedBug plants a bug and requires findings > 0.
//
// Sampling size is a flag, not a constant: the default is the fast one that
// runs on every `go test`, and `OC_TZSWEEP_YEARS` / `OC_TZSWEEP_STEP_SECS`
// widen it for a deliberate deep run.

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Sampling knobs
// ---------------------------------------------------------------------------

// tzSweepYears is how many years of DST transitions the sweep walks, starting
// at tzSweepFirstYear. One year is the default because the point of the sweep
// is COVERAGE OF ZONES, not of history: a zone's spring-forward is the same
// shape every year, and the readings a zone deletes outright (Pacific/Apia,
// 30 December 2011) are pinned by name in the sentinel file. Widen it with
// OC_TZSWEEP_YEARS for a deep run.
const tzSweepFirstYear = 2026

func tzSweepYears() int { return envInt("OC_TZSWEEP_YEARS", 1) }

// tzSweepStep is the spacing between consecutive `now` readings inside a
// transition window. One minute is the finest reading the slot vocabulary has,
// so the default already misses nothing WITHIN a window.
func tzSweepStep() time.Duration {
	return time.Duration(envInt("OC_TZSWEEP_STEP_SECS", 60)) * time.Second
}

// tzSweepWindow is the half-width of the window swept around each transition.
func tzSweepWindow() time.Duration {
	return time.Duration(envInt("OC_TZSWEEP_WINDOW_MINS", 90)) * time.Minute
}

func envInt(key string, def int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return def
	}
	return v
}

// ---------------------------------------------------------------------------
// The zone list
// ---------------------------------------------------------------------------

// tzSweepZones returns every IANA zone name this machine can load.
//
// 🔴 Two sources on purpose, and the union of them. The host database is the
// larger list; $GOROOT/lib/time/zoneinfo.zip is the one that is present even
// on a machine without /usr/share/zoneinfo. Reading only the host's would make
// the sweep silently degrade to nothing in a slim container — the exact failure
// mode the `_ "time/tzdata"` import in schedule_slot.go exists to prevent.
// Every candidate is confirmed by time.LoadLocation, so a zone.tab or a
// leapseconds file cannot smuggle itself in as a zone.
func tzSweepZones(t *testing.T) []string {
	t.Helper()
	seen := map[string]bool{}
	for _, root := range []string{"/usr/share/zoneinfo", "/usr/lib/zoneinfo", "/usr/share/lib/zoneinfo"} {
		collectZoneDir(root, seen)
	}
	for _, root := range []string{os.Getenv("GOROOT"), runtime.GOROOT()} {
		if root == "" {
			continue
		}
		collectZoneZip(filepath.Join(root, "lib", "time", "zoneinfo.zip"), seen)
	}

	out := make([]string, 0, len(seen))
	for name := range seen {
		if _, err := time.LoadLocation(name); err != nil {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// collectZoneDir walks one zoneinfo tree. The root is resolved through
// EvalSymlinks first: on macOS /usr/share/zoneinfo IS a symlink, and Walk
// lstats its root, so without this the richest source on the development
// machine yields nothing at all — silently.
func collectZoneDir(root string, seen map[string]bool) {
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		if plausibleZoneName(rel) {
			seen[rel] = true
		}
		return nil
	})
}

func collectZoneZip(path string, seen map[string]bool) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return
	}
	defer zr.Close()
	for _, f := range zr.File {
		if plausibleZoneName(f.Name) {
			seen[f.Name] = true
		}
	}
}

// plausibleZoneName screens out the non-zone files that live in the same tree
// (zone.tab, iso3166.tab, leapseconds, tzdata.zi, +VERSION) and the posix/ and
// right/ mirrors, which are the same zones under another prefix and would
// merely triple the run time.
func plausibleZoneName(name string) bool {
	if name == "" || strings.ContainsAny(name, ".+") {
		return false
	}
	if strings.HasPrefix(name, "posix/") || strings.HasPrefix(name, "right/") {
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// The schedules the sweep drives
// ---------------------------------------------------------------------------

type sweepSchedule struct {
	name string
	sm   ScheduledMessage
}

// sweepSchedules are chosen so that every invariant below has something to bite
// on: a dense one (a reading every twenty minutes, so a DST gap always swallows
// several), a sparse one whose hours sit exactly where spring-forward gaps land
// and whose days include a 31st (February's missing day), and a single-day one
// where the gap between two occurrences is the 59-day worst case customLookbackDays
// is sized for.
func sweepSchedules(tz string) []sweepSchedule {
	return []sweepSchedule{
		{name: "custom/every-20-min", sm: ScheduledMessage{
			Cadence: ScheduledMessageCadenceCustom, Timezone: tz,
			CustomMonths: intRange(1, 12),
			CustomDays:   intRange(1, 31), CustomHours: intRange(0, 23),
			CustomMinutes: []int{0, 20, 40},
		}},
		{name: "custom/days-1-15-31-hours-0-2-3", sm: ScheduledMessage{
			Cadence: ScheduledMessageCadenceCustom, Timezone: tz,
			CustomMonths: intRange(1, 12),
			CustomDays:   []int{1, 15, 31}, CustomHours: []int{0, 2, 3},
			CustomMinutes: []int{0, 15, 30, 45},
		}},
		{name: "custom/day-31-only", sm: ScheduledMessage{
			Cadence: ScheduledMessageCadenceCustom, Timezone: tz,
			CustomMonths: intRange(1, 12),
			CustomDays:   []int{31}, CustomHours: []int{0, 12},
			CustomMinutes: []int{0},
		}},
		// One reading a day, in the middle of the day. 🔴 This one is not
		// decoration: the day-arithmetic trap dayAnchor exists to prevent
		// (walking days on a LOCAL time, so a step back onto a wall reading the
		// zone deleted normalises into the day BEFORE) is invisible to a dense
		// schedule, because a dense schedule always has an elapsed reading today
		// and the walk-back never runs. It shows up only when today's single
		// reading is still ahead and yesterday has to be examined.
		{name: "custom/noon-daily", sm: ScheduledMessage{
			Cadence: ScheduledMessageCadenceCustom, Timezone: tz,
			CustomMonths: intRange(1, 12),
			CustomDays:   intRange(1, 31), CustomHours: []int{12},
			CustomMinutes: []int{0},
		}},
		{name: "daily/00:30", sm: ScheduledMessage{
			Cadence: ScheduledMessageCadenceDaily, Timezone: tz,
			Hour: 0, Minute: 30,
		}},
	}
}

// ---------------------------------------------------------------------------
// Findings
// ---------------------------------------------------------------------------

type sweepFinding struct {
	rule string // "monotonic" | "no-duplicate" | "no-merge" | "per-day"
	text string
}

type sweepStats struct {
	zones      int
	instants   int // distinct `now` readings fed to mostRecentSlot
	samples    int // instants × schedules
	findings   []sweepFinding
	byRule     map[string]int
	maxRecord  int
	suppressed int
}

func newSweepStats() *sweepStats {
	return &sweepStats{byRule: map[string]int{}, maxRecord: 6}
}

// note records a finding. The printing budget is PER RULE, not global: a broken
// rule fires on every instant inside the window it broke in, so a global budget
// is spent by whichever rule sorts first and the report then omits the rules
// that would have told you the most.
func (st *sweepStats) note(rule, format string, args ...any) {
	st.byRule[rule]++
	if st.byRule[rule] <= st.maxRecord {
		st.findings = append(st.findings, sweepFinding{rule: rule, text: fmt.Sprintf(format, args...)})
		return
	}
	st.suppressed++
}

func (st *sweepStats) total() int {
	n := 0
	for _, c := range st.byRule {
		n += c
	}
	return n
}

func (st *sweepStats) report() string {
	var b strings.Builder
	fmt.Fprintf(&b, "scanned %d zones, %d instants, %d samples (schedule × instant)\n",
		st.zones, st.instants, st.samples)
	if st.total() == 0 {
		b.WriteString("findings: 0 — CLEAN\n")
		return b.String()
	}
	fmt.Fprintf(&b, "findings: %d\n", st.total())
	rules := make([]string, 0, len(st.byRule))
	for r := range st.byRule {
		rules = append(rules, r)
	}
	sort.Strings(rules)
	for _, r := range rules {
		fmt.Fprintf(&b, "  %-13s %d\n", r, st.byRule[r])
	}
	for _, r := range rules {
		for _, f := range st.findings {
			if f.rule == r {
				fmt.Fprintf(&b, "  [%s] %s\n", f.rule, f.text)
			}
		}
	}
	if st.suppressed > 0 {
		fmt.Fprintf(&b, "  ... %d further findings not printed\n", st.suppressed)
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// The sweep itself
// ---------------------------------------------------------------------------

// sweepAllZones drives every schedule over every window of every zone and
// returns what it found. It is a plain function, not a test, so both the CLEAN
// run and the planted-bug run go through exactly the same code.
func sweepAllZones(t *testing.T) *sweepStats {
	t.Helper()
	st := newSweepStats()
	zones := tzSweepZones(t)
	st.zones = len(zones)
	for _, tz := range zones {
		loc, err := time.LoadLocation(tz)
		if err != nil {
			continue
		}
		for _, win := range sweepWindows(loc) {
			for _, sched := range sweepSchedules(tz) {
				sweepWindow(st, sched, loc, win)
			}
		}
	}
	return st
}

// sweepWindowSpec is a half-open stretch of instants plus the step between them.
type sweepWindowSpec struct {
	kind  string
	from  time.Time
	to    time.Time
	step  time.Duration
	perDa bool // run the per-day invariant over this window
}

// sweepWindows returns the stretches of time worth walking in loc: every DST
// transition it has in the year range (that is where readings are deleted and
// repeated), plus one February (that is where a listed day-of-month is absent
// from the calendar rather than from the zone).
func sweepWindows(loc *time.Location) []sweepWindowSpec {
	var out []sweepWindowSpec
	half := tzSweepWindow()
	for _, tr := range zoneTransitions(loc, tzSweepFirstYear, tzSweepYears()) {
		out = append(out, sweepWindowSpec{kind: "dst", from: tr.Add(-half), to: tr.Add(half), step: tzSweepStep()})
		// 🔴 And the stretch from the transition THROUGH the following local
		// midnight, coarsely. A ±90-minute window cannot see the trap that
		// broke monotonicity before: that one needs `now` to reach a reading
		// the PREVIOUS day did not have, which is a midnight away. Coarse is
		// enough — the rule being checked here is an ordering between samples,
		// not a boundary, so what matters is that both sides of the midnight
		// are in the SAME window.
		out = append(out, sweepWindowSpec{
			kind: "post-transition", from: tr, to: midnightAfter(tr, loc).Add(half), step: 10 * time.Minute,
		})
	}
	// February, hourly. The per-day rule asks a question about DATES, so the
	// window has to span a whole month and the step can be coarse.
	feb := time.Date(tzSweepFirstYear, time.February, 1, 0, 0, 0, 0, time.UTC)
	out = append(out, sweepWindowSpec{
		kind: "february", from: feb, to: feb.AddDate(0, 1, 2), step: time.Hour, perDa: true,
	})
	return out
}

// midnightAfter is the first instant of the next calendar date in loc, found by
// stepping rather than constructed, because the reading 00:00 is precisely the
// one some zones do not have.
func midnightAfter(at time.Time, loc *time.Location) time.Time {
	local := at.In(loc)
	next := dayAnchor(local).AddDate(0, 0, 1)
	first, ok := firstReadingOn(next.Year(), next.Month(), next.Day(), loc)
	if !ok {
		return at.Add(24 * time.Hour)
	}
	return first
}

// zoneTransitions finds the instants where loc changes offset, by walking the
// range at one-hour steps and watching for the offset to move. Sub-hour
// transition instants are therefore located to the hour they fall in, which is
// all the sweep needs: the window around each is 90 minutes wide.
func zoneTransitions(loc *time.Location, firstYear, years int) []time.Time {
	start := time.Date(firstYear, time.January, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(years, 0, 0)
	var out []time.Time
	_, prev := start.In(loc).Zone()
	for at := start; at.Before(end); at = at.Add(time.Hour) {
		_, off := at.In(loc).Zone()
		if off != prev {
			out = append(out, at)
			prev = off
		}
	}
	return out
}

// sweepWindow walks one window and checks every invariant it can check there.
//
// The four rules, and what each one is protecting:
//
//	monotonic    — `now` moves forward, so the reported slot must never move
//	               back. A backwards slot re-delivers something already sent,
//	               and a duplicate is indistinguishable in the chat log from a
//	               correct delivery.
//	no-duplicate — a slot key that stopped being reported must never come back.
//	               Same delivery, second time, via a different route.
//	no-merge     — every reported slot must BE one of the declared readings.
//	               A reading the zone deleted has to be SKIPPED; moving it onto
//	               another reading is what collapses two occurrences into one
//	               slot key, and moving it anywhere else delivers at a wall
//	               clock nobody chose.
//	               ⚠️ Honest scope: this catches every move that lands OFF the
//	               declared grid, which is what a forward search does whenever
//	               the gap does not end exactly on a declared reading. A move
//	               that lands EXACTLY on another declared reading is, through
//	               mostRecentSlot, indistinguishable from skipping — both
//	               report that reading once — so no invariant here can see it.
//	               Say so rather than implying the rule is total.
//	latest       — no declared reading may sit strictly between the reported
//	               slot and `now`. "Most recent" is the whole contract; a slot
//	               that is merely SOME past reading is an arbitrarily stale one.
//	elapsed      — the slot must not be in the future.
func sweepWindow(st *sweepStats, sched sweepSchedule, loc *time.Location, win sweepWindowSpec) {
	custom := sched.sm.Cadence == ScheduledMessageCadenceCustom
	var prevSlot time.Time
	var prevKey string
	hadSlot := false
	retired := map[string]bool{} // keys whose contiguous run has ended
	sawDate := map[string]bool{} // "2026-02-15" → this date produced a slot

	instants := 0
	for now := win.from; !now.After(win.to); now = now.Add(win.step) {
		instants++
		slot, ok := mostRecentSlot(sched.sm, now)
		if !ok {
			if hadSlot {
				st.note("monotonic", "%s %s: slot %s at %s vanished by %s (a schedule cannot un-fire)",
					loc, sched.name, slotKey(prevSlot), now.Add(-win.step).In(loc), now.In(loc))
				hadSlot = false
			}
			continue
		}
		key := slotKey(slot)

		// elapsed — a slot is "the most recent one that HAS ALREADY elapsed".
		// A slot in the future is a delivery sent before its time, and the
		// cursor then refuses the real occurrence when it arrives.
		if slot.After(now) {
			st.note("elapsed", "%s %s: at %s the reported slot is %s, which has not happened yet",
				loc, sched.name, now.In(loc), key)
		}

		if hadSlot && slot.Before(prevSlot) {
			st.note("monotonic", "%s %s: now advanced to %s but the slot went BACK from %s to %s",
				loc, sched.name, now.In(loc), slotKey(prevSlot), key)
		}
		if key != prevKey {
			if retired[key] {
				st.note("no-duplicate", "%s %s: slot %s was reported again at %s after %s had taken over — that is a second delivery of the same occurrence",
					loc, sched.name, key, now.In(loc), prevKey)
			}
			if prevKey != "" {
				retired[prevKey] = true
			}
		}

		if custom {
			if why := readingIsDeclared(slot, sched.sm, loc); why != "" {
				st.note("no-merge", "%s %s: reported slot %s is not a declared reading (%s) — a deleted reading was MOVED instead of skipped",
					loc, sched.name, key, why)
			}
			if r, found := declaredReadingBetween(slot, now, sched.sm, loc); found {
				st.note("latest", "%s %s: at %s the slot is %s, but the declared reading %s also exists and is later — that is not the most recent slot",
					loc, sched.name, now.In(loc), key, slotKey(r))
			}
		}

		local := slot.In(loc)
		sawDate[local.Format("2006-01-02")] = true
		prevSlot, prevKey, hadSlot = slot, key, true
	}

	st.instants += instants
	st.samples += instants

	if win.perDa && custom {
		checkPerDay(st, sched, loc, win, sawDate)
	}
}

// readingIsDeclared reports why slot is not one of the schedule's declared
// readings, or "" when it is one. Read in loc, because the declaration is in
// wall-clock terms in that zone and nowhere else.
func readingIsDeclared(slot time.Time, sm ScheduledMessage, loc *time.Location) string {
	local := slot.In(loc)
	switch {
	case !intSetContains(sm.CustomDays, local.Day()):
		return fmt.Sprintf("day %d is not in custom_days", local.Day())
	case !intSetContains(sm.CustomHours, local.Hour()):
		return fmt.Sprintf("hour %d is not in custom_hours", local.Hour())
	case !intSetContains(sm.CustomMinutes, local.Minute()):
		return fmt.Sprintf("minute %d is not in custom_minutes", local.Minute())
	}
	return ""
}

// declaredReadingBetween looks for a declared reading the zone genuinely has in
// (slot, now]. Existence is asked with readBack — the same judgement the
// production code uses — so a reading inside a DST gap is correctly not counted
// as a missed one.
func declaredReadingBetween(slot, now time.Time, sm ScheduledMessage, loc *time.Location) (time.Time, bool) {
	if !now.After(slot) {
		return time.Time{}, false
	}
	first := slot.In(loc)
	last := now.In(loc)
	for day := dayAnchor(first); !day.After(dayAnchor(last)); day = day.AddDate(0, 0, 1) {
		if !intSetContains(sm.CustomDays, day.Day()) {
			continue
		}
		for _, h := range sm.CustomHours {
			for _, m := range sm.CustomMinutes {
				at, ok := readBack(time.Date(day.Year(), day.Month(), day.Day(), h, m, 0, 0, time.UTC), loc)
				if !ok {
					continue
				}
				if at.After(slot) && !at.After(now) {
					return at, true
				}
			}
		}
	}
	return time.Time{}, false
}

// checkPerDay is the day-by-day rule: a day-of-month the month does not contain
// costs THAT DAY and nothing else. [1,15,31] in February keeps the 1st and the
// 15th. A cadence that reads the missing 31st as "skip the month" loses two
// deliveries in silence, which is exactly the prose bug this ticket corrected.
//
// The expectation is derived, not listed: a date is owed a slot when its
// day-of-month is declared AND the zone genuinely has at least one of the
// declared readings on it AND that reading has elapsed inside the window.
func checkPerDay(st *sweepStats, sched sweepSchedule, loc *time.Location, win sweepWindowSpec, saw map[string]bool) {
	for day := dayAnchor(win.from.In(loc)); !day.After(dayAnchor(win.to.In(loc))); day = day.AddDate(0, 0, 1) {
		if !intSetContains(sched.sm.CustomDays, day.Day()) {
			continue
		}
		earliest, ok := earliestDeclaredReading(day, sched.sm, loc)
		if !ok || earliest.After(win.to) {
			continue // the zone has no such reading, or it has not elapsed yet
		}
		date := earliest.In(loc).Format("2006-01-02")
		if !saw[date] {
			st.note("per-day", "%s %s: %s is a declared day whose reading %s exists, yet no instant in the sweep ever reported a slot on it — the day was skipped wholesale",
				loc, sched.name, date, slotKey(earliest))
		}
	}
}

func earliestDeclaredReading(day time.Time, sm ScheduledMessage, loc *time.Location) (time.Time, bool) {
	best := time.Time{}
	found := false
	for _, h := range sm.CustomHours {
		for _, m := range sm.CustomMinutes {
			at, ok := readBack(time.Date(day.Year(), day.Month(), day.Day(), h, m, 0, 0, time.UTC), loc)
			if !ok {
				continue
			}
			if !found || at.Before(best) {
				best, found = at, true
			}
		}
	}
	return best, found
}

// ---------------------------------------------------------------------------
// The tests
// ---------------------------------------------------------------------------

// TestTZSweepFindsNothing is the sweep itself: all IANA zones, every DST
// transition in the year range, plus a February, checked against the four
// invariants. It prints the sampling scale whether it passes or fails —
// "no findings" is only worth reading next to "out of how many".
//
// Red when: any of the four rules is broken anywhere. The failure names the
// zone, the schedule, the instant and the rule.
func TestTZSweepFindsNothing(t *testing.T) {
	assertSweepSchedulesSelectEveryMonth(t)
	st := sweepAllZones(t)
	t.Log(st.report())
	assertSweepLookedAtSomething(t, st)
	if st.total() != 0 {
		t.Fatalf("timezone sweep is not clean:\n%s", st.report())
	}
}

// assertSweepSchedulesSelectEveryMonth keeps the independent oracle below
// honest about the fourth set.
//
// The oracle re-derives the expected readings from CustomDays/Hours/Minutes and
// has NO month condition — which is correct only while every custom fixture
// here selects all twelve months. Add a month-filtered fixture and the oracle
// would silently expect readings production correctly declines, so every
// invariant in the sweep would start reporting findings that are not bugs. This
// turns that into a named failure at the top of the sweep instead.
func assertSweepSchedulesSelectEveryMonth(t *testing.T) {
	t.Helper()
	checked := 0
	for _, sched := range sweepSchedules("UTC") {
		if sched.sm.Cadence != ScheduledMessageCadenceCustom {
			continue
		}
		checked++
		if canonicalIntSet(sched.sm.CustomMonths) != canonicalIntSet(intRange(1, 12)) {
			t.Fatalf("sweep schedule %q selects months [%s], but the oracle below has no month "+
				"condition and would expect readings production correctly declines. Teach the "+
				"oracle the month test before adding a month-filtered fixture.",
				sched.name, canonicalIntSet(sched.sm.CustomMonths))
		}
	}
	if checked == 0 {
		t.Fatal("no custom schedules in the sweep — this check, and most of the sweep, would be vacuous")
	}
}

// assertSweepLookedAtSomething is the anti-vacuity floor. A sweeper that loaded
// zero zones reports zero findings, and so does a correct one.
//
// The floors are deliberately far below the real numbers (hundreds of zones,
// hundreds of thousands of samples): a floor that hugs today's count is a
// second copy of that count, and it goes stale the first time tzdata ships a
// new zone.
func assertSweepLookedAtSomething(t *testing.T, st *sweepStats) {
	t.Helper()
	if st.zones < 100 {
		t.Fatalf("only %d zones were loaded — the sweep is looking at almost nothing, so a clean result proves nothing. "+
			"Neither /usr/share/zoneinfo nor $GOROOT/lib/time/zoneinfo.zip yielded a usable list.", st.zones)
	}
	if st.instants < 10000 {
		t.Fatalf("only %d instants were swept — too few for a clean result to mean anything", st.instants)
	}
	if st.samples < 10000 {
		t.Fatalf("only %d samples were taken — too few for a clean result to mean anything", st.samples)
	}
}

// TestTZSweepIsAliveOnAPlantedBug is the positive control, and it is the reason
// the sweep above is worth reading at all: a sweeper that cannot fail is
// indistinguishable from a sweeper that passes.
//
// It plants THE bug this ticket deliberately rejected — resolving a reading the
// zone deleted by searching FORWARD to the next reading, the way the calendar
// cadences do — and requires the sweep to find it. The planted version is a
// local re-implementation of customSlotOn's inner loop driven through the same
// invariant checkers, because production code must not carry a switch that
// turns its own bug back on.
func TestTZSweepIsAliveOnAPlantedBug(t *testing.T) {
	zones := tzSweepZones(t)
	if len(zones) < 100 {
		t.Fatalf("only %d zones loaded — the control cannot demonstrate anything", len(zones))
	}
	st := newSweepStats()
	st.zones = len(zones)
	planted := 0
	for _, tz := range zones {
		loc, err := time.LoadLocation(tz)
		if err != nil {
			continue
		}
		for _, win := range sweepWindows(loc) {
			if win.kind != "dst" {
				continue // the planted bug lives in the DST gap
			}
			for _, sched := range sweepSchedules(tz) {
				if sched.sm.Cadence != ScheduledMessageCadenceCustom {
					continue
				}
				planted++
				sweepWindowWith(st, sched, loc, win, forwardSearchingSlotOn)
			}
		}
	}
	t.Log(st.report())
	if planted == 0 {
		t.Fatal("the control planted the bug nowhere — no DST windows were swept")
	}
	if st.total() == 0 {
		t.Fatalf("the sweep found NOTHING on a version that resolves a deleted reading by searching forward — "+
			"the invariants are not live and every clean result in this file is worthless\n%s", st.report())
	}
}

// forwardSearchingSlotOn is customSlotOn with the one line this ticket argued
// about inverted: instead of skipping a reading the zone deleted, it walks
// forward to the next reading the zone does have, exactly as slotAt does for
// the calendar cadences. Everything else is a copy.
func forwardSearchingSlotOn(day time.Time, s ScheduledMessage, loc *time.Location, notAfter time.Time) (time.Time, bool) {
	year, month, dayNum := day.Year(), day.Month(), day.Day()
	if _, exists := firstReadingOn(year, month, dayNum, loc); !exists {
		return time.Time{}, false
	}
	hours, minutes := sortedIntSet(s.CustomHours), sortedIntSet(s.CustomMinutes)
	for hi := len(hours) - 1; hi >= 0; hi-- {
		for mi := len(minutes) - 1; mi >= 0; mi-- {
			want := time.Date(year, month, dayNum, hours[hi], minutes[mi], 0, 0, time.UTC)
			slot, ok := readBack(want, loc)
			if !ok {
				// THE PLANTED BUG: search forward for the next reading the zone
				// does have, rather than dropping this occurrence.
				for probe := want.Add(time.Minute); probe.Day() == dayNum; probe = probe.Add(time.Minute) {
					if moved, movedOK := readBack(probe, loc); movedOK {
						slot, ok = moved, true
						break
					}
				}
				if !ok {
					continue
				}
			}
			if !slot.After(notAfter) {
				return slot, true
			}
		}
	}
	return time.Time{}, false
}

// customSlotOnFunc is the shape of customSlotOn, so the sweep can be pointed at
// a planted variant without production carrying a seam.
type customSlotOnFunc func(day time.Time, s ScheduledMessage, loc *time.Location, notAfter time.Time) (time.Time, bool)

// mostRecentSlotWith is mostRecentSlot's custom branch with the per-date
// resolver injected. It exists only for the positive control.
func mostRecentSlotWith(s ScheduledMessage, now time.Time, slotOnDate customSlotOnFunc) (time.Time, bool) {
	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		return time.Time{}, false
	}
	local := now.In(loc)
	anchor := dayAnchor(local)
	for back := 0; back <= customLookbackDays; back++ {
		day := anchor.AddDate(0, 0, -back)
		if !intSetContains(s.CustomDays, day.Day()) {
			continue
		}
		if slot, ok := slotOnDate(day, s, loc, local); ok {
			return slot, true
		}
	}
	return time.Time{}, false
}

// sweepWindowWith is sweepWindow driven through an injected resolver. The two
// share their invariant checkers via the helpers above; only the call into the
// slot arithmetic differs.
func sweepWindowWith(st *sweepStats, sched sweepSchedule, loc *time.Location, win sweepWindowSpec, slotOnDate customSlotOnFunc) {
	var prevSlot time.Time
	var prevKey string
	hadSlot := false
	retired := map[string]bool{}

	instants := 0
	for now := win.from; !now.After(win.to); now = now.Add(win.step) {
		instants++
		slot, ok := mostRecentSlotWith(sched.sm, now, slotOnDate)
		if !ok {
			hadSlot = false
			continue
		}
		key := slotKey(slot)
		if slot.After(now) {
			st.note("elapsed", "%s %s: at %s the reported slot is %s, which has not happened yet",
				loc, sched.name, now.In(loc), key)
		}
		if hadSlot && slot.Before(prevSlot) {
			st.note("monotonic", "%s %s: now advanced to %s but the slot went BACK from %s to %s",
				loc, sched.name, now.In(loc), slotKey(prevSlot), key)
		}
		if key != prevKey {
			if retired[key] {
				st.note("no-duplicate", "%s %s: slot %s was reported again at %s after %s had taken over",
					loc, sched.name, key, now.In(loc), prevKey)
			}
			if prevKey != "" {
				retired[prevKey] = true
			}
		}
		if why := readingIsDeclared(slot, sched.sm, loc); why != "" {
			st.note("no-merge", "%s %s: reported slot %s is not a declared reading (%s) — a deleted reading was MOVED instead of skipped",
				loc, sched.name, key, why)
		}
		if r, found := declaredReadingBetween(slot, now, sched.sm, loc); found {
			st.note("latest", "%s %s: at %s the slot is %s, but the declared reading %s also exists and is later",
				loc, sched.name, now.In(loc), key, slotKey(r))
		}
		prevSlot, prevKey, hadSlot = slot, key, true
	}
	st.instants += instants
	st.samples += instants
}
