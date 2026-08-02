package main

// restore.go — putting a backup file BACK, and the two facts that shape the
// whole design (T-a90f).
//
// WHY this is not the mirror image of backup.go: taking a snapshot is
// something the running server can do to itself (VACUUM INTO reads; the
// server keeps serving). Putting one back is not. A SQLite database file
// cannot be swapped underneath a process that has it open — the open handles
// keep pointing at the replaced inode and the -wal sidecar no longer belongs
// to the file it names. So "restore" unavoidably means A RESTART, and the
// cockpit button must say so rather than promise something it cannot do.
//
// Shape of one restore, and WHY it is split across two process lifetimes:
//
//	while serving (stageRestore, called by the API handler)
//	  1. validate the requested file: a bare backup FILE NAME from this
//	     station's own backup directory — never a path the caller composed.
//	  2. take a pre-restore snapshot of the CURRENT database, so the restore
//	     itself can be undone (owner's ruling: 還原前自動先備一份現況). It
//	     lands in its OWN rotation pool — see backupPoolPreRestore.
//	  3. copy the chosen backup to a STAGED file beside the database
//	     (`.partial` then rename, the same crash-safe idiom backup.go uses)
//	     and integrity-check the copy. A corrupt or truncated source is
//	     refused HERE, while the live database is still untouched.
//	  4. write the command file. Only now is a restore "ordered".
//	  5. re-exec (the caller's job — the same syscall.Exec the upgrade path
//	     uses, which is correct under launchd AND for a hand-run server).
//
//	at boot (applyPendingRestore, called before the database is opened)
//	  6. nothing holds the file yet, so the swap is a plain rename: the live
//	     database (and its -wal/-shm) MOVE into trash/, the staged file takes
//	     their place, and the command file is consumed into trash/ so it can
//	     never replay.
//
// 🔴 WHY the command is a FILE and not a row in the `setting` table: the
// command has to survive the replacement of the very database it describes.
// A row would be overwritten by the restored file's own copy of that row —
// the instruction would be erased by the act it orders.
//
// 🔴 Nothing here deletes. The replaced database moves to trash/ exactly like
// a rotated backup does (repo rule: the server does not `rm`; reclamation is
// the warden's job). This matters most on the unhappy path: a bug in a mover
// leaves the old database findable, the same bug in a deleter is the one
// failure this whole feature exists to prevent.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// restoreStagedName is the verified copy waiting to become the database.
	// It sits BESIDE the database (same directory, same filesystem) so the
	// boot-time swap is an atomic rename rather than a copy that can be
	// interrupted half way.
	restoreStagedName = "restore-staged.db"

	// restoreCommandName is the order itself. Its presence at boot — and
	// nothing else — is what makes a restore happen.
	restoreCommandName = "restore.pending"
)

// restoreCommand is what one ordered restore records. It is written while the
// old station is still alive and read by the next process image, so every
// field is something the reader cannot work out for itself.
type restoreCommand struct {
	// Source is the backup file name the owner chose (bare name, no path).
	Source string `json:"source"`
	// PreRestore is the snapshot of the state being replaced — the way back
	// from the restore. Empty means the snapshot could not be taken, which
	// stageRestore treats as fatal, so a written command always names one.
	PreRestore string `json:"pre_restore"`
	// OrderedTS is when the restore was ordered (unix seconds).
	OrderedTS float64 `json:"ordered_ts"`
}

// commandingDisarmed reads the live "do not command anything out there" state
// under the settings snapshot lock. Every choke point calls THIS rather than
// the field, so the owner's re-arm takes effect without a restart.
func (s *apiServer) commandingDisarmed() bool {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.commandDisarmed
}

func restoreStagedPath(dbPath string) string {
	return filepath.Join(filepath.Dir(dbPath), restoreStagedName)
}

func restoreCommandPath(dbPath string) string {
	return filepath.Join(filepath.Dir(dbPath), restoreCommandName)
}

