package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // 单写协程，规避 SQLite 写锁
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	// 增量迁移：老库补列（new schema 已含该列时 ALTER 会报 duplicate column，忽略即可）
	for _, stmt := range migrations {
		if _, err := db.Exec(stmt); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
			return nil, fmt.Errorf("migrate %q: %w", stmt, err)
		}
	}
	// 上一进程被杀时 running 的行不会有任何东西去收：Prune 排除 running、
	// 统计一直把它算成"队列里"，界面上永远转圈。启动时统一收成 dead。
	if _, err := db.Exec(`UPDATE runs SET status='dead', error='进程重启，执行被中断', finished_at=?
		WHERE status='running'`, now()); err != nil {
		return nil, fmt.Errorf("recover running: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// 增量迁移：只追加，不修改历史项
var migrations = []string{
	`ALTER TABLE rules ADD COLUMN mode TEXT NOT NULL DEFAULT 'recurring'`,
	`ALTER TABLE jobs ADD COLUMN oneshot INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE jobs ADD COLUMN notify_json TEXT NOT NULL DEFAULT '[]'`,
}

const schema = `
CREATE TABLE IF NOT EXISTS endpoints (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  slug        TEXT NOT NULL UNIQUE,
  title       TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  auth        TEXT NOT NULL DEFAULT 'none',   -- none | token | hmac
  secret      TEXT NOT NULL DEFAULT '',
  max_body    INTEGER NOT NULL DEFAULT 1048576,
  retain_days INTEGER NOT NULL DEFAULT 30,
  rate_limit  INTEGER NOT NULL DEFAULT 0,   -- 每分钟允许次数，0=不限
  enabled     INTEGER NOT NULL DEFAULT 1,
  created_at  INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS requests (
  id            TEXT PRIMARY KEY,
  endpoint_id   INTEGER NOT NULL REFERENCES endpoints(id) ON DELETE CASCADE,
  method        TEXT NOT NULL,
  path          TEXT NOT NULL,
  query         TEXT NOT NULL DEFAULT '',
  headers_json  TEXT NOT NULL DEFAULT '{}',
  body          TEXT NOT NULL DEFAULT '',
  body_bytes    INTEGER NOT NULL DEFAULT 0,
  truncated     INTEGER NOT NULL DEFAULT 0,
  content_type  TEXT NOT NULL DEFAULT '',
  remote_ip     TEXT NOT NULL DEFAULT '',
  user_agent    TEXT NOT NULL DEFAULT '',
  auth_ok       INTEGER NOT NULL DEFAULT 1,
  auth_kind     TEXT NOT NULL DEFAULT '',
  payload_json  TEXT NOT NULL DEFAULT '{}',
  received_at   INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_requests_ep ON requests(endpoint_id, received_at DESC);
CREATE INDEX IF NOT EXISTS idx_requests_time ON requests(received_at DESC);

CREATE TABLE IF NOT EXISTS rules (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  endpoint_id INTEGER NOT NULL REFERENCES endpoints(id) ON DELETE CASCADE,
  name        TEXT NOT NULL,
  source      TEXT NOT NULL,          -- body | query | header
  path        TEXT NOT NULL DEFAULT '',  -- JSONPath for body, key名 for query/header
  op          TEXT NOT NULL,          -- eq | contains | regex | exists | prefix
  value       TEXT NOT NULL DEFAULT '',
  job_name    TEXT NOT NULL DEFAULT '',
  schedule    TEXT NOT NULL,          -- cron 或 @every 30s
  exec_argv   TEXT NOT NULL,          -- JSON 数组
  timeout_sec INTEGER NOT NULL DEFAULT 60,
  max_attempts INTEGER NOT NULL DEFAULT 3,
  mode        TEXT NOT NULL DEFAULT 'recurring',  -- recurring=命中一次后按 schedule 循环；per_event=每次命中建一条任务
  enabled     INTEGER NOT NULL DEFAULT 1,
  created_at  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_rules_ep ON rules(endpoint_id);

CREATE TABLE IF NOT EXISTS jobs (
  id          TEXT PRIMARY KEY,
  name        TEXT NOT NULL,
  endpoint_id INTEGER REFERENCES endpoints(id) ON DELETE SET NULL,
  rule_id     INTEGER REFERENCES rules(id) ON DELETE SET NULL,
  schedule    TEXT NOT NULL,
  exec_argv   TEXT NOT NULL,
  payload_json TEXT NOT NULL DEFAULT '{}',
  notify_json TEXT NOT NULL DEFAULT '[]',
  timeout_sec INTEGER NOT NULL DEFAULT 60,
  max_attempts INTEGER NOT NULL DEFAULT 3,
  enabled     INTEGER NOT NULL DEFAULT 1,
  oneshot     INTEGER NOT NULL DEFAULT 0,   -- 1=执行完一次自动停用，不按 schedule 重复
  next_run_at INTEGER NOT NULL,
  created_at  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_jobs_next ON jobs(enabled, next_run_at);

CREATE TABLE IF NOT EXISTS channels (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  name         TEXT NOT NULL,
  type         TEXT NOT NULL,
  config_json  TEXT NOT NULL DEFAULT '{}',
  enabled      INTEGER NOT NULL DEFAULT 1,
  last_sent_at INTEGER NOT NULL DEFAULT 0,
  last_error   TEXT NOT NULL DEFAULT '',
  created_at   INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS runs (
  id             TEXT PRIMARY KEY,
  job_id         TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  status         TEXT NOT NULL,   -- queued|running|succeeded|failed|timeout|dead|canceled
  attempt        INTEGER NOT NULL DEFAULT 1,
  max_attempts   INTEGER NOT NULL DEFAULT 3,
  exit_code      INTEGER,
  output         TEXT NOT NULL DEFAULT '',
  truncated      INTEGER NOT NULL DEFAULT 0,
  error          TEXT NOT NULL DEFAULT '',
  next_attempt_at INTEGER,
  created_at     INTEGER NOT NULL,
  started_at     INTEGER,
  finished_at    INTEGER
);
CREATE INDEX IF NOT EXISTS idx_runs_job ON runs(job_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_runs_due ON runs(status, next_attempt_at);

CREATE TABLE IF NOT EXISTS tokens (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  name         TEXT NOT NULL,
  prefix       TEXT NOT NULL DEFAULT '',
  hash         TEXT NOT NULL UNIQUE,   -- 明文 sha256，明文不落库
  role         TEXT NOT NULL DEFAULT 'operator',  -- admin | operator | viewer（仅作展示与预设）
  scopes       TEXT NOT NULL DEFAULT '',          -- 允许的端点 slug，逗号分隔；空=全部
  perms        TEXT NOT NULL DEFAULT '',          -- 细粒度权限，逗号分隔；见 tokenPerm
  enabled      INTEGER NOT NULL DEFAULT 1,
  note         TEXT NOT NULL DEFAULT '',
  last_used_at INTEGER NOT NULL DEFAULT 0,
  created_at   INTEGER NOT NULL
);
`

// ---------- 基础类型 ----------

type Endpoint struct {
	ID          int64  `json:"id"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Auth        string `json:"auth"`
	Secret      string `json:"secret,omitempty"`
	MaxBody     int64  `json:"max_body"`
	RetainDays  int64  `json:"retain_days"`
	RateLimit   int64  `json:"rate_limit"`
	Enabled     bool   `json:"enabled"`
	CreatedAt   int64  `json:"created_at"`
}

type Request struct {
	ID          string `json:"id"`
	EndpointID  int64  `json:"endpoint_id"`
	Method      string `json:"method"`
	Path        string `json:"path"`
	Query       string `json:"query"`
	Headers     string `json:"headers"`
	Body        string `json:"body"`
	BodyBytes   int64  `json:"body_bytes"`
	Truncated   bool   `json:"truncated"`
	ContentType string `json:"content_type"`
	RemoteIP    string `json:"remote_ip"`
	UserAgent   string `json:"user_agent"`
	AuthOK      bool   `json:"auth_ok"`
	AuthKind    string `json:"auth_kind"`
	Payload     string `json:"payload"`
	ReceivedAt  int64  `json:"received_at"`
}

type Rule struct {
	ID          int64  `json:"id"`
	EndpointID  int64  `json:"endpoint_id"`
	Name        string `json:"name"`
	Source      string `json:"source"`
	Path        string `json:"path"`
	Op          string `json:"op"`
	Value       string `json:"value"`
	JobName     string `json:"job_name"`
	Schedule    string `json:"schedule"`
	ExecArgv    string `json:"exec_argv"`
	TimeoutSec  int64  `json:"timeout_sec"`
	MaxAttempts int64  `json:"max_attempts"`
	Mode        string `json:"mode"`
	Enabled     bool   `json:"enabled"`
	CreatedAt   int64  `json:"created_at"`
}

type Job struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	EndpointID  *int64          `json:"endpoint_id"`
	RuleID      *int64          `json:"rule_id"`
	Schedule    string          `json:"schedule"`
	ExecArgv    string          `json:"exec_argv"`
	Payload     string          `json:"payload"`
	Notify      []NotifyBinding `json:"notify"`
	TimeoutSec  int64           `json:"timeout_sec"`
	MaxAttempts int64           `json:"max_attempts"`
	Enabled     bool            `json:"enabled"`
	OneShot     bool            `json:"oneshot"`
	NextRunAt   int64           `json:"next_run_at"`
	CreatedAt   int64           `json:"created_at"`
}

// NotifyBinding 把任务与推送渠道绑定，并按事件过滤。
// Events 取 success / failure；为空视为两者都推。
type NotifyBinding struct {
	ChannelID int64    `json:"channel_id"`
	Events    []string `json:"events"`
}

// Channel 一条推送渠道。Config 是 JSON 对象，字段随 Type 变化（见 notify.Catalog）。
type Channel struct {
	ID         int64             `json:"id"`
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	Config     map[string]string `json:"config"`
	Enabled    bool              `json:"enabled"`
	LastSentAt int64             `json:"last_sent_at"`
	LastError  string            `json:"last_error"`
	CreatedAt  int64             `json:"created_at"`
}

type Run struct {
	ID            string `json:"id"`
	JobID         string `json:"job_id"`
	JobName       string `json:"job_name,omitempty"`
	Status        string `json:"status"`
	Attempt       int64  `json:"attempt"`
	MaxAttempts   int64  `json:"max_attempts"`
	ExitCode      *int64 `json:"exit_code"`
	Output        string `json:"output"`
	Truncated     bool   `json:"truncated"`
	Error         string `json:"error"`
	NextAttemptAt *int64 `json:"next_attempt_at"`
	CreatedAt     int64  `json:"created_at"`
	StartedAt     *int64 `json:"started_at"`
	FinishedAt    *int64 `json:"finished_at"`
}

func now() int64 { return time.Now().Unix() }

func NewID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s_%x%s", prefix, time.Now().UnixMilli(), hex.EncodeToString(b)[:12])
}

// ---------- endpoints ----------

func (s *Store) ListEndpoints() ([]Endpoint, error) {
	rows, err := s.db.Query(`SELECT id,slug,title,description,auth,secret,max_body,retain_days,rate_limit,enabled,created_at FROM endpoints ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Endpoint{}
	for rows.Next() {
		var e Endpoint
		if err := rows.Scan(&e.ID, &e.Slug, &e.Title, &e.Description, &e.Auth, &e.Secret, &e.MaxBody, &e.RetainDays, &e.RateLimit, &e.Enabled, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) GetEndpointBySlug(slug string) (*Endpoint, error) {
	var e Endpoint
	err := s.db.QueryRow(`SELECT id,slug,title,description,auth,secret,max_body,retain_days,rate_limit,enabled,created_at FROM endpoints WHERE slug=?`, slug).
		Scan(&e.ID, &e.Slug, &e.Title, &e.Description, &e.Auth, &e.Secret, &e.MaxBody, &e.RetainDays, &e.RateLimit, &e.Enabled, &e.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &e, err
}

func (s *Store) GetEndpoint(id int64) (*Endpoint, error) {
	var e Endpoint
	err := s.db.QueryRow(`SELECT id,slug,title,description,auth,secret,max_body,retain_days,rate_limit,enabled,created_at FROM endpoints WHERE id=?`, id).
		Scan(&e.ID, &e.Slug, &e.Title, &e.Description, &e.Auth, &e.Secret, &e.MaxBody, &e.RetainDays, &e.RateLimit, &e.Enabled, &e.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &e, err
}

func (s *Store) UpsertEndpoint(e *Endpoint) error {
	if e.ID == 0 {
		e.CreatedAt = now()
		if e.MaxBody == 0 {
			e.MaxBody = 1048576
		}
		if e.RetainDays == 0 {
			e.RetainDays = 30
		}
		if e.Auth == "" {
			e.Auth = "none"
		}
		res, err := s.db.Exec(`INSERT INTO endpoints(slug,title,description,auth,secret,max_body,retain_days,rate_limit,enabled,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			e.Slug, e.Title, e.Description, e.Auth, e.Secret, e.MaxBody, e.RetainDays, e.RateLimit, e.Enabled, e.CreatedAt)
		if err != nil {
			return err
		}
		e.ID, _ = res.LastInsertId()
		return nil
	}
	_, err := s.db.Exec(`UPDATE endpoints SET slug=?,title=?,description=?,auth=?,secret=?,max_body=?,retain_days=?,rate_limit=?,enabled=? WHERE id=?`,
		e.Slug, e.Title, e.Description, e.Auth, e.Secret, e.MaxBody, e.RetainDays, e.RateLimit, e.Enabled, e.ID)
	return err
}

func (s *Store) DeleteEndpoint(id int64) error {
	_, err := s.db.Exec(`DELETE FROM endpoints WHERE id=?`, id)
	return err
}

// ---------- requests ----------

func (s *Store) InsertRequest(r *Request) error {
	r.ID = NewID("rq")
	r.ReceivedAt = now()
	_, err := s.db.Exec(`INSERT INTO requests(id,endpoint_id,method,path,query,headers_json,body,body_bytes,truncated,content_type,remote_ip,user_agent,auth_ok,auth_kind,payload_json,received_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.EndpointID, r.Method, r.Path, r.Query, r.Headers, r.Body, r.BodyBytes, r.Truncated,
		r.ContentType, r.RemoteIP, r.UserAgent, r.AuthOK, r.AuthKind, r.Payload, r.ReceivedAt)
	return err
}

func (s *Store) ListRequests(endpointID int64, limit int, before int64) ([]Request, error) {
	q := `SELECT id,endpoint_id,method,path,query,headers_json,body,body_bytes,truncated,content_type,remote_ip,user_agent,auth_ok,auth_kind,payload_json,received_at FROM requests`
	args := []any{}
	where := []string{}
	if endpointID > 0 {
		where = append(where, "endpoint_id=?")
		args = append(args, endpointID)
	}
	if before > 0 {
		where = append(where, "received_at < ?")
		args = append(args, before)
	}
	if len(where) > 0 {
		q += " WHERE " + joinAnd(where)
	}
	q += " ORDER BY received_at DESC LIMIT ?"
	args = append(args, limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Request{}
	for rows.Next() {
		var r Request
		if err := rows.Scan(&r.ID, &r.EndpointID, &r.Method, &r.Path, &r.Query, &r.Headers, &r.Body, &r.BodyBytes,
			&r.Truncated, &r.ContentType, &r.RemoteIP, &r.UserAgent, &r.AuthOK, &r.AuthKind, &r.Payload, &r.ReceivedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) GetRequest(id string) (*Request, error) {
	var r Request
	err := s.db.QueryRow(`SELECT id,endpoint_id,method,path,query,headers_json,body,body_bytes,truncated,content_type,remote_ip,user_agent,auth_ok,auth_kind,payload_json,received_at FROM requests WHERE id=?`, id).
		Scan(&r.ID, &r.EndpointID, &r.Method, &r.Path, &r.Query, &r.Headers, &r.Body, &r.BodyBytes, &r.Truncated,
			&r.ContentType, &r.RemoteIP, &r.UserAgent, &r.AuthOK, &r.AuthKind, &r.Payload, &r.ReceivedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &r, err
}

func joinAnd(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " AND "
		}
		out += p
	}
	return out
}

// ---------- rules ----------

func (s *Store) ListRules(endpointID int64) ([]Rule, error) {
	rows, err := s.db.Query(`SELECT id,endpoint_id,name,source,path,op,value,job_name,schedule,exec_argv,timeout_sec,max_attempts,mode,enabled,created_at FROM rules WHERE endpoint_id=? ORDER BY id`, endpointID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Rule{}
	for rows.Next() {
		var r Rule
		if err := rows.Scan(&r.ID, &r.EndpointID, &r.Name, &r.Source, &r.Path, &r.Op, &r.Value, &r.JobName,
			&r.Schedule, &r.ExecArgv, &r.TimeoutSec, &r.MaxAttempts, &r.Mode, &r.Enabled, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) CreateRule(r *Rule) error {
	r.CreatedAt = now()
	if r.TimeoutSec == 0 {
		r.TimeoutSec = 60
	}
	if r.MaxAttempts == 0 {
		r.MaxAttempts = 3
	}
	if r.Mode != "per_event" {
		r.Mode = "recurring"
	}
	res, err := s.db.Exec(`INSERT INTO rules(endpoint_id,name,source,path,op,value,job_name,schedule,exec_argv,timeout_sec,max_attempts,mode,enabled,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.EndpointID, r.Name, r.Source, r.Path, r.Op, r.Value, r.JobName, r.Schedule, r.ExecArgv, r.TimeoutSec, r.MaxAttempts, r.Mode, r.Enabled, r.CreatedAt)
	if err != nil {
		return err
	}
	r.ID, _ = res.LastInsertId()
	return nil
}

func (s *Store) DeleteRule(id int64) error {
	_, err := s.db.Exec(`DELETE FROM rules WHERE id=?`, id)
	return err
}

// ---------- jobs ----------

func (s *Store) CreateJob(j *Job) error {
	if j.ID == "" {
		j.ID = NewID("job")
	}
	if j.Name == "" {
		// 兜底名必须在 ID 生成之后拼，否则永远得到"任务 "（原 bug 顺序反了）
		j.Name = "任务 " + j.ID
	}
	j.CreatedAt = now()
	if j.TimeoutSec == 0 {
		j.TimeoutSec = 60
	}
	if j.MaxAttempts == 0 {
		j.MaxAttempts = 3
	}
	_, err := s.db.Exec(`INSERT INTO jobs(id,name,endpoint_id,rule_id,schedule,exec_argv,payload_json,notify_json,timeout_sec,max_attempts,enabled,oneshot,next_run_at,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		j.ID, j.Name, j.EndpointID, j.RuleID, j.Schedule, j.ExecArgv, j.Payload, NotifyJSON(j.Notify),
		j.TimeoutSec, j.MaxAttempts, j.Enabled, j.OneShot, j.NextRunAt, j.CreatedAt)
	return err
}

const jobCols = `id,name,endpoint_id,rule_id,schedule,exec_argv,payload_json,notify_json,timeout_sec,max_attempts,enabled,oneshot,next_run_at,created_at`

// scanJob 统一解析任务行（含 notify_json）。Row 与 Rows 都满足 Scan 接口。
func scanJob(sc interface{ Scan(...any) error }) (Job, error) {
	var j Job
	var epID, ruleID sql.NullInt64
	var notifyJSON string
	err := sc.Scan(&j.ID, &j.Name, &epID, &ruleID, &j.Schedule, &j.ExecArgv, &j.Payload, &notifyJSON,
		&j.TimeoutSec, &j.MaxAttempts, &j.Enabled, &j.OneShot, &j.NextRunAt, &j.CreatedAt)
	if err != nil {
		return j, err
	}
	if epID.Valid {
		j.EndpointID = &epID.Int64
	}
	if ruleID.Valid {
		j.RuleID = &ruleID.Int64
	}
	j.Notify = ParseNotifyJSON(notifyJSON)
	return j, nil
}

// NotifyJSON 把任务的通知绑定序列化进库。
func NotifyJSON(bindings []NotifyBinding) string {
	if len(bindings) == 0 {
		return "[]"
	}
	b, err := json.Marshal(bindings)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// ParseNotifyJSON 容错解析历史数据；坏数据按「未配置」处理。
func ParseNotifyJSON(s string) []NotifyBinding {
	s = strings.TrimSpace(s)
	if s == "" || s == "null" {
		return nil
	}
	var out []NotifyBinding
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}

func (s *Store) ListJobs() ([]Job, error) {
	rows, err := s.db.Query(`SELECT ` + jobCols + ` FROM jobs ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Job{}
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (s *Store) GetJob(id string) (*Job, error) {
	j, err := scanJob(s.db.QueryRow(`SELECT `+jobCols+` FROM jobs WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &j, nil
}

func (s *Store) UpdateJob(j *Job) error {
	_, err := s.db.Exec(`UPDATE jobs SET name=?,schedule=?,exec_argv=?,payload_json=?,notify_json=?,timeout_sec=?,max_attempts=?,enabled=?,oneshot=?,next_run_at=? WHERE id=?`,
		j.Name, j.Schedule, j.ExecArgv, j.Payload, NotifyJSON(j.Notify), j.TimeoutSec, j.MaxAttempts, j.Enabled, j.OneShot, j.NextRunAt, j.ID)
	return err
}

func (s *Store) SetJobEnabled(id string, enabled bool, nextRun int64) error {
	_, err := s.db.Exec(`UPDATE jobs SET enabled=?, next_run_at=? WHERE id=?`, enabled, nextRun, id)
	return err
}

func (s *Store) DeleteJob(id string) error {
	_, err := s.db.Exec(`DELETE FROM jobs WHERE id=?`, id)
	return err
}

func (s *Store) DueJobs(t int64) ([]Job, error) {
	rows, err := s.db.Query(`SELECT `+jobCols+` FROM jobs WHERE enabled=1 AND next_run_at<=? ORDER BY next_run_at LIMIT 50`, t)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Job{}
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// ---------- runs ----------

func (s *Store) EnqueueRun(runID, jobID string, maxAttempts int64) error {
	_, err := s.db.Exec(`INSERT INTO runs(id,job_id,status,attempt,max_attempts,created_at,next_attempt_at) VALUES(?,?,'queued',1,?,?,?)`,
		runID, jobID, maxAttempts, now(), now())
	return err
}

func (s *Store) DueRuns(t int64) ([]Run, error) {
	rows, err := s.db.Query(`SELECT id,job_id,status,attempt,max_attempts,output,truncated,error,next_attempt_at,created_at,started_at,finished_at
		FROM runs WHERE status='queued' AND (next_attempt_at IS NULL OR next_attempt_at<=?) ORDER BY created_at LIMIT 20`, t)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		var r Run
		if err := rows.Scan(&r.ID, &r.JobID, &r.Status, &r.Attempt, &r.MaxAttempts, &r.Output, &r.Truncated, &r.Error,
			&r.NextAttemptAt, &r.CreatedAt, &r.StartedAt, &r.FinishedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) ClaimRun(id string) (bool, error) {
	res, err := s.db.Exec(`UPDATE runs SET status='running', started_at=? WHERE id=? AND status='queued'`, now(), id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

func (s *Store) FinishRun(id, status string, exitCode *int64, output string, truncated bool, errMsg string) error {
	_, err := s.db.Exec(`UPDATE runs SET status=?, exit_code=?, output=?, truncated=?, error=?, finished_at=? WHERE id=?`,
		status, exitCode, output, truncated, errMsg, now(), id)
	return err
}

// RetryRun: 失败后重新排队
func (s *Store) RetryRun(id string, nextAttempt, nextAt int64, lastErr, output string) error {
	_, err := s.db.Exec(`UPDATE runs SET status='queued', attempt=?, next_attempt_at=?, error=?, output=?, finished_at=NULL WHERE id=?`,
		nextAttempt, nextAt, lastErr, output, id)
	return err
}

// RequeueRun: 死信重投（人工触发）。
// 只允许终态重投：对已排队/正在跑的 run 重投会把它打回 queued，
// 于是同一个 job 有两行同时被 worker 领走，输出互相覆盖。
func (s *Store) RequeueRun(id string) error {
	res, err := s.db.Exec(`UPDATE runs SET status='queued', attempt=1, next_attempt_at=?, error='', output='', finished_at=NULL
		WHERE id=? AND status NOT IN ('queued','running')`, now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrRunNotRequeueable
	}
	return nil
}

// ErrRunNotRequeueable 表示这条 run 正在排队或正在执行，不能重投。
var ErrRunNotRequeueable = errors.New("这条执行正在排队或正在跑，等它结束再重投")

func (s *Store) GetRun(id string) (*Run, error) {
	var r Run
	err := s.db.QueryRow(`SELECT r.id,r.job_id,j.name,r.status,r.attempt,r.max_attempts,r.exit_code,r.output,r.truncated,r.error,r.next_attempt_at,r.created_at,r.started_at,r.finished_at
		FROM runs r LEFT JOIN jobs j ON j.id=r.job_id WHERE r.id=?`, id).
		Scan(&r.ID, &r.JobID, &r.JobName, &r.Status, &r.Attempt, &r.MaxAttempts, &r.ExitCode, &r.Output, &r.Truncated, &r.Error,
			&r.NextAttemptAt, &r.CreatedAt, &r.StartedAt, &r.FinishedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &r, err
}

func (s *Store) ListRuns(jobID string, status string, limit int) ([]Run, error) {
	q := `SELECT r.id,r.job_id,j.name,r.status,r.attempt,r.max_attempts,r.exit_code,substr(r.output,1,400),r.truncated,r.error,r.next_attempt_at,r.created_at,r.started_at,r.finished_at
		FROM runs r LEFT JOIN jobs j ON j.id=r.job_id`
	where := []string{}
	args := []any{}
	if jobID != "" {
		where = append(where, "r.job_id=?")
		args = append(args, jobID)
	}
	if status != "" {
		where = append(where, "r.status=?")
		args = append(args, status)
	}
	if len(where) > 0 {
		q += " WHERE " + joinAnd(where)
	}
	q += " ORDER BY r.created_at DESC LIMIT ?"
	args = append(args, limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		var r Run
		if err := rows.Scan(&r.ID, &r.JobID, &r.JobName, &r.Status, &r.Attempt, &r.MaxAttempts, &r.ExitCode, &r.Output, &r.Truncated,
			&r.Error, &r.NextAttemptAt, &r.CreatedAt, &r.StartedAt, &r.FinishedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---------- 统计 ----------

type Stats struct {
	Requests24h  int64 `json:"requests_24h"`
	RequestsHour int64 `json:"requests_1h"`
	Runs24h      int64 `json:"runs_24h"`
	OK24h        int64 `json:"ok_24h"`
	Fail24h      int64 `json:"fail_24h"`
	Dead         int64 `json:"dead"`
	Queued       int64 `json:"queued"`
	JobsEnabled  int64 `json:"jobs_enabled"`
	Endpoints    int64 `json:"endpoints"`
	Hourly       []int `json:"hourly"`
}

func (s *Store) Stats() (*Stats, error) {
	st := &Stats{}
	t := now()
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM requests WHERE received_at>?`, t-86400).Scan(&st.Requests24h)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM requests WHERE received_at>?`, t-3600).Scan(&st.RequestsHour)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM runs WHERE created_at>?`, t-86400).Scan(&st.Runs24h)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM runs WHERE created_at>? AND status='succeeded'`, t-86400).Scan(&st.OK24h)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM runs WHERE created_at>? AND status IN ('failed','timeout','dead')`, t-86400).Scan(&st.Fail24h)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM runs WHERE status='dead'`).Scan(&st.Dead)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM runs WHERE status IN ('queued','running')`).Scan(&st.Queued)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM jobs WHERE enabled=1`).Scan(&st.JobsEnabled)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM endpoints WHERE enabled=1`).Scan(&st.Endpoints)

	st.Hourly = make([]int, 24)
	rows, err := s.db.Query(`SELECT CAST((?-received_at)/3600 AS INTEGER) AS h, COUNT(*) FROM requests WHERE received_at>? GROUP BY h`, t, t-86400)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var h, c int
			if err := rows.Scan(&h, &c); err == nil && h >= 0 && h < 24 {
				st.Hourly[23-h] = c
			}
		}
	}
	return st, nil
}

// Prune 保留期清理：requests 按端点的 retain_days，runs 按固定保留天数。
// runs 之前从不清理，每次执行的输出（≤256KB）会永久留在库里，一年能涨到几十 MB
// 并拖慢统计查询；这里按 created_at 清终态（非 queued/running）的行。
// runsRetainDays 是 runs 表的固定保留天数：终态执行记录（含输出）只留 30 天，
// 再早的清掉。量级按「每天几十次 × ≤256KB」算，30 天最多几十 MB，可控。
const runsRetainDays = 30

func (s *Store) Prune() (int64, error) {
	res, err := s.db.Exec(`DELETE FROM requests WHERE received_at < (SELECT strftime('%s','now') - retain_days*86400 FROM endpoints WHERE endpoints.id=requests.endpoint_id)`)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()

	res, err = s.db.Exec(`DELETE FROM runs WHERE status NOT IN ('queued','running') AND created_at < ?`, now()-runsRetainDays*86400)
	if err != nil {
		return n, err
	}
	m, _ := res.RowsAffected()
	return n + m, nil
}

func JSONMust(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// JobByRule 返回某条规则对应的常驻任务（recurring 模式下同一规则只保留一条），无则 nil。
func (s *Store) JobByRule(ruleID int64) *Job {
	j, err := scanJob(s.db.QueryRow(`SELECT `+jobCols+` FROM jobs WHERE rule_id=? AND oneshot=0 ORDER BY id LIMIT 1`, ruleID))
	if err != nil {
		return nil
	}
	return &j
}

// ---------- tokens（多令牌：不同访问对象各自一个） ----------

// Token 一个 API 访问令牌。明文只在创建时返回一次，库里只存 sha256。
type Token struct {
	ID         int64    `json:"id"`
	Name       string   `json:"name"`   // 访问对象名，如「CI 机器人」「张三-只读」
	Prefix     string   `json:"prefix"` // 明文前 8 位，用于列表里辨认
	Role       string   `json:"role"`   // admin | operator | viewer（预设，展示用）
	Scopes     []string `json:"scopes"` // 允许的端点 slug；空 = 全部
	Perms      []string `json:"perms"`  // 细粒度权限；空 = 按 role 预设
	Enabled    bool     `json:"enabled"`
	Note       string   `json:"note"`
	LastUsedAt int64    `json:"last_used_at"`
	CreatedAt  int64    `json:"created_at"`
}

const tokenCols = `id,name,prefix,role,scopes,perms,enabled,note,last_used_at,created_at`

func scanToken(sc interface {
	Scan(...any) error
}) (*Token, error) {
	var t Token
	var scopes, perms string
	var enabled int
	if err := sc.Scan(&t.ID, &t.Name, &t.Prefix, &t.Role, &scopes, &perms, &enabled, &t.Note, &t.LastUsedAt, &t.CreatedAt); err != nil {
		return nil, err
	}
	t.Enabled = enabled == 1
	if scopes != "" {
		t.Scopes = strings.Split(scopes, ",")
	}
	if perms != "" {
		t.Perms = strings.Split(perms, ",")
	}
	return &t, nil
}

func (s *Store) ListTokens() ([]Token, error) {
	rows, err := s.db.Query(`SELECT ` + tokenCols + ` FROM tokens ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Token{}
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func (s *Store) GetToken(id int64) (*Token, error) {
	row := s.db.QueryRow(`SELECT `+tokenCols+` FROM tokens WHERE id=?`, id)
	t, err := scanToken(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return t, err
}

func (s *Store) CreateToken(t *Token, hash string) error {
	res, err := s.db.Exec(
		`INSERT INTO tokens(name,prefix,hash,role,scopes,perms,enabled,note,last_used_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		t.Name, t.Prefix, hash, t.Role, strings.Join(t.Scopes, ","), strings.Join(t.Perms, ","),
		boolInt(t.Enabled), t.Note, 0, time.Now().Unix())
	if err != nil {
		return err
	}
	t.ID, _ = res.LastInsertId()
	return nil
}

func (s *Store) UpdateToken(t *Token) error {
	_, err := s.db.Exec(
		`UPDATE tokens SET name=?,role=?,scopes=?,perms=?,enabled=?,note=? WHERE id=?`,
		t.Name, t.Role, strings.Join(t.Scopes, ","), strings.Join(t.Perms, ","), boolInt(t.Enabled), t.Note, t.ID)
	return err
}

func (s *Store) DeleteToken(id int64) error {
	_, err := s.db.Exec(`DELETE FROM tokens WHERE id=?`, id)
	return err
}

// TouchToken 记录最近使用时间；节流，60 秒内只写一次。
func (s *Store) TouchToken(id int64) {
	now := time.Now().Unix()
	_, _ = s.db.Exec(`UPDATE tokens SET last_used_at=? WHERE id=? AND last_used_at < ?`, now, id, now-60)
}

// TokenByHash 按明文 sha256 查令牌，只返回启用中的。
func (s *Store) TokenByHash(hash string) (*Token, error) {
	row := s.db.QueryRow(`SELECT `+tokenCols+` FROM tokens WHERE hash=? AND enabled=1`, hash)
	t, err := scanToken(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return t, err
}

// CountTokens 返回启用中的令牌数量。
func (s *Store) CountTokens() (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COUNT(*) FROM tokens WHERE enabled=1`).Scan(&n)
	return n, err
}

// ---------- channels（推送渠道） ----------

const channelCols = `id,name,type,config_json,enabled,last_sent_at,last_error,created_at`

func scanChannel(sc interface{ Scan(...any) error }) (Channel, error) {
	var c Channel
	var cfg string
	var enabled int
	err := sc.Scan(&c.ID, &c.Name, &c.Type, &cfg, &enabled, &c.LastSentAt, &c.LastError, &c.CreatedAt)
	if err != nil {
		return c, err
	}
	c.Enabled = enabled == 1
	c.Config = map[string]string{}
	if strings.TrimSpace(cfg) != "" {
		_ = json.Unmarshal([]byte(cfg), &c.Config)
	}
	return c, nil
}

func (s *Store) ListChannels() ([]Channel, error) {
	rows, err := s.db.Query(`SELECT ` + channelCols + ` FROM channels ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Channel{}
	for rows.Next() {
		c, err := scanChannel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) GetChannel(id int64) (*Channel, error) {
	c, err := scanChannel(s.db.QueryRow(`SELECT `+channelCols+` FROM channels WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) CreateChannel(c *Channel) error {
	c.CreatedAt = now()
	cfg, err := json.Marshal(c.Config)
	if err != nil {
		return err
	}
	res, err := s.db.Exec(`INSERT INTO channels(name,type,config_json,enabled,last_sent_at,last_error,created_at) VALUES(?,?,?,?,0,'',?)`,
		c.Name, c.Type, string(cfg), boolInt(c.Enabled), c.CreatedAt)
	if err != nil {
		return err
	}
	c.ID, _ = res.LastInsertId()
	return nil
}

func (s *Store) UpdateChannel(c *Channel) error {
	cfg, err := json.Marshal(c.Config)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE channels SET name=?,type=?,config_json=?,enabled=? WHERE id=?`,
		c.Name, c.Type, string(cfg), boolInt(c.Enabled), c.ID)
	return err
}

func (s *Store) DeleteChannel(id int64) error {
	_, err := s.db.Exec(`DELETE FROM channels WHERE id=?`, id)
	return err
}

// RecordChannelResult 记录最近一次投递结果，用于界面排障。
func (s *Store) RecordChannelResult(id int64, errMsg string) {
	_, _ = s.db.Exec(`UPDATE channels SET last_sent_at=?, last_error=? WHERE id=?`, now(), errMsg, id)
}
