package main

import (
	"bytes"
	"fmt"
	"log"
	"strings"
	"testing"
)

// aMessageKey returns one whitelisted message code for the happy-path cases.
func aMessageKey(t *testing.T) string {
	t.Helper()
	for k := range messageKeys {
		return k
	}
	t.Fatal("messageKeys is empty — gen:msgkeys did not run")
	return ""
}

func TestValidateWording(t *testing.T) {
	code := aMessageKey(t)

	// nil overlay is admissible (wording is optional).
	if err := validateWording(nil, "t"); err != nil {
		t.Fatalf("nil wording must be admissible: %v", err)
	}

	ok := map[string]map[string]string{
		"zh": {code: "文字"},
		"en": {code: "text"},
	}
	if err := validateWording(&ok, "t"); err != nil {
		t.Fatalf("legal wording overlay must pass: %v", err)
	}

	// An unrecognised code no longer fails the bundle: it is dropped from the
	// overlay, and the known codes beside it survive (owner ruling 2026-07-27).
	// "profile.themeOffice" is the real-world case — T-081b removed the
	// theme-identity keys from the whitelist, so shipped packs carry it.
	lenient := map[string]map[string]string{
		"zh": {code: "文字", "profile.themeOffice": "精靈村", "typo.not.a.key": "x"},
	}
	// The drop is silent to the theme author by owner ruling, but never to the
	// OPERATOR: this repo's decoder principle (api_helpers.go) exists because a
	// silent drop once wiped a document, so every drop leaves a log trail
	// naming the bundle and the codes it removed.
	logs := captureLog(t)
	if err := validateWording(&lenient, "custom_themes[2]"); err != nil {
		t.Fatalf("an unknown wording code must not fail the bundle: %v", err)
	}
	logged := logs.String()
	for _, want := range []string{"custom_themes[2]", "profile.themeOffice", "typo.not.a.key", "2"} {
		if !strings.Contains(logged, want) {
			t.Fatalf("dropping a wording code must be logged with %q: %q", want, logged)
		}
	}
	if _, ok := lenient["zh"]["profile.themeOffice"]; ok {
		t.Fatal("an unknown wording code must be dropped from the overlay")
	}
	if _, ok := lenient["zh"]["typo.not.a.key"]; ok {
		t.Fatal("an unknown wording code must be dropped from the overlay")
	}
	if got := lenient["zh"][code]; got != "文字" {
		t.Fatalf("the known codes beside an unknown one must survive: %q", got)
	}
	logs.Reset()
	clean := map[string]map[string]string{"zh": {code: "文字"}}
	if err := validateWording(&clean, "t"); err != nil {
		t.Fatal(err)
	}
	if noise := logs.String(); noise != "" {
		t.Fatalf("an overlay with nothing dropped must log nothing: %q", noise)
	}
	// A junk-heavy pack must not write a line per code — the count carries the
	// rest of them.
	junk := map[string]string{}
	for i := 0; i < maxLoggedDroppedCodes*3; i++ {
		junk[fmt.Sprintf("junk.key.%02d", i)] = "x"
	}
	logs.Reset()
	junkOverlay := map[string]map[string]string{"zh": junk}
	if err := validateWording(&junkOverlay, "t"); err != nil {
		t.Fatal(err)
	}
	noisy := logs.String()
	if n := strings.Count(noisy, "junk.key."); n != maxLoggedDroppedCodes {
		t.Fatalf("a junk-heavy pack must name at most %d codes: named %d in %q",
			maxLoggedDroppedCodes, n, noisy)
	}
	if !strings.Contains(noisy, fmt.Sprintf("dropped %d ", maxLoggedDroppedCodes*3)) {
		t.Fatalf("the log must carry the full dropped count: %q", noisy)
	}

	// Everything else stays strict.
	badLang := map[string]map[string]string{"xian": {code: "仙"}}
	if err := validateWording(&badLang, "t"); err == nil ||
		!strings.Contains(err.Error(), "is not allowed") {
		t.Fatalf("a language outside {zh,en} must 422: %v", err)
	}

	// The per-language cap counts the RAW submitted entries, so a pack cannot
	// smuggle thousands of junk keys in behind the new leniency.
	over := map[string]string{}
	for i := 0; i <= maxWordingEntriesPerLang; i++ {
		over[fmt.Sprintf("junk.key.%d", i)] = "x"
	}
	overCap := map[string]map[string]string{"zh": over}
	if err := validateWording(&overCap, "t"); err == nil ||
		!strings.Contains(err.Error(), "holds more than") {
		t.Fatalf("an over-cap overlay of unknown keys must still 422: %v", err)
	}

	for _, bad := range []struct {
		value string
		want  string
	}{
		{"a\nb", "control characters"},
		{"\x07", "control characters"},
		{"   ", "1..200 characters"},
		{strings.Repeat("字", maxWordingValueLen+1), "1..200 characters"},
	} {
		m := map[string]map[string]string{"zh": {code: bad.value}}
		if err := validateWording(&m, "t"); err == nil ||
			!strings.Contains(err.Error(), bad.want) {
			t.Fatalf("illegal wording value %q must 422 with %q: %v", bad.value, bad.want, err)
		}
	}
}

