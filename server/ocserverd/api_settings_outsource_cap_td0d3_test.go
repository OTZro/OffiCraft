package main

// api_settings_outsource_cap_td0d3_test.go — task.outsource_max_parallel is
// validated by ONE predicate on BOTH faces (T-d0d3).
//
// The defect: the PATCH face allowed -1 (the 無限 button) while
// loadAuthSettings applied the generic "non-negative integer" check, so an
// owner who saved -1 got a 200 and no warning, and the NEXT `ocserverd serve`
// exited 1 with `FATAL: load settings: … not a non-negative integer: "-1"`.
//
// The load face ALSO had no upper bound, so 21 was refused on save and
// accepted on boot — the same disagreement pointing the other way.
//
// TestOutsourceCapBothFacesAgree is the one that carries the discrimination:
// it drives BOTH faces with the same value and requires the same verdict, so
// hardcoding either side back to its own bounds turns it red.

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// loadOutsourceCap runs the real boot-time loader against a DB whose
// task.outsource_max_parallel row holds raw. Returns the loaded value.
func loadOutsourceCap(t *testing.T, raw string) (int, error) {
	t.Helper()
	d := newTestDAL(t)
	if err := d.PutSetting(settingOutsourceMaxParallel, raw); err != nil {
		t.Fatalf("PutSetting: %v", err)
	}
	// loadAuthSettings directly, NOT loadForTest: that helper t.Fatal's on a
	// load error, so it could never express "this load must fail".
	got, err := loadAuthSettings(d, defaultConfig(), func(string) {})
	return got.outsourceMaxParallel, err
}

// patchOutsourceCap drives the real PATCH /api/settings face. Returns status.
func patchOutsourceCap(t *testing.T, srv string, token string, raw string) int {
	t.Helper()
	status, _ := doJSON(t, "PATCH", srv+"/api/settings", token,
		fmt.Sprintf(`{"outsource_max_parallel":%s}`, raw))
	return status
}

func newOutsourceCapServer(t *testing.T) (string, string) {
	t.Helper()
	_, srv, _, _ := newSettingsTestServer(t, "cap-pass")
	status, data := doJSON(t, "POST", srv.URL+"/api/login", "", `{"password":"cap-pass"}`)
	if status != 200 {
		t.Fatalf("login: %d", status)
	}
	return srv.URL, data["token"].(string)
}

// TestOutsourceCapBootLoadAcceptsUnlimited is the reproduction turned into a
// regression: -1 is what the UI's 無限 button writes, so it must boot.
func TestOutsourceCapBootLoadAcceptsUnlimited(t *testing.T) {
	got, err := loadOutsourceCap(t, "-1")
	if err != nil {
		t.Fatalf("-1 must load (it is what the write face and the UI call legal): %v", err)
	}
	if got != -1 {
		t.Fatalf("the loaded cap must be the stored value, got %d", got)
	}
	// Positive control on the other end of the legal range: without it a loader
	// that accepted everything would satisfy the line above just as well.
	if got, err := loadOutsourceCap(t, "20"); err != nil || got != 20 {
		t.Fatalf("20 must load as 20: %d %v", got, err)
	}
}

// TestOutsourceCapBootLoadStillFailsClosed — widening for -1 must not widen
// for anything else. 21 in particular used to load fine (no upper bound).
func TestOutsourceCapBootLoadStillFailsClosed(t *testing.T) {
	for _, raw := range []string{"-2", "21", "abc", "", "1.5"} {
		if _, err := loadOutsourceCap(t, raw); err == nil {
			t.Fatalf("%q must fail the boot load, not be silently accepted", raw)
		}
	}
}

// TestOutsourceCapPatchFaceBounds — the write face keeps its verdicts.
func TestOutsourceCapPatchFaceBounds(t *testing.T) {
	base, token := newOutsourceCapServer(t)
	if got := patchOutsourceCap(t, base, token, "-1"); got != http.StatusOK {
		t.Fatalf("PATCH -1 must be accepted (無限), got %d", got)
	}
	if got := patchOutsourceCap(t, base, token, "20"); got != http.StatusOK {
		t.Fatalf("PATCH 20 must be accepted, got %d", got)
	}
	for _, raw := range []string{"-2", "21", `"abc"`} {
		if got := patchOutsourceCap(t, base, token, raw); got != http.StatusUnprocessableEntity {
			t.Fatalf("PATCH %s must be refused with 422, got %d", raw, got)
		}
	}
}

// TestOutsourceCapBothFacesAgree drives the SAME value through the write face
// and the boot face and requires the SAME verdict. This is what a shared
// source of truth buys; it is also the assertion that goes red the moment
// either side grows its own hardcoded bounds again.
func TestOutsourceCapBothFacesAgree(t *testing.T) {
	base, token := newOutsourceCapServer(t)
	for _, n := range []int{-3, -2, -1, 0, 1, 3, 20, 21, 99} {
		raw := strconv.Itoa(n)
		patchOK := patchOutsourceCap(t, base, token, raw) == http.StatusOK
		_, err := loadOutsourceCap(t, raw)
		loadOK := err == nil
		if patchOK != loadOK {
			t.Fatalf("%s: PATCH accepted=%v but boot load accepted=%v — a value "+
				"that saves must boot, and a value that is refused on save must "+
				"never be honoured on boot", raw, patchOK, loadOK)
		}
	}
}

// TestOutsourceCapRefusalTeachesNoBypass — both refusals state the range and
// nothing else. A message that names a flag/escape hatch teaches the reader to
// route around a fail-closed bound.
func TestOutsourceCapRefusalTeachesNoBypass(t *testing.T) {
	_, err := loadOutsourceCap(t, "-2")
	if err == nil {
		t.Fatal("-2 must be refused at boot")
	}
	msg := err.Error()
	if !strings.Contains(msg, outsourceParallelRangeMsg) {
		t.Fatalf("the boot refusal must carry the one shared wording, got %q", msg)
	}
	for _, bypass := range []string{"force", "allow", "override", "skip", "disable"} {
		if strings.Contains(msg, bypass) {
			t.Fatalf("the refusal must not hint at a bypass (%q): %q", bypass, msg)
		}
	}
}
