package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// themeBundleNamed returns a minimal, otherwise-valid bundle carrying `name`, so
// each case below isolates the NAME rule under test.
func themeBundleNamed(name string) []ThemeBundleDTO {
	return []ThemeBundleDTO{{
		Id:     "midnight",
		Name:   name,
		Colors: map[string]string{"--color-bg": "#101018"},
	}}
}

func TestValidateThemeBundles(t *testing.T) {
	t.Run("rejects a name carrying control, formatting, private-use, surrogate or line/paragraph separator characters", func(t *testing.T) {
		// Written as escapes on purpose: these characters are INVISIBLE, and a
		// reviewer must be able to see which one each case is testing.
		for _, name := range []string{
			"Mid\u0000night", // NUL
			"Mid\u000Anight", // newline
			"Mid\u007Fnight", // DEL
			"Mid\u009Fnight", // C1 control
			"\u202EMidnight", // RIGHT-TO-LEFT OVERRIDE
			"Mid\u202Dnight", // LEFT-TO-RIGHT OVERRIDE
			"Mid\u202Anight", // LEFT-TO-RIGHT EMBEDDING
			"Mid\u2066night", // LEFT-TO-RIGHT ISOLATE
			"Mid\u2069night", // POP DIRECTIONAL ISOLATE
			"Mid\u200Enight", // LEFT-TO-RIGHT MARK
			"Mid\u200Fnight", // RIGHT-TO-LEFT MARK
			// ZERO-WIDTH class (T-081b review round 3, BLOCKER-1). U+FEFF is the
			// load-bearing one: it is the ONE codepoint JS's trim() strips and
			// strings.TrimSpace does not, so while it was left to the trim THIS
			// side — the authority — accepted 「\uFEFF辦公室」 and only the client
			// rejected it. The twin table lives in frontend/src/lib/themeBundle.test.ts.
			"\uFEFF辦公室",    // BOM prefix — renders as 「辦公室」
			"辦公室\uFEFF",    // BOM suffix
			"\uFEFFOffice", // BOM prefix, en spelling
			"Office\uFEFF", // BOM suffix, en spelling
			"辦\u200B公室",    // ZERO WIDTH SPACE
			"Off\u200Bice",
			"Off\u200Cice", // ZERO WIDTH NON-JOINER
			"Off\u200Dice", // ZERO WIDTH JOINER
			"Office\u2060", // WORD JOINER
			"Off\u061Cice", // ARABIC LETTER MARK (a bidi char the first list missed)
			// ── round 4, SHOULD-C: the members of the SAME categories the round-3
			// codepoint list never thought of. Listing codepoints is what missed
			// them; the rule is now the CATEGORY (Cc/Cf/Co/Cs/Zl/Zp).
			"Off\u00ADice",     // SOFT HYPHEN (Cf) — renders as 「Office」
			"Off\u180Eice",     // MONGOLIAN VOWEL SEPARATOR (Cf)
			"Office\U000E0041", // TAG LATIN CAPITAL A (Cf) — the classic invisible payload
			"Office\uE000",     // PRIVATE USE (Co) — renders as whatever the font decides
			"Mid\u2028night",   // LINE SEPARATOR (Zl)
			"Mid\u2029night",   // PARAGRAPH SEPARATOR (Zp)
		} {
			err := validateThemeBundles(themeBundleNamed(name))
			if err == nil || !strings.Contains(err.Error(), "control, formatting, private-use, surrogate or line/paragraph separator") {
				t.Fatalf("name %q must be rejected, got %v", name, err)
			}
		}
		// Zs is NOT in that set — every space separator is NORMALISED to U+0020
		// first (T-081b review round 4 recheck, SHOULD-3), so these are ordinary
		// names that simply lose their padding. Round 8 removed the reserved-name
		// rule that used to catch them on the way out, so they are ACCEPTED, and
		// the trim is what the assertion is really about: the stored name must be
		// the normalised one, not the padded bytes.
		for _, name := range []string{
			"\u00A0Office\u00A0", // NO-BREAK SPACE (Zs) — renders as 「Office」
			"\u3000辦公室\u3000",    // IDEOGRAPHIC SPACE (Zs) — renders as 「辦公室」
			"\u1680Office",       // OGHAM SPACE MARK (Zs) — blank in most fonts
		} {
			if err := validateThemeBundles(themeBundleNamed(name)); err != nil {
				t.Fatalf("name %q must be accepted, got %v", name, err)
			}
			if got := trimThemeName(name); strings.ContainsAny(got, "\u00A0\u3000\u1680") {
				t.Fatalf("name %q kept a non-ASCII space after trimming: %q", name, got)
			}
		}
		// …and a name that is nothing BUT spaces has no name left after the
		// normalise + trim, in every Zs spelling.
		for _, name := range []string{"\u3000", "\u00A0", " \u3000 ", "\u1680\u2000"} {
			err := validateThemeBundles(themeBundleNamed(name))
			if err == nil || !strings.Contains(err.Error(), "name must be 1..") {
				t.Fatalf("name %q must be rejected as empty, got %v", name, err)
			}
		}
	})

	t.Run("accepts a name that matches the built-in theme's display name", func(t *testing.T) {
		// Until round 8 these were rejected: a pack calling itself 辦公室 put a
		// second 辦公室 row in the picker. The owner dropped the rule — 「這是大家
		// 自己用的,自己要怎麼搞我們不用特別管」 — so a duplicate display name is now
		// the user's own business. What still holds is the BUILT-IN's name: it
		// comes from the non-overridable themeIdentity subtree, so the shipped
		// row keeps saying 辦公室 no matter what a pack calls itself.
		// The id stays reserved (reservedThemeIDs); only the NAME is free.
		for _, name := range []string{"辦公室", "Office", "office", "  OFFICE  ", " 辦公室 "} {
			if err := validateThemeBundles(themeBundleNamed(name)); err != nil {
				t.Fatalf("name %q must be accepted, got %v", name, err)
			}
		}
		if err := validateThemeBundles([]ThemeBundleDTO{{
			Id: "office", Name: "Whatever", Colors: map[string]string{"--color-bg": "#101018"},
		}}); err == nil || !strings.Contains(err.Error(), "reserved for a built-in theme") {
			t.Fatalf("the built-in ID must stay reserved, got %v", err)
		}
	})

	t.Run("accepts every legitimate name shape, including the new-theme default", func(t *testing.T) {
		// The rule must not become a general-purpose name filter: CJK, emoji,
		// spaces and punctuation are all ordinary theme names. 新主題 / New theme
		// live in the SAME themeIdentity subtree as the built-in's name but are
		// the default name a NEW custom theme is created with — banning them
		// would reject the app's own create-theme flow.
		for _, name := range []string{
			"精靈村",
			"深海の夜",
			"밤하늘",
			"🌙 Midnight 🌙",
			"Mid night — v2 (beta)!",
			"新主題",
			"New theme",
			"Officescape",
			"辦公室的夜",
			"OFF\u0130CE", // LATIN CAPITAL LETTER I WITH DOT ABOVE — folded by neither side
			// Not a general-purpose filter: the categories rejected above are the
			// invisible ones only. Scripts, emoji (variation selectors included —
			// U+FE0F is Mn and stays legal on purpose), ordinary spaces and
			// punctuation all pass (T-081b review round 4, SHOULD-C).
			"Heart \u2764\uFE0F", // VARIATION SELECTOR-16 — how an emoji name is spelled
			"سمة داكنة",          // Arabic, ordinary letters + ASCII space
			"ערכת נושא כהה",      // Hebrew, ordinary letters + ASCII space
			"Tiếng Việt",         // combining marks (Mn) in an ordinary Latin name
			// Zs is NORMALISED, not rejected (round 4 recheck, SHOULD-3): a
			// full-width space is what a Chinese IME emits for the space bar and
			// a NO-BREAK SPACE is what a paste out of a document carries. Both
			// are ordinary names, and rejecting them told the user nothing they
			// could act on.
			"深海\u3000之夜",             // IDEOGRAPHIC SPACE inside a legitimate name
			"深\u3000海\u3000之\u3000夜", // …several of them
			"Deep\u00A0Ocean",        // NO-BREAK SPACE inside an ordinary name
			"\u3000深海之夜\u3000",       // padded — but not with a built-in's name
		} {
			if err := validateThemeBundles(themeBundleNamed(name)); err != nil {
				t.Fatalf("name %q must be accepted, got %v", name, err)
			}
		}
	})
}

