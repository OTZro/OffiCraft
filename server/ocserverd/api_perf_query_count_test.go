package main

// api_perf_query_count_test.go — T-a3e4 的收尾:把「查詢次數」本身變成被守住的性質。
//
// 為什麼要多這一個檔案:`api_perf_status_set_test.go` 釘的是 dep 解析的
// 「答案」(被 filter 排除的 dep 也講得出名字),而那個性質**逐 dep
// `s.dal.GetTask(id)` 一樣答得出來**——只是每多一個 dep 就多一次 round trip。
// review 實測過:把 handler 的 join 改寫成逐 dep 查詢,整包 `go test` 一條都沒紅。
// 這是效能票,payload 修好卻換來 N+1 等於沒修,所以這裡量的是**次數**,不是答案。
//
// 量測面是 database/sql 的 **driver seam**,不是 DAL 上的欄位:計數器活在測試檔裡,
// production 一個 byte 都沒動,而且它是**文字比對 SQL**、不是掛在某個 DAL method 上,
// 所以 `GetTask`、手寫一句 `SELECT`、或任何別的 DAL method 走的都是同一條路。
//
// 🔴 **但它不是「看得到每一條」——它認的是一份具名的形狀清單,射程之外一律漏數,
// 而漏數的方向是誤綠。** 射程與邊界由 `TestTaskTableReadPatternKnowsItsOwnBoundary`
// 逐條釘住(那是護欄自己的測試:邊界要被斷言,不能只寫在註解裡)。**仍在射程外**的
// 已知形狀:讀取透過 VIEW / CTE 名字 / 子查詢別名間接發生(`FROM (SELECT … )` 之後
// 對別名取用)、`FROM /*註解*/ task` 這種中間夾註解、以及本 package 以外的碼。
// ⚠️ **部分漏數比 seam 全死更危險**:合法的 `ListTasks` 那一次仍會被數到,所以下面
// 那道 `> 0` 自檢**不會響**——一個用射程外寫法做的 N+1 會安靜地全綠。review 實測過
// 這件事(M7:`SELECT … FROM "task" WHERE id = ?`,舊 regexp 停在 1 次、測試全綠)。
//
// 🔴 反恆真:語料非空是**先斷言的**。如果那一跑根本沒有帶相依的任務,查詢次數當然
// 不會成長——那時「次數沒成長」什麼都沒證明。所以下面先確認回應裡真的有 dep、
// 兩個情境的 dep 數真的不同,而且計數器真的數到東西(> 0),才比較次數。

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// ── the driver seam ──────────────────────────────────────────────────────────

// taskTableRead matches a read of the `task` table itself.
//
// 涵蓋:`FROM` 與 `JOIN` 兩個動詞、可選的 schema 限定(`main.task`)、以及 SQLite
// 合法的四種 identifier 引號(`"task"` / `'task'` / 反引號 / `[task]`)。
// 排除:`task_step` / `task_dep` / `task_artifact`(輕量清單也會發的那幾句 grouped
// COUNT)——`_` 是 word character,所以 `task\b` 不匹配,加引號的分支也要求引號緊貼
// 在 `task` 之後,`"task_step"` 同樣不匹配。
//
// 🔴 這個 pattern 的射程就是這條護欄的射程,所以它自己有測試
// (`TestTaskTableReadPatternKnowsItsOwnBoundary`);射程外的形狀見檔頭。
var taskTableRead = regexp.MustCompile(
	`(?i)\b(?:from|join)\s+(?:[a-z_][a-z0-9_]*\.)?` +
		"(?:\"task\"|'task'|`task`|\\[task\\]|task\\b)")

