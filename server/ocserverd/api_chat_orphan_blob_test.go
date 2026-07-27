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

// newWiredTestServerWithDB is newWiredTestServer plus the raw *sql.DB — this
// guard has to break a table and then count rows, which is exactly the handle
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

// T-e2b2 / review F1, second attempt. The AST scan in
// api_chat_attachment_wiring_test.go pins a SPELLING, and the reviewer defeated
// it three ways without touching the defect's meaning: hoist the loop into a
// helper (which is literally the shape this change deleted), write the loop
// inline against the putChatAttachmentOn/putChatOn seams this change
// introduced (plain identifiers, not selectors), or take a method value. A
// guard that enumerates ways of writing something has no end.
//
// This one asserts the OUTCOME through real HTTP instead: make the record write
// fail at the database, then ask the database whether a blob appeared. Any
// implementation that writes blobs before the record fails it, however spelled;
// only a single transaction passes. The failure is injected by renaming the
// record's table out from under the handler — no seam, no fake DAL, and nothing
// the production path can bypass, because the transaction is the only thing
// standing between the blob INSERT and the missing table.
func TestFailedRecordWriteOrphansNoBlob(t *testing.T) {
	for _, face := range []struct {
		name, table, path, body string
	}{
		{
			name: "post_chat", table: "chat_message", path: "/api/chat",
			body: `{"to":"owner","body":"see attached","attachments":[{"data_b64":"aGVsbG8=","filename":"a.txt"}]}`,
		},
		{
			name: "reply card create", table: "reply_card", path: "/api/reply-cards",
			body: `{"kind":"decision","summary":"ship it?","options":["yes","no"],"attachments":[{"data_b64":"aGVsbG8=","filename":"a.txt"}]}`,
		},
	} {
		t.Run(face.name, func(t *testing.T) {
			srv, secret, db := newWiredTestServerWithDB(t)
			now := time.Now().Unix()
			agentTok, _ := mintJWT("mira", "agent", 300, secret, now, "")

			count := func() int {
				var n int
				if err := db.QueryRow(
					`SELECT COUNT(*) FROM chat_attachment`).Scan(&n); err != nil {
					t.Fatalf("count blobs: %v", err)
				}
				return n
			}
			before := count()

			// Break the LAST write of the record path — the blob INSERT still
			// succeeds, so anything short of one transaction leaves it behind.
			if _, err := db.Exec(
				fmt.Sprintf(`ALTER TABLE %s RENAME TO %s_moved`, face.table, face.table)); err != nil {
				t.Fatalf("rename %s: %v", face.table, err)
			}
			defer func() {
				if _, err := db.Exec(fmt.Sprintf(
					`ALTER TABLE %s_moved RENAME TO %s`, face.table, face.table)); err != nil {
					t.Fatalf("restore %s: %v", face.table, err)
				}
			}()

			status, resp := doRaw(t, "POST", srv.URL+face.path, agentTok,
				"application/json", []byte(face.body))
			if status < 500 {
				t.Fatalf("a broken record write must surface as an error, got %d %s",
					status, resp)
			}
			if got := count(); got != before {
				t.Fatalf("ORPHAN BLOB: chat_attachment went %d -> %d; the blob "+
					"outlived the record that was supposed to name it, and "+
					"nothing reclaims it (the only cascade walks from record refs)",
					before, got)
			}
		})
	}
}

// The answer face has its own shape: no companion message, the card row itself
// is the record naming the blobs. Same outcome assertion.
func TestFailedAnswerWriteOrphansNoBlob(t *testing.T) {
	srv, secret, db := newWiredTestServerWithDB(t)
	now := time.Now().Unix()
	agentTok, _ := mintJWT("mira", "agent", 300, secret, now, "")

	status, resp := doRaw(t, "POST", srv.URL+"/api/reply-cards", agentTok,
		"application/json",
		[]byte(`{"kind":"decision","summary":"ship it?","options":["yes","no"]}`))
	if status != 200 {
		t.Fatalf("open card: %d %s", status, resp)
	}
	cardID := replyCardIDFromJSON(t, resp)

	var before int
	if err := db.QueryRow(`SELECT COUNT(*) FROM chat_attachment`).Scan(&before); err != nil {
		t.Fatalf("count blobs: %v", err)
	}
	if _, err := db.Exec(`ALTER TABLE reply_card RENAME TO reply_card_moved`); err != nil {
		t.Fatalf("rename: %v", err)
	}
	defer func() {
		if _, err := db.Exec(`ALTER TABLE reply_card_moved RENAME TO reply_card`); err != nil {
			t.Fatalf("restore: %v", err)
		}
	}()

	ownerTok, _ := mintJWT(wireOwnerID, "owner", 300, secret, now, "")
	status, resp = doRaw(t, "POST", srv.URL+"/api/reply-cards/"+cardID+"/answer",
		ownerTok, "application/json",
		[]byte(`{"option_idx":0,"attachments":[{"data_b64":"aGVsbG8=","filename":"a.txt"}]}`))
	if status < 500 {
		t.Fatalf("a broken card write must surface as an error, got %d %s", status, resp)
	}
	var after int
	if err := db.QueryRow(`SELECT COUNT(*) FROM chat_attachment`).Scan(&after); err != nil {
		t.Fatalf("count blobs: %v", err)
	}
	if after != before {
		t.Fatalf("ORPHAN BLOB on the answer face: chat_attachment went %d -> %d",
			before, after)
	}
}
