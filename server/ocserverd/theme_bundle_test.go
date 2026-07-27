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
		// first (T-081b review round 4 recheck, SHOULD-3). A Zs-padded built-in
		// name is still refused, but now by the rule that can name the actual
		// reason: a user who typed a full-width space is told 「辦公室」 is
		// reserved rather than that their name carries "non-ASCII space
		// characters", which they cannot act on.
		for _, name := range []string{
			"\u00A0Office\u00A0", // NO-BREAK SPACE (Zs) — renders as 「Office」
			"\u3000辦公室\u3000",    // IDEOGRAPHIC SPACE (Zs) — renders as 「辦公室」
			"\u1680Office",       // OGHAM SPACE MARK (Zs) — blank in most fonts
		} {
			err := validateThemeBundles(themeBundleNamed(name))
			if err == nil || !strings.Contains(err.Error(), "reserved for a built-in theme") {
				t.Fatalf("name %q must be rejected as reserved, got %v", name, err)
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

	t.Run("rejects a name that claims the built-in theme's display name", func(t *testing.T) {
		// Language-independent: both spellings of the ONE built-in are blocked,
		// and trimming + case-folding closes the trivial dodges. The id is
		// guarded separately (reservedThemeIDs) — this is the guard on what the
		// owner SEES in the picker.
		// The fold is ASCII-ONLY on BOTH sides on purpose (T-081b review round 3,
		// SHOULD-6): strings.ToLower's simple case mapping sends U+0130 (İ) to
		// 'i' while JS's full mapping sends it to "i\u0307", so "OFF\u0130CE" was
		// rejected here and accepted by the client. Neither side folds it now, so
		// 「OFFİCE」 is an ordinary claimable name (see the accept table below).
		for _, name := range []string{"辦公室", "Office", "office", "  OFFICE  ", " 辦公室 "} {
			err := validateThemeBundles(themeBundleNamed(name))
			if err == nil || !strings.Contains(err.Error(), "reserved for a built-in theme") {
				t.Fatalf("name %q must be rejected, got %v", name, err)
			}
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

// TestNormalizeThemeName pins the comparison form character by character — the
// twin table lives in frontend/src/lib/themeBundle.test.ts. Nothing observable
// through validateThemeBundles can tell an ASCII fold from strings.ToLower here
// (a fold that collapses MORE characters can only reject more), so the two
// sides' agreement is pinned on the normaliser itself: this is what stopped
// 「OFF\u0130CE」 being rejected here and accepted by the client, and 「\uFEFF辦公室」
// the other way round.
func TestNormalizeThemeName(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"Office", "office"},
		{"  OFFICE  ", "office"},
		{"\tOFFICE\r\n", "office"},
		{"辦公室", "辦公室"},
		// strings.ToLower would fold these; an ASCII fold must not.
		{"OFF\u0130CE", "off\u0130ce"},
		{"ＯＦＦＩＣＥ", "ＯＦＦＩＣＥ"},
		{"\u212ANIGHT", "\u212Anight"},
		// Every Zs is folded onto U+0020 BEFORE the ASCII trim, so a
		// full-width-padded name normalises exactly like an ASCII-padded one
		// (round 4 recheck, SHOULD-3) — which is what makes 「　辦公室　」 collide
		// with the built-in and be told so.
		{"\u3000辦公室", "辦公室"},
		{"辦公室\u3000", "辦公室"},
		{"\u00A0Office", "office"},
		{"深海\u3000之夜", "深海 之夜"},
		{"\u1680Deep\u2000Ocean\u3000", "deep ocean"},
	} {
		if got := normalizeThemeName(c.in); got != c.want {
			t.Fatalf("normalizeThemeName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestIsBuiltinThemeName pins the DERIVATION, not the two literals: the banned
// set is themeIdentityNames (generated from the i18n locales) intersected with
// reservedThemeIDs. A future built-in must be covered automatically, and an id
// outside the reserved set must stay claimable.
func TestIsBuiltinThemeName(t *testing.T) {
	for id := range reservedThemeIDs {
		names := themeIdentityNames[id]
		if len(names) == 0 {
			t.Fatalf("built-in theme %q has no display name in themeIdentityNames — "+
				"the name guard would silently pass for it", id)
		}
		for _, name := range names {
			if !isBuiltinThemeName(name) {
				t.Fatalf("built-in display name %q must be blocked", name)
			}
		}
	}
	for id, names := range themeIdentityNames {
		if reservedThemeIDs[id] {
			continue
		}
		for _, name := range names {
			if isBuiltinThemeName(name) {
				t.Fatalf("%q is themeIdentity.%s, not a built-in theme's name — "+
					"it must stay claimable by a custom theme", name, id)
			}
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