// TestTaskTableReadPatternKnowsItsOwnBoundary 直接對字串斷言 pattern 的兩側。
//
// 為什麼要有這一條:M7 反例證明了「N+1 只要換一種合法的 identifier 寫法就整條漏數」,
// 而漏數的方向是**誤綠**。護欄的邊界必須被釘住,不能只寫在註解裡——這正是這張票的
// 精神:不要用推論代替覆蓋。
func TestTaskTableReadPatternKnowsItsOwnBoundary(t *testing.T) {
	mustMatch := []string{
		// 現行 DAL 的兩種真實寫法(改動時這兩條會先紅)。
		"SELECT id FROM task ORDER BY created_ts",
		"SELECT id FROM task WHERE id = ?",
		// M7 反例本體:雙引號 identifier,SQLite 合法。
		`SELECT title, status FROM "task" WHERE id = ?`,
		"SELECT title FROM 'task' WHERE id = ?",
		"SELECT title FROM `task` WHERE id = ?",
		"SELECT title FROM [task] WHERE id = ?",
		// schema 限定。
		"SELECT id FROM main.task WHERE id = ?",
		`SELECT id FROM main."task" WHERE id = ?`,
		// JOIN 動詞(alias 與否都算)。
		"SELECT t.id FROM task_dep d JOIN task t ON t.id = d.dep_id",
		"SELECT t.id FROM task_dep d JOIN task ON task.id = d.dep_id",
		// 大小寫與換行(SQL 常這樣排版)。
		"select id\n\tfrom\n\ttask\n\twhere id = ?",
	}
	for _, q := range mustMatch {
		if !taskTableRead.MatchString(q) {
			t.Errorf("必須算成 task 表讀取,卻漏數了(誤綠方向): %q", q)
		}
	}

	mustNotMatch := []string{
		// 輕量清單自己發的三句 grouped COUNT — 數進去會讓基準膨脹、把 N+1 藏在雜訊裡。
		"SELECT task_id, COUNT(*) FROM task_step GROUP BY task_id",
		"SELECT task_id, dep_id FROM task_dep",
		"SELECT task_id, COUNT(*) FROM task_artifact GROUP BY task_id",
		`SELECT task_id FROM "task_step"`,
		"SELECT task_id FROM main.task_dep",
		"SELECT id FROM task_manual",
		"SELECT id FROM taskish",
		// 只是提到 task 這個字,不是從它讀。
		"SELECT task_id FROM chat_message",
		"UPDATE task SET title = ?",
	}
	for _, q := range mustNotMatch {
		if taskTableRead.MatchString(q) {
			t.Errorf("不該算成 task 表讀取,卻數進去了: %q", q)
		}
	}
}

// queryCounter counts task-table reads while armed. Arming is explicit so that
// migrations and seeding (which run before the request under test) cannot
// contribute — the number has to be "what ONE list request costs".
type queryCounter struct {
	mu      sync.Mutex
	armed   bool
	n       int
	seenSQL []string
}

func (c *queryCounter) note(q string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.armed && taskTableRead.MatchString(q) {
		c.n++
		c.seenSQL = append(c.seenSQL, strings.Join(strings.Fields(q), " "))
	}
}

// arm resets and starts counting; the returned func stops it and reports the
// count plus the statements it saw (the statements make a failure readable).
func (c *queryCounter) arm() func() (int, []string) {
	c.mu.Lock()
	c.armed, c.n, c.seenSQL = true, 0, nil
	c.mu.Unlock()
	return func() (int, []string) {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.armed = false
		return c.n, c.seenSQL
	}
}

var (
	countingOnce     sync.Once
	countingInnerErr error
	countingRegistry struct {
		mu sync.Mutex
		m  map[string]*queryCounter
	}
)

const countingDriverName = "sqlite-taskquerycount"

// registerCountingDriver wraps the real sqlite driver ONCE under a second name.
// The inner driver is taken from a throwaway *sql.DB rather than imported, so
// this file stays independent of which sqlite package the server picks.
func registerCountingDriver() error {
	countingOnce.Do(func() {
		probe, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			countingInnerErr = err
			return
		}
		inner := probe.Driver()
		probe.Close()
		countingRegistry.m = map[string]*queryCounter{}
		sql.Register(countingDriverName, countingDriver{inner: inner})
	})
	return countingInnerErr
}

func counterFor(dsn string) *queryCounter {
	countingRegistry.mu.Lock()
	defer countingRegistry.mu.Unlock()
	c := countingRegistry.m[dsn]
	if c == nil {
		c = &queryCounter{}
		countingRegistry.m[dsn] = c
	}
	return c
}

type countingDriver struct{ inner driver.Driver }

func (d countingDriver) Open(dsn string) (driver.Conn, error) {
	c, err := d.inner.Open(dsn)
	if err != nil {
		return nil, err
	}
	return &countingConn{inner: c, ctr: counterFor(dsn)}, nil
}

type countingConn struct {
	inner driver.Conn
	ctr   *queryCounter
}