// validBackupFileName is the ONE gate between a caller-supplied string and the
// filesystem. It accepts only a bare name that this station's own backup
// engine could have produced: no directory separators, no `..`, the engine's
// prefix and suffix, and a parseable stamp.
//
// 🔴 It deliberately does not accept a path. "Restore from this file" with a
// caller-composed path is how a restore endpoint becomes an arbitrary-file
// read; the caller names a file IN the backup directory or it names nothing.
func validBackupFileName(name string) error {
	if name == "" {
		return fmt.Errorf("no backup file named")
	}
	if strings.ContainsRune(name, os.PathSeparator) || strings.Contains(name, "/") ||
		name != filepath.Base(name) || strings.Contains(name, "..") {
		return fmt.Errorf("%q is not a bare backup file name", name)
	}
	if !strings.HasPrefix(name, backupFilePrefix) || !strings.HasSuffix(name, backupFileSuffix) {
		return fmt.Errorf("%q is not one of this station's backups", name)
	}
	if _, ok := parseBackupStamp(name); !ok {
		return fmt.Errorf("%q does not carry a backup timestamp", name)
	}
	return nil
}

// verifySQLiteFile opens a file as SQLite and asks it whether it is intact.
// A restore whose source is corrupt is worse than no restore at all: it
// replaces a working database with a broken one and reports success. This
// runs on the STAGED COPY, before the command file exists, so a failure here
// leaves the live database untouched and nothing ordered.
var verifySQLiteFile = func(path string) error {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return fmt.Errorf("open %s: %w", filepath.Base(path), err)
	}
	defer db.Close()
	var result string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&result); err != nil {
		return fmt.Errorf("integrity check on %s: %w", filepath.Base(path), err)
	}
	if result != "ok" {
		return fmt.Errorf("%s fails its integrity check (%s)", filepath.Base(path), result)
	}
	return nil
}

// copyFile streams src to dst via a `.partial` name, renaming on success —
// the same idiom runDatabaseBackup uses, for the same reason: a half-written
// file must never be able to sit at the final name looking complete.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	partial := dst + ".partial"
	_ = os.Remove(partial)
	out, err := os.OpenFile(partial, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(partial)
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		_ = os.Remove(partial)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(partial)
		return err
	}
	if err := os.Rename(partial, dst); err != nil {
		_ = os.Remove(partial)
		return err
	}
	return nil
}

// stageRestore performs steps 1-4 (see the file header) and returns the
// command it wrote. Every failure leaves the live database untouched and
// nothing ordered — the command file is the LAST thing written.
func stageRestore(db *sql.DB, dbPath, sourceName string, now time.Time) (restoreCommand, error) {
	var cmd restoreCommand
	if err := validBackupFileName(sourceName); err != nil {
		return cmd, err
	}
	source := filepath.Join(backupDirFor(dbPath), sourceName)
	if _, err := os.Stat(source); err != nil {
		return cmd, fmt.Errorf("no such backup: %s", sourceName)
	}

	// The way back from this restore. Fatal on failure: the owner's ruling was
	// that the restore itself must be undoable, and a restore that quietly
	// skipped its own retreat point would look identical to one that had it.
	pre, err := runDatabaseBackup(db, dbPath, backupReasonPreRestore, now)
	if err != nil {
		return cmd, fmt.Errorf("could not snapshot the current database before restoring: %w", err)
	}
	if pre.Skipped != "" {
		return cmd, fmt.Errorf("could not snapshot the current database before restoring: %s", pre.Skipped)
	}

	staged := restoreStagedPath(dbPath)
	if err := copyFile(source, staged); err != nil {
		return cmd, fmt.Errorf("stage %s: %w", sourceName, err)
	}
	if err := verifySQLiteFile(staged); err != nil {
		// Do not leave a bad staged file lying around next to a command that
		// might later be written by hand.
		_ = os.Rename(staged, staged+".rejected")
		return cmd, err
	}

	cmd = restoreCommand{
		Source:     sourceName,
		PreRestore: filepath.Base(pre.Path),
		OrderedTS:  float64(now.UnixNano()) / 1e9,
	}
	blob, err := json.Marshal(cmd)
	if err != nil {
		return restoreCommand{}, err
	}
	if err := os.WriteFile(restoreCommandPath(dbPath)+".partial", blob, 0o600); err != nil {
		return restoreCommand{}, fmt.Errorf("write restore command: %w", err)
	}
	if err := os.Rename(restoreCommandPath(dbPath)+".partial", restoreCommandPath(dbPath)); err != nil {
		return restoreCommand{}, fmt.Errorf("publish restore command: %w", err)
	}
	log.Printf("[restore] ordered: %s (way back: %s); applying on the next boot", sourceName, cmd.PreRestore)
	return cmd, nil
}

