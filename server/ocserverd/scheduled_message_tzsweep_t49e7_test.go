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
// ROUND 2 — the MONTH dimension. custom grew a fourth set (custom_months,
// migrations/00053), and round 1's sweep could not see it: every fixture here
// selected all twelve months, so the month test never declined anything and the
// oracle did not carry one. Round 2 teaches the oracle the month test and adds
// fixtures that select FEWER than twelve — which re-opens the vacuity question
// one level down, because a filtered fixture whose readings all land inside its
// own months is indistinguishable from an unfiltered one. So the month events
// are COUNTED (sweepStats.monthDeclined, crossMonth, monthEndSlots,
// leapDaySlots, dstInMonth/dstOutOfMonth, monthPerDayRun) and each count carries
// a floor. A clean sweep with a zero in that line is a failure, not a pass.
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

// tzSweepLeapYear is pinned SEPARATELY from tzSweepFirstYear because 29 February
// is a reading the sweep has to see and tzSweepFirstYear is not a leap year.
// Widening OC_TZSWEEP_YEARS would eventually reach one, but "eventually" is not
// coverage: the default run is the one that guards every commit, so the leap day
// is a window of its own rather than a lucky consequence of a knob.
const tzSweepLeapYear = 2028

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

// monthFiltered reports whether this fixture names FEWER than all twelve months
// — i.e. whether the month test can decline anything at all for it. Derived from
// the set rather than carried as a flag, so a fixture cannot claim one thing and
// select another.
func (s sweepSchedule) monthFiltered() bool {
	return s.sm.Cadence == ScheduledMessageCadenceCustom &&
		canonicalIntSet(s.sm.CustomMonths) != canonicalIntSet(intRange(1, 12))
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

// sweepMonthSchedules are the round-2 fixtures: every one of them names FEWER
// than twelve months, so for every one of them there are dates the month test
// must decline. Round 1 had none — every fixture selected all twelve — which is
// why the fourth dimension was, at this layer, not swept at all.
//
// Each fixture is here for a reading that is hard to get any other way:
//
//	months-1-2-day-31   — the month END. January has a 31st, February does not,
//	                      so the slot has to STAY on 31 January for the whole of
//	                      February and then stop being reported once March (an
//	                      unselected month) is more than a lookback away. This is
//	                      simultaneously the cross-month case: the most recent
//	                      slot is in the PREVIOUS month for weeks at a time.
//	feb-29-only         — the leap day, and nothing else. It fires in 2028 and
//	                      never in 2026: `months {2} × days {29}` is not a new
//	                      rule, it is the day-by-day rule reading a date three
//	                      Februaries in four do not have.
//	feb-days-1-15-29-30-31 — February again, with three days the month never has
//	                      and two it does, at hours that sit where spring-forward
//	                      gaps land. The per-day rule has to keep the 1st and the
//	                      15th while losing exactly the three absent dates.
//	odd-months-every-20-min — the month test MEETING DST. The readings are dense
//	                      (one every twenty minutes) so a gap always swallows
//	                      several, and the month set is every ODD month, which
//	                      across the shipped zones puts some transitions INSIDE a
//	                      selected month and others outside it. Both sides are
//	                      counted and floored (see sweepStats.dstInMonth /
//	                      dstOutOfMonth) rather than assumed.
func sweepMonthSchedules(tz string) []sweepSchedule {
	return []sweepSchedule{
		{name: "custom/months-1-2-day-31", sm: ScheduledMessage{
			Cadence: ScheduledMessageCadenceCustom, Timezone: tz,
			CustomMonths: []int{1, 2},
			CustomDays:   []int{31}, CustomHours: []int{0, 12},
			CustomMinutes: []int{0},
		}},
		{name: "custom/feb-29-only", sm: ScheduledMessage{
			Cadence: ScheduledMessageCadenceCustom, Timezone: tz,
			CustomMonths: []int{2},
			CustomDays:   []int{29}, CustomHours: []int{9},
			CustomMinutes: []int{0},
		}},
		{name: "custom/feb-days-1-15-29-30-31", sm: ScheduledMessage{
			Cadence: ScheduledMessageCadenceCustom, Timezone: tz,
			CustomMonths: []int{2},
			CustomDays:   []int{1, 15, 29, 30, 31}, CustomHours: []int{0, 2, 3},
			CustomMinutes: []int{0, 15, 30, 45},
		}},
		{name: "custom/odd-months-every-20-min", sm: ScheduledMessage{
			Cadence: ScheduledMessageCadenceCustom, Timezone: tz,
			CustomMonths: []int{1, 3, 5, 7, 9, 11},
			CustomDays:   intRange(1, 31), CustomHours: intRange(0, 23),
			CustomMinutes: []int{0, 20, 40},
		}},
	}
}

// sweepSchedulesFor picks the fixtures to drive over one window.
//
// 🔴 It is a SPLIT and not a cross product, and the reason is cost, stated here
// rather than discovered by the next person watching the suite get slower: the
// february window is by far the most expensive one (a whole month at one-hour
// steps, in every zone), and running four more fixtures over it would roughly
// double the sweep for coverage the two calendar windows below already give a
// month-filtered fixture more cheaply. So:
//
//	dst, post-transition — BOTH families. The month × DST combination has to be
//	  swept in every zone, and these windows are where a zone deletes or repeats
//	  a reading at all.
//	february             — the round-1 family only, unchanged.
//	month-boundary, leap-february — the month-filtered family only. They exist
//	  for the month dimension; an all-year fixture has nothing to say there that
//	  the february window does not already say.
//
// Measured on the development machine at the default knobs: round 1 was 585
// zones / 2,751,415 samples / ~117s, round 2 is 585 zones / 4,070,367 samples /
// ~176s. That is a real half-minute-plus added to every `go test` of this
// package, stated here rather than left for whoever notices the suite got
// slower. A full cross product of both families over every window would have
// been roughly twice round 1 again.
func sweepSchedulesFor(tz string, win sweepWindowSpec) []sweepSchedule {
	switch win.kind {
	case "month-boundary", "leap-february":
		return sweepMonthSchedules(tz)
	case "february":
		return sweepSchedules(tz)
	default:
		return append(sweepSchedules(tz), sweepMonthSchedules(tz)...)
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

	// 🔴 The month counters below are the anti-vacuity instrumentation for the
	// FOURTH dimension, and they are the answer to the only question that
	// matters about a clean sweep: did the month test ever actually decide
	// anything? A month-filtered fixture whose readings all happen to fall
	// inside its own month set is indistinguishable from an all-year one, and a
	// sweep made of those would report "0 findings — CLEAN" while checking
	// nothing at all. So each interesting month event is COUNTED and each count
	// carries a floor in assertSweepLookedAtSomething.
	monthSamples   int // samples driven by a month-filtered fixture
	monthDeclined  int // instants whose OWN month is not in the fixture's set
	crossMonth     int // reported slot sits in a different month from `now`
	monthEndSlots  int // slot on a 31st while `now` is in a month with no 31st
	leapDaySlots   int // slot on 29 February
	dstInMonth     int // DST windows where the fixture's set CONTAINS that month
	dstOutOfMonth  int // DST windows where it does not
	monthPerDayRun int // per-day checks run for a month-filtered fixture
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
	// The month line is printed only when something counted it. The planted-bug
	// control drives the same month-filtered fixtures but goes through
	// sweepWindowWith, which deliberately carries no month instrumentation — its
	// question is the planted bug, not the fourth dimension — so an all-zero
	// line there would read as "the month fixtures were not swept", which is
	// false. Absence of the line means "not measured"; a zero IN the line means
	// "measured and never happened", and that one is a failure (see
	// assertSweepActuallyExercisedTheMonthTest).
	if st.monthSamples > 0 {
		fmt.Fprintf(&b, "month dimension: %d month-filtered samples, %d instants outside their month set, "+
			"%d cross-month slots, %d month-end slots, %d leap-day slots, %d in-month / %d out-of-month DST windows, "+
			"%d month-filtered per-day checks\n",
			st.monthSamples, st.monthDeclined, st.crossMonth, st.monthEndSlots, st.leapDaySlots,
			st.dstInMonth, st.dstOutOfMonth, st.monthPerDayRun)
	}
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
			for _, sched := range sweepSchedulesFor(tz, win) {
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
	// pivot is the transition this window was built around, zero for the
	// calendar windows. It is what lets the month counters say whether a
	// zone's deleted/repeated reading fell INSIDE a fixture's month set.
	pivot time.Time
}

// sweepWindows returns the stretches of time worth walking in loc: every DST
// transition it has in the year range (that is where readings are deleted and
// repeated), plus one February (that is where a listed day-of-month is absent
// from the calendar rather than from the zone).
func sweepWindows(loc *time.Location) []sweepWindowSpec {
	var out []sweepWindowSpec
	half := tzSweepWindow()
	for _, tr := range zoneTransitions(loc, tzSweepFirstYear, tzSweepYears()) {
		out = append(out, sweepWindowSpec{kind: "dst", from: tr.Add(-half), to: tr.Add(half), step: tzSweepStep(), pivot: tr})
		// 🔴 And the stretch from the transition THROUGH the following local
		// midnight, coarsely. A ±90-minute window cannot see the trap that
		// broke monotonicity before: that one needs `now` to reach a reading
		// the PREVIOUS day did not have, which is a midnight away. Coarse is
		// enough — the rule being checked here is an ordering between samples,
		// not a boundary, so what matters is that both sides of the midnight
		// are in the SAME window.
		out = append(out, sweepWindowSpec{
			kind: "post-transition", from: tr, to: midnightAfter(tr, loc).Add(half), step: 10 * time.Minute, pivot: tr,
		})
	}
	// February, hourly. The per-day rule asks a question about DATES, so the
	// window has to span a whole month and the step can be coarse.
	feb := time.Date(tzSweepFirstYear, time.February, 1, 0, 0, 0, 0, time.UTC)
	out = append(out, sweepWindowSpec{
		kind: "february", from: feb, to: feb.AddDate(0, 1, 2), step: time.Hour, perDa: true,
	})
	// 🔴 The month boundary, coarsely, over three calendar months. This is the
	// window the round-1 sweep did not have and could not have used: with every
	// fixture selecting all twelve months, crossing from January into February
	// changed nothing anyone could observe. With a month-filtered fixture it is
	// where the answer STOPS MOVING — `months {1,2} × days {31}` keeps reporting
	// 31 January for the whole of February — and where a month the set excludes
	// (March) has to leave the previous month's slot standing rather than
	// producing one of its own. Four-hour steps: the question is which DATE the
	// slot sits on, not which minute.
	boundary := time.Date(tzSweepFirstYear, time.January, 28, 0, 0, 0, 0, time.UTC)
	out = append(out, sweepWindowSpec{
		kind: "month-boundary", from: boundary, to: boundary.AddDate(0, 1, 6), step: 4 * time.Hour, perDa: true,
	})
	// 29 February, in a leap year pinned separately (see tzSweepLeapYear). Short
	// and hourly: the whole point is the one date, plus enough of the days on
	// either side to see it arrive and then stay reported once March begins.
	leap := time.Date(tzSweepLeapYear, time.February, 26, 0, 0, 0, 0, time.UTC)
	out = append(out, sweepWindowSpec{
		kind: "leap-february", from: leap, to: leap.AddDate(0, 0, 5), step: time.Hour, perDa: true,
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
//	no-merge     — every reported slot must BE one of the declared readings —
//	               month, day, hour and minute all four (round 2 added the
//	               month; before it, a slot in a switched-off month passed this
//	               rule silently because the rule did not know months existed).
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
	monthed := sched.monthFiltered()
	var prevSlot time.Time
	var prevKey string
	hadSlot := false
	retired := map[string]bool{} // keys whose contiguous run has ended
	sawDate := map[string]bool{} // "2026-02-15" → this date produced a slot

	// Which side of the month test this zone's transition falls on. Counted per
	// window rather than per instant: the question is about the TRANSITION, and
	// one window is one transition.
	if monthed && !win.pivot.IsZero() {
		if intSetContains(sched.sm.CustomMonths, int(win.pivot.In(loc).Month())) {
			st.dstInMonth++
		} else {
			st.dstOutOfMonth++
		}
	}

	instants := 0
	for now := win.from; !now.After(win.to); now = now.Add(win.step) {
		instants++
		if monthed {
			st.monthSamples++
			if !intSetContains(sched.sm.CustomMonths, int(now.In(loc).Month())) {
				st.monthDeclined++
			}
		}
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
		if monthed {
			noteMonthReading(st, local, now.In(loc))
		}
		sawDate[local.Format("2006-01-02")] = true
		prevSlot, prevKey, hadSlot = slot, key, true
	}

	st.instants += instants
	st.samples += instants

	if win.perDa && custom {
		if monthed {
			st.monthPerDayRun++
		}
		checkPerDay(st, sched, loc, win, sawDate)
	}
}

// noteMonthReading records what a reported slot says about the month dimension.
// Nothing here is a verdict — these are the counters that stop a clean sweep
// from being a sweep that never asked the question.
func noteMonthReading(st *sweepStats, slotLocal, nowLocal time.Time) {
	if slotLocal.Month() != nowLocal.Month() || slotLocal.Year() != nowLocal.Year() {
		st.crossMonth++
	}
	if slotLocal.Day() == 31 && !monthHasDay(nowLocal.Year(), nowLocal.Month(), 31) {
		st.monthEndSlots++
	}
	if slotLocal.Month() == time.February && slotLocal.Day() == 29 {
		st.leapDaySlots++
	}
}

// monthHasDay is the calendar question, asked zone-free: UTC has no transitions,
// so time.Date can only normalise here for the reason being asked about.
func monthHasDay(year int, month time.Month, day int) bool {
	d := time.Date(year, month, day, 12, 0, 0, 0, time.UTC)
	return d.Year() == year && d.Month() == month && d.Day() == day
}

// readingIsDeclared reports why slot is not one of the schedule's declared
// readings, or "" when it is one. Read in loc, because the declaration is in
// wall-clock terms in that zone and nowhere else.
//
// The month is asked FIRST, in the same order the production loop asks it, and
// for the same reason: it is the cheapest way to reject a date, and a reported
// slot in an unselected month is the bluntest possible month bug — the schedule
// fired in a month the owner switched off.
func readingIsDeclared(slot time.Time, sm ScheduledMessage, loc *time.Location) string {
	local := slot.In(loc)
	switch {
	case !intSetContains(sm.CustomMonths, int(local.Month())):
		return fmt.Sprintf("month %d is not in custom_months", int(local.Month()))
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
		// The month test, on the CANDIDATE date — never on `now` and never on
		// the slot. A month-filtered schedule that is between two selected
		// months has no missed reading behind it, and asking the question of
		// the wrong date is precisely how an oracle starts demanding readings
		// production correctly declines.
		if !intSetContains(sm.CustomMonths, int(day.Month())) {
			continue
		}
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
// The expectation is derived, not listed: a date is owed a slot when its MONTH
// is declared AND its day-of-month is declared AND the zone genuinely has at
// least one of the declared readings on it AND that reading has elapsed inside
// the window.
//
// 🔴 The month condition here is a NARROWING, and that is the direction that can
// hide a bug: a date in an unselected month is simply not owed anything, so the
// rule stops accusing production of skipping it. What keeps that from disarming
// the rule is the other side — a date whose month IS selected is owed a slot
// exactly as before, and custom/feb-days-1-15-29-30-31 is a fixture whose whole
// month set is selected, so for it this condition removes nothing at all.
func checkPerDay(st *sweepStats, sched sweepSchedule, loc *time.Location, win sweepWindowSpec, saw map[string]bool) {
	for day := dayAnchor(win.from.In(loc)); !day.After(dayAnchor(win.to.In(loc))); day = day.AddDate(0, 0, 1) {
		if !intSetContains(sched.sm.CustomMonths, int(day.Month())) {
			continue
		}
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
	assertSweepFixturesCoverBothSidesOfTheMonthTest(t)
	st := sweepAllZones(t)
	t.Log(st.report())
	assertSweepLookedAtSomething(t, st)
	if st.total() != 0 {
		t.Fatalf("timezone sweep is not clean:\n%s", st.report())
	}
}

// assertSweepFixturesCoverBothSidesOfTheMonthTest is what the round-1 gate
// turned into, and the premise really did change rather than the gate merely
// being switched off.
//
// 🔴 ROUND 1 read: "the oracle has NO month condition, so every fixture must
// select all twelve months, or the oracle would expect readings production
// correctly declines." That premise is gone — readingIsDeclared,
// declaredReadingBetween, checkPerDay and mostRecentSlotWith each carry the
// month test now, so a month-filtered fixture is exactly what the oracle is
// built to be driven with. Keeping the old assertion would forbid the fixtures
// this round exists to add.
//
// What survives is the danger the old gate was really pointing at: the month
// dimension being present in the code and absent from the sweep. So the
// assertion is INVERTED — both sides of the month test must be represented in
// the fixture list. All-twelve fixtures keep the round-1 behaviour under
// guard (a month test that started rejecting valid months would break them);
// month-filtered fixtures are the only reason any month event below can happen
// at all. Neither alone is a sweep of this dimension.
//
// ⚠️ This is a check on the FIXTURE LIST, not on what the sweep observed. It
// cannot tell whether a filtered fixture's month test ever actually decided
// anything — that is what the counters in assertSweepLookedAtSomething are for,
// and the two are deliberately separate: this one fails before a two-minute run,
// that one fails on the evidence the run produced.
func assertSweepFixturesCoverBothSidesOfTheMonthTest(t *testing.T) {
	t.Helper()
	allYear, filtered := 0, 0
	for _, sched := range append(sweepSchedules("UTC"), sweepMonthSchedules("UTC")...) {
		if sched.sm.Cadence != ScheduledMessageCadenceCustom {
			continue
		}
		if len(sched.sm.CustomMonths) == 0 {
			t.Fatalf("sweep schedule %q names NO months. An empty set is a 422 at write time "+
				"(ValidateScheduledMessageCustomSets), so a fixture in that shape sweeps a state "+
				"production cannot be in, and every rule below would be vacuous for it.", sched.name)
		}
		if sched.monthFiltered() {
			filtered++
		} else {
			allYear++
		}
	}
	if allYear == 0 {
		t.Fatal("no all-twelve-month custom fixture is left in the sweep — every pre-round-2 row means " +
			"exactly that (see migrations/00053), so dropping the last one stops sweeping the shape " +
			"every existing schedule is in")
	}
	if filtered == 0 {
		t.Fatal("no month-filtered custom fixture in the sweep — custom_months would be carried by the " +
			"production loop and exercised by nothing here, which is the round-1 gap this check exists " +
			"to keep closed")
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
	assertSweepActuallyExercisedTheMonthTest(t, st)
}

// assertSweepActuallyExercisedTheMonthTest is the anti-vacuity floor for the
// FOURTH dimension, and it is the harder half of the two.
//
// 🔴 A month-filtered fixture proves nothing on its own. If every instant it was
// driven at happened to fall inside its own month set, and every slot it
// reported happened to sit in the same month as `now`, then the month test never
// decided anything and the sweep is CLEAN for the same reason an empty sweep is
// clean. Each counter below names one month event that a mutant in the month
// arithmetic would have to disturb, so a zero here means "this sweep could not
// have caught that class of bug" — which is a failure, not a pass.
//
// The floors are order-of-magnitude below the real counts on purpose (the same
// reasoning as the zone/instant floors next door): a floor that hugs today's
// number is a second copy of that number and goes stale the first time tzdata
// ships a zone or a fixture is retuned.
func assertSweepActuallyExercisedTheMonthTest(t *testing.T, st *sweepStats) {
	t.Helper()
	for _, c := range []struct {
		got  int
		min  int
		what string
	}{
		{st.monthSamples, 10000, "samples driven by a month-filtered fixture — the fourth dimension was not swept at all"},
		{st.monthDeclined, 1000, "instants whose own month is OUTSIDE the fixture's set — every reading fell in a selected month, so the month test never declined anything"},
		{st.crossMonth, 1000, "reported slots sitting in a different month from `now` — the month test never had to look back past a month boundary"},
		{st.monthEndSlots, 100, "slots on a 31st reported while `now` is in a month that has no 31st — the month-end case was never reached"},
		{st.leapDaySlots, 100, "slots on 29 February — the leap day was never reported, so the leap-year window looked at nothing"},
		{st.dstInMonth, 50, "DST windows whose transition falls INSIDE a filtered fixture's month set — month × DST never met"},
		{st.dstOutOfMonth, 50, "DST windows whose transition falls OUTSIDE it — the other side of month × DST was never seen"},
		{st.monthPerDayRun, 100, "per-day checks run for a month-filtered fixture — the day-by-day rule never met the month test"},
	} {
		if c.got < c.min {
			t.Fatalf("month dimension is vacuous: only %d %s (want at least %d).\n%s",
				c.got, c.what, c.min, st.report())
		}
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
			for _, sched := range sweepSchedulesFor(tz, win) {
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
		// The month test, copied from mostRecentSlot: the planted bug being
		// demonstrated lives in the per-DATE resolver, so everything ABOVE it
		// has to stay faithful or the control would report month findings that
		// are not the bug it planted.
		if !intSetContains(s.CustomMonths, int(day.Month())) {
			continue
		}
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
