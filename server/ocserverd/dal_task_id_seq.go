package main

// dal_task_id_seq.go — T-52917b 遞增票號: where a NEW task's id comes from.
//
// A task id used to be "t-" + 12 random hex. It is now "T-" + the next integer
// off a one-row counter table (migrations/00060). Old ids are NOT migrated and
// nothing here reads them: the two formats coexist because task.id is a plain
// TEXT PRIMARY KEY with no CHECK, no COLLATE and no length limit, GetTask is a
// byte-exact `WHERE id = ?`, and no table carries a foreign key into task.

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
)

// taskIDPrefix is the ONE place the new id's shape is written down.
const taskIDPrefix = "T-"

// mintRetryLimit bounds the compare-and-set loop. Inside our own IMMEDIATE
// transaction the CAS cannot lose at all (the write lock is already held and
// the write pool is one connection), so this only ever spends turns against an
// EXTERNAL writer on the same file — a `sqlite3` shell, a stray `ocserverd
// migrate`. A bound rather than a bare `for` so a pathological outsider makes
// the request fail loudly instead of hanging a connection forever.
//
// 🔴 MEASURED, so nobody has to re-derive it (T-52917b, mutation run on this
// tree): with the mint inside the transaction, DELETING the `AND next = ?`
// condition changes NOTHING observable — every test stayed green across three
// runs. That is not an argument for deleting it. It is the statement of WHICH
// of the two mechanisms is load-bearing today: the TRANSACTION is, and the CAS
// is the belt that catches the case the transaction cannot cover (a writer
// outside this process) and the case where someone later moves the mint out of
// the transaction. Take BOTH away and the same 32-request probe lost 19 rows
// while every request still answered 200 — which is exactly the shape the
// outsource-codename MAX+1 minter fails in once its scheduler mutex is removed.
const mintRetryLimit = 64

// CreateTaskMintingID mints the next id and INSERTS the task under it, both in
// ONE transaction.
//
// 🔴 THE TWO HALVES ARE NOT SEPARABLE. Minting on its own connection and then
// inserting on another would be correct-but-lossy on a crash (a burned number,
// survivable) and, worse, it invites the next reader to "simplify" the CAS away
// because a single-connection write pool makes a bare `next = next + 1` look
// safe. It is not: the READ that decides which number this task gets happens on
// a different statement from the write, the connection goes back to the pool in
// between, and two handlers that both read 7 both mint T-7. task.id is written
// through an `ON CONFLICT (id) DO UPDATE` upsert, so the second T-7 does not
// error — it OVERWRITES the first task and the API still answers 200. The
// damage is a MISSING ROW, never a duplicate one.
//
// The claim is a compare-and-set on the value THIS transaction just read:
//
//	UPDATE task_id_seq SET next = next + 1 WHERE id = 1 AND next = <what I read>
//
// 1 row affected ⇒ the number is mine. 0 ⇒ somebody moved it; re-read and try
// again. (Whether the driver's RowsAffected is trustworthy here is not taken on
// faith — TestRowsAffectedIsExactOnThisDriver in dal_task_id_seq_test.go
// measures it.)
//
// The property being defended is UNIQUENESS, not contiguity. A precheck refusal
// or a failed insert rolls the counter back and the number is reused; an
// external writer advancing it leaves a gap. Neither hurts. Two tasks called
// T-7 does.
//
// precheck runs AFTER the id exists and BEFORE the row lands, so a gate that
// needs the id can see it and a refusal still leaves no orphan task. It may be
// nil. 🔴 It runs INSIDE the write transaction on the single write connection,
// so it must not touch the database — any write would deadlock against the
// transaction holding the lock, and any read would be served from a different
// pool at a different point in time. Return AbortCreate(err) to refuse.
//
// A precheck error rolls the transaction back and is returned UNCHANGED, so the
// caller can tell its own refusal from a database failure with errors.Is and
// answer 403 rather than 500.
//
// It returns the task with ID filled in.
func (d *DAL) CreateTaskMintingID(t Task, precheck func(id string) error) (Task, error) {
	// Begin on the write pool ⇒ BEGIN IMMEDIATE (openSQLite's `_txlock=immediate`,
	// migrate.go). The write lock is taken UP FRONT, which is what makes the
	// read-then-CAS below a genuine critical section rather than a hopeful one.
	tx, err := d.wdb.Begin()
	if err != nil {
		return t, err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after a successful Commit

	n, err := mintTaskNumber(tx)
	if err != nil {
		return t, err
	}
	t.ID = taskIDPrefix + strconv.Itoa(n)

	if precheck != nil {
		if err := precheck(t.ID); err != nil {
			return t, err // rollback returns the number
		}
	}
	if err := putTaskOn(tx, t); err != nil {
		return t, err
	}
	return t, tx.Commit()
}

// mintTaskNumber claims the next number on an OPEN transaction. Split out so
// the CAS is testable on its own and so nothing can call it without a tx in
// hand: it takes the tx, it does not open one.
func mintTaskNumber(tx *sql.Tx) (int, error) {
	for attempt := 0; attempt < mintRetryLimit; attempt++ {
		var next int
		if err := tx.QueryRow(
			`SELECT next FROM task_id_seq WHERE id = 1`).Scan(&next); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				// The single row is a schema invariant (migrations/00060 seeds it and
				// the CHECK forbids a second). Absent ⇒ the file is not the schema we
				// think it is; say so rather than inventing a counter.
				return 0, errors.New("task_id_seq row is missing — database not migrated")
			}
			return 0, err
		}
		res, err := tx.Exec(
			`UPDATE task_id_seq SET next = next + 1 WHERE id = 1 AND next = ?`, next)
		if err != nil {
			return 0, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, err
		}
		if n == 1 {
			return next, nil
		}
	}
	return 0, fmt.Errorf(
		"could not claim a task number in %d attempts — task_id_seq is being "+
			"advanced by another writer faster than this transaction can claim it",
		mintRetryLimit)
}

// errOutsourceGateDenied is the sentinel the create handler's spawn-gate
// precheck refuses with, so a 403 stays a 403 after travelling out through
// CreateTaskMintingID's error return instead of becoming a 500.
var errOutsourceGateDenied = errors.New("outsource spawn gate denied")