func (c *countingConn) Prepare(q string) (driver.Stmt, error) {
	st, err := c.inner.Prepare(q)
	if err != nil {
		return nil, err
	}
	return &countingStmt{inner: st, sql: q, ctr: c.ctr}, nil
}

func (c *countingConn) PrepareContext(ctx context.Context, q string) (driver.Stmt, error) {
	if p, ok := c.inner.(driver.ConnPrepareContext); ok {
		st, err := p.PrepareContext(ctx, q)
		if err != nil {
			return nil, err
		}
		return &countingStmt{inner: st, sql: q, ctr: c.ctr}, nil
	}
	return c.Prepare(q)
}

func (c *countingConn) Close() error              { return c.inner.Close() }
func (c *countingConn) Begin() (driver.Tx, error) { return c.inner.Begin() }

func (c *countingConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if b, ok := c.inner.(driver.ConnBeginTx); ok {
		return b.BeginTx(ctx, opts)
	}
	return c.inner.Begin()
}

type countingStmt struct {
	inner driver.Stmt
	sql   string
	ctr   *queryCounter
}

func (s *countingStmt) Close() error  { return s.inner.Close() }
func (s *countingStmt) NumInput() int { return s.inner.NumInput() }

func (s *countingStmt) Exec(args []driver.Value) (driver.Result, error) {
	return s.inner.Exec(args)
}

func (s *countingStmt) Query(args []driver.Value) (driver.Rows, error) {
	s.ctr.note(s.sql)
	return s.inner.Query(args)
}

func (s *countingStmt) ExecContext(
	ctx context.Context, args []driver.NamedValue,
) (driver.Result, error) {
	if e, ok := s.inner.(driver.StmtExecContext); ok {
		return e.ExecContext(ctx, args)
	}
	vals, err := namedToValues(args)
	if err != nil {
		return nil, err
	}
	return s.inner.Exec(vals)
}

func (s *countingStmt) QueryContext(
	ctx context.Context, args []driver.NamedValue,
) (driver.Rows, error) {
	s.ctr.note(s.sql)
	if q, ok := s.inner.(driver.StmtQueryContext); ok {
		return q.QueryContext(ctx, args)
	}
	vals, err := namedToValues(args)
	if err != nil {
		return nil, err
	}
	return s.inner.Query(vals)
}

func namedToValues(args []driver.NamedValue) ([]driver.Value, error) {
	out := make([]driver.Value, len(args))
	for _, a := range args {
		if a.Name != "" {
			return nil, errors.New("counting driver: named args unsupported")
		}
		if a.Ordinal < 1 || a.Ordinal > len(out) {
			return nil, fmt.Errorf("counting driver: bad ordinal %d", a.Ordinal)
		}
		out[a.Ordinal-1] = a.Value
	}
	return out, nil
}

