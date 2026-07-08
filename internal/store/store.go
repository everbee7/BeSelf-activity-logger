package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	_ "modernc.org/sqlite"
)

const (
	StatusActive      = "active"
	StatusIdle        = "idle"
	StatusLocked      = "locked"
	StatusTrackingOff = "tracking_off"
	StatusUnknown     = "unknown"

	SettingTrackingEnabled = "tracking_enabled"
	SettingStartWithWin    = "start_with_windows"
	SettingIdleThreshold   = "idle_threshold_seconds"
)

type DB struct {
	db *sql.DB
}

type Session struct {
	ID              int64      `json:"id"`
	StartTime       time.Time  `json:"startTime"`
	EndTime         *time.Time `json:"endTime,omitempty"`
	DurationSeconds int64      `json:"durationSeconds"`
	Status          string     `json:"status"`
	ProcessName     string     `json:"processName"`
	WindowTitle     string     `json:"windowTitle"`
	ExecutablePath  string     `json:"executablePath"`
}

type Totals struct {
	ActiveSeconds  int64 `json:"activeSeconds"`
	IdleSeconds    int64 `json:"idleSeconds"`
	LockedSeconds  int64 `json:"lockedSeconds"`
	OffSeconds     int64 `json:"offSeconds"`
	UnknownSeconds int64 `json:"unknownSeconds"`
}

type ProgramUsage struct {
	ProcessName       string     `json:"processName"`
	ExecutablePath    string     `json:"executablePath"`
	TotalSeconds      int64      `json:"totalSeconds"`
	FirstUsed         *time.Time `json:"firstUsed,omitempty"`
	LastUsed          *time.Time `json:"lastUsed,omitempty"`
	PercentActiveTime float64    `json:"percentActiveTime"`
}

func Open(path string) (*DB, error) {
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	raw.SetMaxOpenConns(1)
	db := &DB{db: raw}
	if err := db.migrate(); err != nil {
		raw.Close()
		return nil, err
	}
	return db, nil
}

func (d *DB) Close() error {
	return d.db.Close()
}