// TestTrimThemeName pins the surviving normaliser character by character — the
// twin table lives in frontend/src/lib/themeBundle.test.ts. It decides the
// LENGTH verdict on both ends, and nothing observable through
// validateThemeBundles can tell strings.TrimSpace from the explicit ASCII set
// (they differ only on U+0085 / U+FEFF, which hasInvisibleNameRune rejects
// first), so the two sides' agreement has to be pinned on the normaliser itself.
func TestTrimThemeName(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"Office", "Office"},
		{"  OFFICE  ", "OFFICE"},
		{"\tOFFICE\r\n", "OFFICE"},
		{"辦公室", "辦公室"},
		// Case is NOT folded — round 8 removed the name comparison that needed a
		// fold, and the two sides' case mappings disagree (U+0130, U+212A).
		{"OFF\u0130CE", "OFF\u0130CE"},
		{"ＯＦＦＩＣＥ", "ＯＦＦＩＣＥ"},
		{"\u212ANIGHT", "\u212ANIGHT"},
		// Every Zs is folded onto U+0020 BEFORE the ASCII trim, so a
		// full-width-padded name trims exactly like an ASCII-padded one
		// (round 4 recheck, SHOULD-3).
		{"\u3000辦公室", "辦公室"},
		{"辦公室\u3000", "辦公室"},
		{"\u00A0Office", "Office"},
		{"深海\u3000之夜", "深海 之夜"},
		{"\u1680Deep\u2000Ocean\u3000", "Deep Ocean"},
	} {
		if got := trimThemeName(c.in); got != c.want {
			t.Fatalf("trimThemeName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestThemeNameVerdictsEmit is the Go HALF of the cross-language name-parity
// safety net (T-081b review round 4, SHOULD-C). It is not an assertion: driven
// by the two env vars below it reads a shared case file and writes THIS side's
// verdict for every name, so frontend/src/lib/themeName.parity.test.ts can feed
// the SAME 61 names to both validators and fail on any divergence.
//
// A cross-check is needed because the two ends now read Unicode CATEGORIES out
// of two different runtimes' tables (Go's `unicode` package vs the JS engine's
// property escapes). That is the point — neither side hand-keeps a codepoint
// list — but it also means the two could drift apart on a Unicode version bump,
// silently, in exactly the direction that matters: a character one side calls
// Cf and the other does not. Without the env vars the test no-ops, so an
// ordinary `go test ./...` is unaffected.
func TestThemeNameVerdictsEmit(t *testing.T) {
	in, out := os.Getenv("OC_THEME_NAME_CASES"), os.Getenv("OC_THEME_NAME_VERDICTS")
	if in == "" || out == "" {
		t.Skip("OC_THEME_NAME_CASES / OC_THEME_NAME_VERDICTS unset — driven by themeName.parity.test.ts")
	}
	raw, err := os.ReadFile(in)
	if err != nil {
		t.Fatalf("read cases: %v", err)
	}
	var cases []struct {
		K string `json:"k"`
		N string `json:"n"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("parse cases: %v", err)
	}
	verdicts := make(map[string]string, len(cases))
	for _, c := range cases {
		if err := validateThemeBundles(themeBundleNamed(c.N)); err != nil {
			verdicts[c.K] = "REJECT: " + strings.TrimPrefix(err.Error(), "custom_themes[0]: ")
		} else {
			verdicts[c.K] = "ACCEPT"
		}
	}
	blob, err := json.Marshal(verdicts)
	if err != nil {
		t.Fatalf("marshal verdicts: %v", err)
	}
	if err := os.WriteFile(out, blob, 0o600); err != nil {
		t.Fatalf("write verdicts: %v", err)
	}
}