// newCountingDAL is newTestDAL with the counting driver in front of it. Same
// DSN as openSQLite so the file behaves identically (WAL, immediate txlock).
func newCountingDAL(t *testing.T) (*DAL, *queryCounter) {
	t.Helper()
	if err := registerCountingDriver(); err != nil {
		t.Fatalf("register counting driver: %v", err)
	}
	path := filepath.Join(t.TempDir(), "querycount.db")
	dsn := "file:" + path +
		"?_pragma=busy_timeout(5000)&_pragma=journal_mode(" + sqliteJournalMode + ")&_txlock=immediate"
	ctr := counterFor(dsn)
	db, err := sql.Open(countingDriverName, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	return NewDAL(db), ctr
}

// ── the test ─────────────────────────────────────────────────────────────────

// seedDepFanout creates one live task blocked by depN DONE tasks (plus the
// blockers themselves, which the status filter will exclude from the response
// — that is the shape the dep join exists for). Returns the blocked task id.
func seedDepFanout(t *testing.T, s *apiServer, depN int) string {
	t.Helper()
	blockedID := "t-fanout000001"
	if err := s.dal.PutTask(Task{
		ID: blockedID, Title: "被擋的", Status: TaskStatusInProgress,
		Priority: TaskPriorityMid, ExecutorKind: TaskExecutorMember,
		ExecutorID: "m-1", CreatedTS: 1000, UpdatedTS: 1000,
	}); err != nil {
		t.Fatal(err)
	}
	deps := make([]string, 0, depN)
	for i := 0; i < depN; i++ {
		id := fmt.Sprintf("t-dep%010d", i)
		if err := s.dal.PutTask(Task{
			ID: id, Title: fmt.Sprintf("阻擋者 %d", i), Status: TaskStatusDone,
			Priority: TaskPriorityMid, ExecutorKind: TaskExecutorMember,
			ExecutorID: "m-1", CreatedTS: 100, UpdatedTS: 100, ClosedTS: 200,
		}); err != nil {
			t.Fatal(err)
		}
		deps = append(deps, id)
	}
	if err := s.dal.ReplaceTaskDeps(blockedID, deps); err != nil {
		t.Fatal(err)
	}
	return blockedID
}

// listTaskReadsFor runs ONE ?statuses=in_progress list request against a fresh
// database seeded with depN deps, and returns (task-table reads, resolved deps
// observed on the wire, the statements it counted).
func listTaskReadsFor(t *testing.T, depN int) (reads, resolvedDeps int, stmts []string) {
	t.Helper()
	dal, ctr := newCountingDAL(t)
	s := &apiServer{dal: dal, hub: NewHub()}
	blockedID := seedDepFanout(t, s, depN)

	stop := ctr.arm()
	rows := listTaskRows(t, s, HandleListTasksApiTasksGetParams{
		Statuses: strsptr(TaskStatusInProgress),
	})
	reads, stmts = stop()

	if len(rows) != 1 || rows[0].ID != blockedID {
		t.Fatalf("depN=%d: expected only the blocked row, got %v", depN, idsOf(rows))
	}
	// 🔴 語料自證:每個 dep 都必須真的被解析出來(有標題)。沒有這一段,一個
	// 「dep 一個都沒帶」的跑法會讓次數斷言恆真地過。
	for _, d := range rows[0].DepTasks {
		if d.Title == "" || d.Status == "" {
			t.Fatalf("depN=%d: dep %s 沒被解析,語料不合格:%+v", depN, d.ID, d)
		}
		resolvedDeps++
	}
	if resolvedDeps != depN {
		t.Fatalf("depN=%d: 只解析到 %d 個 dep — 語料不合格", depN, resolvedDeps)
	}
	return reads, resolvedDeps, stmts
}

func TestTaskListTaskReadsDoNotGrowWithDepCount(t *testing.T) {
	const fewDeps, manyDeps = 2, 25

	fewReads, fewResolved, fewStmts := listTaskReadsFor(t, fewDeps)
	manyReads, manyResolved, manyStmts := listTaskReadsFor(t, manyDeps)

	// ── 語料非空(先於任何次數斷言)────────────────────────────────────────
	if fewResolved == 0 || manyResolved <= fewResolved {
		t.Fatalf("語料不合格:兩跑必須都帶 dep 且數量不同(few=%d many=%d)",
			fewResolved, manyResolved)
	}
	// 計數器自己也要自證活著:如果 seam 死了(driver 沒被套上、regexp 不match),
	// 0 == 0 會讓下面的等式恆真地過。
	if fewReads == 0 {
		t.Fatalf("計數器沒數到任何 task 讀取 — seam 壞了,這一跑什麼都沒證明")
	}

	// ── 被守住的性質 ─────────────────────────────────────────────────────
	// MUTANT(review 實測過的那一個):把 handler 建 byID 的那一段換成逐 dep
	// `s.dal.GetTask(id)`,manyReads 會從 fewReads 拉開 23 次,這裡就紅。
	if manyReads != fewReads {
		t.Fatalf("一次 list 請求的 task 讀取次數隨 dep 數成長了:%d 個 dep → %d 次,"+
			"%d 個 dep → %d 次(N+1)\nfew:\n  %s\nmany:\n  %s",
			fewDeps, fewReads, manyDeps, manyReads,
			strings.Join(fewStmts, "\n  "), strings.Join(manyStmts, "\n  "))
	}
	// 而且它是一個小常數,不是「同樣地大」。ListTasks 是唯一該打到 task 表的
	// 那一句;寬到 2 是留給未來一句合理的拆分,25 個 dep 的 N+1 進不來。
	if fewReads > 2 {
		t.Fatalf("一次 list 請求打了 %d 次 task 表,預期 1 次(ListTasks):\n  %s",
			fewReads, strings.Join(fewStmts, "\n  "))
	}
}