func (d *DB) migrate() error {
	stmts := []string{
		`PRAGMA journal_mode=WAL;`,
		`CREATE TABLE IF NOT EXISTS settings (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			key TEXT NOT NULL UNIQUE,
			value TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS activity_sessions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			start_time TEXT NOT NULL,
			end_time TEXT,
			duration_seconds INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL,
			process_name TEXT NOT NULL DEFAULT '',
			window_title TEXT NOT NULL DEFAULT '',
			executable_path TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE INDEX IF NOT EXISTS idx_activity_sessions_start ON activity_sessions(start_time);`,
		`CREATE INDEX IF NOT EXISTS idx_activity_sessions_end ON activity_sessions(end_time);`,
		`CREATE TABLE IF NOT EXISTS daily_summary (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			date TEXT NOT NULL UNIQUE,
			active_seconds INTEGER NOT NULL DEFAULT 0,
			idle_seconds INTEGER NOT NULL DEFAULT 0,
			locked_seconds INTEGER NOT NULL DEFAULT 0
		);`,
	}
	for _, stmt := range stmts {
		if _, err := d.db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) StartSession(s Session) (int64, error) {
	res, err := d.db.Exec(`INSERT INTO activity_sessions
		(start_time, status, process_name, window_title, executable_path)
		VALUES (?, ?, ?, ?, ?)`,
		formatTime(s.StartTime), s.Status, s.ProcessName, s.WindowTitle, s.ExecutablePath)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) EndSession(id int64, end time.Time) error {
	if id == 0 {
		return nil
	}
	_, err := d.db.Exec(`UPDATE activity_sessions
		SET end_time = ?, duration_seconds = MAX(0, CAST((julianday(?) - julianday(start_time)) * 86400 AS INTEGER))
		WHERE id = ? AND end_time IS NULL`, formatTime(end), formatTime(end), id)
	return err
}

func (d *DB) CloseOpenSessions(end time.Time) error {
	_, err := d.db.Exec(`UPDATE activity_sessions
		SET end_time = ?, duration_seconds = MAX(0, CAST((julianday(?) - julianday(start_time)) * 86400 AS INTEGER))
		WHERE end_time IS NULL`, formatTime(end), formatTime(end))
	return err
}

func (d *DB) SessionsForDate(day time.Time) ([]Session, error) {
	start := dayStart(day)
	end := start.AddDate(0, 0, 1)
	rows, err := d.db.Query(`SELECT id, start_time, end_time, duration_seconds, status, process_name, window_title, executable_path
		FROM activity_sessions
		WHERE start_time < ? AND COALESCE(end_time, ?) > ?
		ORDER BY start_time ASC`, formatTime(end), formatTime(time.Now()), formatTime(start))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

func (d *DB) TotalsForDate(day time.Time) (Totals, error) {
	sessions, err := d.SessionsForDate(day)
	if err != nil {
		return Totals{}, err
	}
	var totals Totals
	start := dayStart(day)
	end := start.AddDate(0, 0, 1)
	now := time.Now()
	for _, s := range sessions {
		seconds := clippedSeconds(s, start, end, now)
		switch s.Status {
		case StatusActive:
			totals.ActiveSeconds += seconds
		case StatusIdle:
			totals.IdleSeconds += seconds
		case StatusLocked:
			totals.LockedSeconds += seconds
		case StatusTrackingOff:
			totals.OffSeconds += seconds
		default:
			totals.UnknownSeconds += seconds
		}
	}
	return totals, nil
}

func (d *DB) ProgramUsageForDate(day time.Time) ([]ProgramUsage, error) {
	sessions, err := d.SessionsForDate(day)
	if err != nil {
		return nil, err
	}
	start := dayStart(day)
	end := start.AddDate(0, 0, 1)
	now := time.Now()
	type agg struct {
		ProgramUsage
	}
	byProgram := map[string]*agg{}
	var totalActive int64
	for _, s := range sessions {
		if s.Status != StatusActive {
			continue
		}
		seconds := clippedSeconds(s, start, end, now)
		totalActive += seconds
		key := s.ProcessName + "\x00" + s.ExecutablePath
		item := byProgram[key]
		if item == nil {
			item = &agg{ProgramUsage: ProgramUsage{ProcessName: s.ProcessName, ExecutablePath: s.ExecutablePath}}
			byProgram[key] = item
		}
		item.TotalSeconds += seconds
		st := maxTime(s.StartTime, start)
		en := minTime(sessionEnd(s, now), end)
		if item.FirstUsed == nil || st.Before(*item.FirstUsed) {
			v := st
			item.FirstUsed = &v
		}
		if item.LastUsed == nil || en.After(*item.LastUsed) {
			v := en
			item.LastUsed = &v
		}
	}
	out := make([]ProgramUsage, 0, len(byProgram))
	for _, item := range byProgram {
		if totalActive > 0 {
			item.PercentActiveTime = float64(item.TotalSeconds) * 100 / float64(totalActive)
		}
		out = append(out, item.ProgramUsage)
	}
	return out, nil
}

func (d *DB) BoolSetting(key string, fallback bool) (bool, error) {
	value, err := d.Setting(key)
	if errors.Is(err, sql.ErrNoRows) {
		return fallback, nil
	}
	if err != nil {
		return fallback, err
	}
	return value == "true" || value == "1", nil
}

func (d *DB) SetBoolSetting(key string, value bool) error {
	if value {
		return d.SetSetting(key, "true")
	}
	return d.SetSetting(key, "false")
}

func (d *DB) Setting(key string) (string, error) {
	var value string
	err := d.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	return value, err
}

func (d *DB) SetSetting(key, value string) error {
	_, err := d.db.Exec(`INSERT INTO settings(key, value) VALUES(?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func (d *DB) RebuildDailySummary(day time.Time) error {
	t, err := d.TotalsForDate(day)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(`INSERT INTO daily_summary(date, active_seconds, idle_seconds, locked_seconds)
		VALUES(?, ?, ?, ?)
		ON CONFLICT(date) DO UPDATE SET
			active_seconds = excluded.active_seconds,
			idle_seconds = excluded.idle_seconds,
			locked_seconds = excluded.locked_seconds`,
		dayStart(day).Format("2006-01-02"), t.ActiveSeconds, t.IdleSeconds, t.LockedSeconds)
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanSession(row scanner) (Session, error) {
	var s Session
	var start, end sql.NullString
	if err := row.Scan(&s.ID, &start, &end, &s.DurationSeconds, &s.Status, &s.ProcessName, &s.WindowTitle, &s.ExecutablePath); err != nil {
		return s, err
	}
	if start.Valid {
		s.StartTime, _ = time.Parse(time.RFC3339Nano, start.String)
	}
	if end.Valid {
		t, _ := time.Parse(time.RFC3339Nano, end.String)
		s.EndTime = &t
	}
	return s, nil
}

func clippedSeconds(s Session, start, end, now time.Time) int64 {
	from := maxTime(s.StartTime, start)
	to := minTime(sessionEnd(s, now), end)
	if !to.After(from) {
		return 0
	}
	return int64(to.Sub(from).Seconds())
}

func sessionEnd(s Session, now time.Time) time.Time {
	if s.EndTime != nil {
		return *s.EndTime
	}
	return now
}

func dayStart(t time.Time) time.Time {
	y, m, d := t.Local().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Local().Location())
}

func formatTime(t time.Time) string {
	return t.Local().Format(time.RFC3339Nano)
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func (d *DB) Ping(ctx context.Context) error {
	return d.db.PingContext(ctx)
}
