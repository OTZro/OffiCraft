package main

import (
	"path/filepath"
	"testing"
)

func TestSaveWithDocumentHistoryKeepsThreePreWriteSnapshots(t *testing.T) {
	db, err := openSQLite(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := runMigrations(db); err != nil {
		t.Fatal(err)
	}
	dal := NewDAL(db)
	if err := dal.PutUserContext(UserContext{Text: "one"}); err != nil {
		t.Fatal(err)
	}
	for _, next := range []string{"two", "three", "four", "five"} {
		current, err := dal.GetUserContext()
		if err != nil {
			t.Fatal(err)
		}
		if err := dal.SaveWithDocumentHistory("global_context", "global", `{"text":"`+current.Text+`"}`, "owner", func(ex sqlExecer) error {
			return putUserContextOn(ex, UserContext{Text: next})
		}); err != nil {
			t.Fatal(err)
		}
	}
	history, err := dal.ListDocumentHistory("global_context", "global")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 3 {
		t.Fatalf("history count = %d, want 3", len(history))
	}
	for i, want := range []string{"four", "three", "two"} {
		if got := history[i].ContentJSON; got != `{"text":"`+want+`"}` {
			t.Errorf("history[%d] = %s, want %s", i, got, want)
		}
	}
	current, err := dal.GetUserContext()
	if err != nil {
		t.Fatal(err)
	}
	if current.Text != "five" {
		t.Fatalf("live text = %q, want five", current.Text)
	}
}
