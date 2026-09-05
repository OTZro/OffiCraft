// api/backupRetain.ts — the frontend's ONE copy of the backup retention range
// (T-8).
//
// ⚠️ THE AUTHORITY IS THE SERVER, NOT THIS FILE. `backupRetainDefault` /
// `minBackupRetain` / `maxBackupRetain` in server/ocserverd/backup.go decide
// what a PATCH is allowed to write; these constants exist only so the settings
// field can refuse an out-of-range value before the owner clicks save, instead
// of handing them an HTTP 422 that reads like a broken system.
//
// 🔴 THE TWO THINGS THE NUMBER DOES NOT SAY, repeated here because this file is
// where a developer looks before writing the label:
//
//   - N COUNTS VERSIONS, NOT DAYS. It is a count of FILES. How much calendar
//     they span depends entirely on how many backups those days produced —
//     measured on the studio's own machine, 19 backups on 2026-08-19 and 4 on
//     2026-08-24. A label that implies a fixed time depth is a lie.
//   - N IS PER POOL, NOT PER DIRECTORY. Routine backups (scheduled + manual)
//     and pre-migration backups hold separate quotas, so N = 5 keeps up to TEN
//     files. A label that implies the directory holds N files is a lie.
//
// Both are said out loud in the settings copy (i18n `settings.backupRetainSub`)
// rather than only here, because the person who has to know is the one turning
// the knob, not the one reading this file.

/** The shipped default — the owner's number from 2026-07-31 (T-ada9). Used only
 * as the fallback for a caller with no server value yet; the number in force
 * always arrives on GET /api/settings. */
export const BACKUP_RETAIN_DEFAULT = 5;

/** The adjustable range, mirroring the server's 422. The floor is 1 (0 would
 * delete the snapshot that was just taken); the ceiling is a disk budget —
 * steady state is 2 × N × one snapshot. */
export const BACKUP_RETAIN_MIN = 1;
export const BACKUP_RETAIN_MAX = 20;
