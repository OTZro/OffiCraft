package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newWiredTestServerWithDB is newWiredTestServer plus the raw *sql.DB — this
// guard has to break a write and then count rows, which is exactly the handle
// the shared helper does not hand back.
func newWiredTestServerWithDB(t *testing.T) (*httptest.Server, []byte, *sql.DB) {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "orphan-guard.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	dal := NewDAL(db)
	if err := seedOutOfBox(dal); err != nil {
		t.Fatalf("seed: %v", err)
	}
	secret := []byte(interopSecret)
	api := newAPIServer(dal, NewHub(), secret, 3600, "../..")
	h, err := buildHandler(specsFor(api), secret, dal.GetMember, nil)
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}
	api.loopback = h
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, secret, db
}

// replyCardIDFromJSON reads the card id out of a create response. Hand-rolled
// index arithmetic got this wrong once already and, because the surrounding
// assertion only checked "status >= 500", the test passed while never touching
// a real card — a green that proved nothing.
func replyCardIDFromJSON(t *testing.T, body string) string {
	t.Helper()
	var card struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(body), &card); err != nil {
		t.Fatalf("decode card: %v %s", err, body)
	}
	if !strings.HasPrefix(card.ID, "rc-") {
		t.Fatalf("card id must look like rc-…, got %q from %s", card.ID, body)
	}
	return card.ID
}

// breakWrites makes every INSERT/UPDATE on one table abort, leaving SELECTs
// working. That distinction is the whole point (review T1): the previous
// version renamed the table instead, which also broke the route's OWN lookup —
// on the answer face the 500 then came from reading the card, no attachment was
// ever decoded, and the guard could not go red for ANY implementation. A
// trigger fails the write and only the write.
func breakWrites(t *testing.T, db *sql.DB, table string) func() {
	t.Helper()
	for _, when := range []string{"INSERT", "UPDATE"} {
		stmt := fmt.Sprintf(
			`CREATE TRIGGER oc_break_%s_%s BEFORE %s ON %s
			 BEGIN SELECT RAISE(ABORT, 'injected write failure'); END`,
			strings.ToLower(when), table, when, table)
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("arm %s trigger on %s: %v", when, table, err)
		}
	}
	return func() {
		for _, when := range []string{"INSERT", "UPDATE"} {
			if _, err := db.Exec(fmt.Sprintf(`DROP TRIGGER oc_break_%s_%s`,
				strings.ToLower(when), table)); err != nil {
				t.Fatalf("disarm %s trigger on %s: %v", when, table, err)
			}
		}
	}
}

func countRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// attachmentFace is one request that carries an attachment, plus the table
// holding the record that names it.
type attachmentFace struct {
	name        string
	recordTable string
	// setup runs BEFORE the row baseline is taken and before any write is
	// broken, and returns whatever id send needs. Rows it writes (the card a
	// answer answers, the task a message hangs off) must not be counted as
	// damage — getting this wrong made three sub-cases fail against correct
	// code, which is the same "the setup is part of the measurement" mistake
	// the positive control exists to catch.
	setup func(t *testing.T, srv *httptest.Server, secret []byte) string
	send  func(t *testing.T, srv *httptest.Server, secret []byte, id string) (int, string)
}

func attachmentFaces() []attachmentFace {
	const inline = `{"data_b64":"aGVsbG8=","filename":"a.txt"}`
	post := func(path, body string, asOwner bool) func(*testing.T, *httptest.Server, []byte, string) (int, string) {
		return func(t *testing.T, srv *httptest.Server, secret []byte, _ string) (int, string) {
			now := time.Now().Unix()
			sub, scope := "mira", "agent"
			if asOwner {
				sub, scope = wireOwnerID, "owner"
			}
			tok, _ := mintJWT(sub, scope, 300, secret, now, "")
			return doRaw(t, "POST", srv.URL+path, tok, "application/json", []byte(body))
		}
	}
	return []attachmentFace{
		{
			name: "post_chat", recordTable: "chat_message",
			send: post("/api/chat",
				`{"to":"owner","body":"see attached","attachments":[`+inline+`]}`, false),
		},
		{
			name: "reply card create", recordTable: "reply_card",
			send: post("/api/reply-cards",
				`{"kind":"decision","summary":"ship it?","options":["yes","no"],"attachments":[`+inline+`]}`, false),
		},
		{
			// Review T2: this face was named as fixed but had no orphan case,
			// and reverting it to blob-then-record kept the whole suite green.
			name: "post_task_message", recordTable: "chat_message",
			setup: func(t *testing.T, srv *httptest.Server, secret []byte) string {
				now := time.Now().Unix()
				ownerTok, _ := mintJWT(wireOwnerID, "owner", 300, secret, now, "")
				status, resp := doRaw(t, "POST", srv.URL+"/api/tasks", ownerTok,
					"application/json",
					[]byte(`{"title":"orphan guard","executor_member_id":"mira"}`))
				if status != 200 {
					t.Fatalf("create task: %d %s", status, resp)
				}
				// The create response wraps the row: {"task":{…},"deduped":…}.
				var created struct {
					Task struct {
						ID string `json:"id"`
					} `json:"task"`
				}
				if err := json.Unmarshal([]byte(resp), &created); err != nil ||
					created.Task.ID == "" {
					t.Fatalf("decode task: %v %s", err, resp)
				}
				return created.Task.ID
			},
			send: func(t *testing.T, srv *httptest.Server, secret []byte, taskID string) (int, string) {
				now := time.Now().Unix()
				ownerTok, _ := mintJWT(wireOwnerID, "owner", 300, secret, now, "")
				return doRaw(t, "POST", srv.URL+"/api/tasks/"+taskID+"/message",
					ownerTok, "application/json",
					[]byte(`{"body":"see attached","attachments":[`+inline+`]}`))
			},
		},
		{
			// Review T1: the answer face's record IS the card row, so it must
			// stay readable while its write fails.
			name: "reply card answer", recordTable: "reply_card",
			setup: func(t *testing.T, srv *httptest.Server, secret []byte) string {
				now := time.Now().Unix()
				agentTok, _ := mintJWT("mira", "agent", 300, secret, now, "")
				status, resp := doRaw(t, "POST", srv.URL+"/api/reply-cards", agentTok,
					"application/json",
					[]byte(`{"kind":"decision","summary":"ship it?","options":["yes","no"]}`))
				if status != 200 {
					t.Fatalf("open card: %d %s", status, resp)
				}
				return replyCardIDFromJSON(t, resp)
			},
			send: func(t *testing.T, srv *httptest.Server, secret []byte, cardID string) (int, string) {
				now := time.Now().Unix()
				ownerTok, _ := mintJWT(wireOwnerID, "owner", 300, secret, now, "")
				return doRaw(t, "POST", srv.URL+"/api/reply-cards/"+cardID+"/answer",
					ownerTok, "application/json",
					[]byte(`{"option_idx":0,"text":"see attached","attachments":[`+inline+`]}`))
			},
		},
	}
}

