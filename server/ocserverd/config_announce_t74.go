package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ── WHY THIS FILE EXISTS ─────────────────────────────────────────────────────
//
// The owner's complaint was one word long and it was "silently": starting
// ocserverd without a config file does not fail, it quietly resolves to the
// built-in default path and acts on the REAL database. Two people read the code
// and agreed: nothing is wrong with the resolution — $OC_DATABASE_URL, then
// oc.toml [storage].dsn, then ~/.officraft{-<ns>}/server/data/officraft.db is
// exactly what it should do, and the absolute default is a deliberate fix (a
// CWD-relative one silently grew a second database when you launched from
// elsewhere).
//
// The first plan was to REFUSE to start without a config file. That plan was
// withdrawn after measuring what it would break: a normal install
// (`curl | bash`) deliberately leaves no config file at all — install.sh:1174
// literally names that outcome CFG_SRC="none" — and two commands the user guide
// tells people to type by hand (`ocserverd mfa-disable` when their authenticator
// is gone, `ocserverd backup`) are exactly the ones that would have been
// blocked. The rescue path would have been the casualty. The owner chose the
// other option (rc-d961ee5e790c [0]): do not block, SAY IT OUT LOUD.
//
// So this prints, before anything is opened and before anything is written,
// which config file was consulted and which database file is about to be acted
// on — AND where each of those two answers came from. The source is the part
// that matters: "the database is /Users/me/.officraft/server/data/officraft.db"
// is a fact someone can misread as "that is what I asked for"; "…(built-in
// default — no config file, $OC_DATABASE_URL unset)" cannot be misread.
//
// ⚠️ It is a NO-OP on behaviour by construction: it opens nothing, writes
// nothing, and never changes an exit code. That is the point — it was chosen
// over the refusal precisely because it cannot break a machine we cannot see.

// configSource and dsnSource name where each answer came from, in the words of
// the thing the reader can go and change.
func configSource(env func(string) string) (path string, exists bool, from string) {
	path = configPath(env)
	if p := env(envConfigPath); p != "" {
		from = "$" + envConfigPath
	} else {
		from = "the default filename, resolved against the current directory"
	}
	if _, err := os.Stat(path); err == nil {
		exists = true
	}
	return path, exists, from
}

func dsnSource(env func(string) string, cfg Config) string {
	if v := env(envDatabaseURL); v != "" {
		return "$" + envDatabaseURL
	}
	if cfg.StorageDSN != "" {
		return "[storage].dsn in the config file"
	}
	if cfg.Server.Namespace != "" {
		return fmt.Sprintf("the built-in default for namespace %q", cfg.Server.Namespace)
	}
	return "the built-in default (no namespace)"
}

// announceResolution is THE seam every command goes through to learn which
// database it is about to touch. It deliberately bundles loadConfig + the
// retired-key warnings + resolveDSN + the announcement into one call, so that a
// caller CANNOT resolve a DSN and forget to say so — the two are the same
// statement. TestEveryDSNResolutionIsAnnounced keeps it that way.
//
// name is the subcommand as the user typed it ("migrate", "set-password", …),
// because the line is read by someone who is trying to work out what they just
// ran. rc is non-zero only when loadConfig itself failed (a MALFORMED config —
// missing is not an error), and in that case the error is already printed.
func announceResolution(name string, env func(string) string, out io.Writer) (cfg Config, dsn string, rc int) {
	cfgPath, cfgExists, cfgFrom := configSource(env)
	cfg, warnings, err := loadConfig(cfgPath)
	if err != nil {
		fmt.Fprintf(out, "[ocserverd] FATAL: %v\n", err)
		return cfg, "", 1
	}
	for _, w := range warnings {
		fmt.Fprintf(out, "[ocserverd] WARN: %s\n", w)
	}
	dsn = resolveDSN(env, cfg)

	if cfgExists {
		fmt.Fprintf(out, "[ocserverd] %s: config file = %s (from %s)\n", name, cfgPath, cfgFrom)
	} else {
		// The wording is deliberately not "ERROR" or even "WARN". On a normal
		// install this is the correct and expected state, and crying wolf here
		// would teach people to ignore the very line that is supposed to stop
		// them. It states the fact and where it looked.
		wd, wdErr := os.Getwd()
		where := cfgPath
		if wdErr == nil && !filepath.IsAbs(cfgPath) {
			where = filepath.Join(wd, cfgPath)
		}
		fmt.Fprintf(out, "[ocserverd] %s: config file = none (looked at %s, from %s)\n", name, where, cfgFrom)
	}
	fmt.Fprintf(out, "[ocserverd] %s: database    = %s (from %s)\n", name, dsn, dsnSource(env, cfg))
	return cfg, dsn, 0
}