// disarmAfterRestore is the bookkeeping a restored station does to its OWN
// (restored) database before anything is served — see the call site in
// cmdServe for the ordering constraints, and settingCommandDisarmed for why
// the row is written rather than trusted.
//
// Split out of cmdServe so it can be tested directly: the alternative is
// asserting on a boot, and a test that has to boot a server to check a safety
// property tends not to get written.
func disarmAfterRestore(dal *DAL, now time.Time) (droppedCommands int64, err error) {
	if err := dal.PutSetting(settingCommandDisarmed, "true"); err != nil {
		return 0, fmt.Errorf("disarm outbound commanding: %w", err)
	}
	n, err := dal.DeleteWardenCommandsBefore(float64(now.Unix()))
	if err != nil {
		return 0, fmt.Errorf("drop the queued machine commands: %w", err)
	}
	return n, nil
}

// moveIntoTrash moves one path into the station's trash directory under a
// stamped name. Missing sources are not an error (a database with no -wal is
// normal). Returns the destination for logging, "" when there was nothing to
// move.
func moveIntoTrash(dbPath, from string, now time.Time) (string, error) {
	if _, err := os.Stat(from); err != nil {
		return "", nil
	}
	trash := backupTrashFor(dbPath)
	if err := os.MkdirAll(trash, 0o700); err != nil {
		return "", fmt.Errorf("create trash dir: %w", err)
	}
	to := filepath.Join(trash, fmt.Sprintf("replaced-%s-%s", now.UTC().Format("20060102-150405"), filepath.Base(from)))
	if err := os.Rename(from, to); err != nil {
		return "", fmt.Errorf("move %s aside: %w", filepath.Base(from), err)
	}
	return to, nil
}

// applyPendingRestore is step 6 — the boot-time half.
//
// 🔴 It MUST be called before the database is opened. That is the entire
// reason the restore is split in two: here, and only here, no process holds
// the file, so replacing it is a rename instead of an impossibility.
//
// It returns the command it applied (nil when there was nothing to do), so
// the caller can record that this boot came up on restored data.
//
// Fail-closed: anything unexpected (no staged file, an unreadable command)
// consumes the command WITHOUT touching the live database. A restore that
// cannot be performed correctly must leave the station exactly as it was, not
// half-swapped — and it must not retry forever on every boot.
func applyPendingRestore(dbPath string, now time.Time) (*restoreCommand, error) {
	cmdPath := restoreCommandPath(dbPath)
	blob, err := os.ReadFile(cmdPath)
	if err != nil {
		return nil, nil // no command = the overwhelmingly common boot
	}
	var cmd restoreCommand
	if err := json.Unmarshal(blob, &cmd); err != nil {
		if _, mErr := moveIntoTrash(dbPath, cmdPath, now); mErr != nil {
			log.Printf("[restore] could not consume the unreadable command file: %v", mErr)
		}
		return nil, fmt.Errorf("restore command is unreadable, so nothing was restored: %w", err)
	}

	staged := restoreStagedPath(dbPath)
	if _, err := os.Stat(staged); err != nil {
		if _, mErr := moveIntoTrash(dbPath, cmdPath, now); mErr != nil {
			log.Printf("[restore] could not consume the orphaned command file: %v", mErr)
		}
		return nil, fmt.Errorf("restore was ordered from %s but the staged file is missing, so nothing was restored", cmd.Source)
	}

	// Move the live database aside FIRST. If this fails we have changed
	// nothing; if the rename below fails, the operator has both files.
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if _, err := moveIntoTrash(dbPath, dbPath+suffix, now); err != nil {
			return nil, err
		}
	}
	if err := os.Rename(staged, dbPath); err != nil {
		return nil, fmt.Errorf("restore %s into place: %w", cmd.Source, err)
	}
	if _, err := moveIntoTrash(dbPath, cmdPath, now); err != nil {
		// The swap has already landed. Leaving the command behind would replay
		// the restore on the next boot, so this is loud.
		log.Printf("[restore] CRITICAL: %s was restored but the command file could not be consumed (%v) — remove %s by hand or the next boot will restore again",
			cmd.Source, err, cmdPath)
	}
	log.Printf("[restore] applied %s (way back: %s); outbound commanding stays disarmed until the owner re-arms it", cmd.Source, cmd.PreRestore)
	return &cmd, nil
}