// T-e2b2 / review F1 → T1, T2, T3. This is the guard that matters: it asserts
// the OUTCOME through real HTTP rather than any spelling of the write, so it
// cannot be talked around the way the AST scan was (three times).
//
// Every face is probed in BOTH directions, because "atomic" has two failure
// modes and pinning one leaves the other free — review T3 built exactly that
// mutant (record first, blobs after) and the one-directional guard stayed green:
//
//	record write fails → no blob may survive (a blob nothing names; the only
//	                     reclaim cascade walks from record refs, so it is
//	                     unreachable forever)
//	blob write fails   → no record may survive (a record naming a blob that was
//	                     never written; its reader 404s on the attachment)
//
// Each direction carries a POSITIVE CONTROL: with nothing broken, the same
// request writes exactly one blob. Without it, "an error happened AND nothing
// bad appeared" is satisfied by a request that never reached the attachment at
// all — which is exactly how the previous answer-face guard passed while being
// unable to fail (review T1).
func faceSetup(t *testing.T, face attachmentFace, srv *httptest.Server, secret []byte) string {
	t.Helper()
	if face.setup == nil {
		return ""
	}
	return face.setup(t, srv, secret)
}

func TestAttachmentWritesAreAllOrNothing(t *testing.T) {
	for _, face := range attachmentFaces() {
		t.Run(face.name, func(t *testing.T) {
			t.Run("positive control", func(t *testing.T) {
				srv, secret, db := newWiredTestServerWithDB(t)
				id := faceSetup(t, face, srv, secret)
				before := countRows(t, db, "chat_attachment")
				status, resp := face.send(t, srv, secret, id)
				if status != 200 {
					t.Fatalf("unbroken request must succeed, got %d %s", status, resp)
				}
				if got := countRows(t, db, "chat_attachment") - before; got != 1 {
					t.Fatalf("unbroken request must write exactly one blob, wrote %d "+
						"— the failure cases would then prove nothing", got)
				}
			})

			t.Run("record write fails", func(t *testing.T) {
				srv, secret, db := newWiredTestServerWithDB(t)
				id := faceSetup(t, face, srv, secret)
				before := countRows(t, db, "chat_attachment")
				disarm := breakWrites(t, db, face.recordTable)
				status, resp := face.send(t, srv, secret, id)
				disarm()
				if status < 500 {
					t.Fatalf("a broken record write must surface as an error, got %d %s",
						status, resp)
				}
				if got := countRows(t, db, "chat_attachment"); got != before {
					t.Fatalf("ORPHAN BLOB: chat_attachment went %d -> %d; the blob "+
						"outlived the record that was supposed to name it, and "+
						"nothing reclaims it", before, got)
				}
			})

			t.Run("blob write fails", func(t *testing.T) {
				srv, secret, db := newWiredTestServerWithDB(t)
				id := faceSetup(t, face, srv, secret)
				before := countRows(t, db, face.recordTable)
				disarm := breakWrites(t, db, "chat_attachment")
				status, resp := face.send(t, srv, secret, id)
				disarm()
				if status < 500 {
					t.Fatalf("a broken blob write must surface as an error, got %d %s",
						status, resp)
				}
				if got := countRows(t, db, face.recordTable); got != before {
					t.Fatalf("DANGLING REF: %s went %d -> %d; a record survived "+
						"naming a blob that was never written — its reader 404s "+
						"on the attachment", face.recordTable, before, got)
				}
			})
		})
	}
}