// TestValidateThemeBundlesWording checks the wording overlay flows through the
// top-level bundle validator (parity with the colours / fonts overlays).
func TestValidateThemeBundlesWording(t *testing.T) {
	code := aMessageKey(t)
	wording := map[string]map[string]string{
		"zh": {code: "文字", "profile.themeOffice": "精靈村"},
	}
	bundles := []ThemeBundleDTO{{
		Id:      "smurf-village",
		Name:    "精靈村",
		Colors:  map[string]string{"--color-bg": "#eef0dc"},
		Wording: &wording,
	}}
	if err := validateThemeBundles(bundles); err != nil {
		t.Fatalf("a bundle carrying an unknown wording code must be accepted: %v", err)
	}
	if _, ok := (*bundles[0].Wording)["zh"]["profile.themeOffice"]; ok {
		t.Fatal("the unknown code must not reach the stored bundle")
	}
	if got := (*bundles[0].Wording)["zh"][code]; got != "文字" {
		t.Fatalf("the known code must still take effect: %q", got)
	}

	badLang := map[string]map[string]string{"xian": {code: "仙"}}
	illegal := []ThemeBundleDTO{{
		Id:      "evil",
		Name:    "Evil",
		Colors:  map[string]string{"--color-bg": "#101018"},
		Wording: &badLang,
	}}
	if err := validateThemeBundles(illegal); err == nil ||
		!strings.Contains(err.Error(), "is not allowed") {
		t.Fatalf("a bundle with a bad wording language must 422: %v", err)
	}
}

// TestDropUnknownWordingCodesLogIsForgeProof pins the log line against the input
// it is made of: a dropped code is exactly the text the whitelist refused, so an
// imported theme could otherwise put newlines in a key and forge whole log lines
// (or flood one with a megabyte-long key).
func TestDropUnknownWordingCodesLogIsForgeProof(t *testing.T) {
	var buf bytes.Buffer
	prevOut, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() { log.SetOutput(prevOut); log.SetFlags(prevFlags) }()

	forged := "x\n2026/01/01 00:00:00 [theme] FORGED: everything is fine"
	long := "y" + strings.Repeat("z", 500)
	wording := map[string]map[string]string{
		"zh": {forged: "a", long: "b"},
	}
	dropUnknownWordingCodes(wording, "custom_themes[0]")

	line := buf.String()
	if strings.Count(strings.TrimRight(line, "\n"), "\n") != 0 {
		t.Fatalf("the drop must be ONE log line, got:\n%s", line)
	}
	if strings.Contains(line, "FORGED: everything is fine\n") {
		t.Fatalf("a forged line escaped into the log:\n%s", line)
	}
	if !strings.Contains(line, `\n`) {
		t.Fatalf("the newline must survive ESCAPED (so the code is still readable):\n%s", line)
	}
	if strings.Contains(line, strings.Repeat("z", maxLoggedDroppedCodeLen+5)) {
		t.Fatalf("an overlong code must be truncated:\n%s", line)
	}
	// …and the drop itself still happened.
	if len(wording["zh"]) != 0 {
		t.Fatalf("unknown codes must still be dropped: %v", wording["zh"])
	}
}
