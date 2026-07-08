package ui

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"sort"
	"time"

	"beself/internal/app"
	"beself/internal/store"
	"beself/internal/tracker"
	"beself/internal/win"
)

type dayResponse struct {
	Date      string               `json:"date"`
	Totals    store.Totals         `json:"totals"`
	Sessions  []store.Session      `json:"sessions"`
	Programs  []store.ProgramUsage `json:"programs"`
	Tracking  bool                 `json:"tracking"`
	LastError string               `json:"lastError,omitempty"`
}

type weekDayResponse struct {
	Date          string `json:"date"`
	ActiveSeconds int64  `json:"activeSeconds"`
	IdleSeconds   int64  `json:"idleSeconds"`
	LockedSeconds int64  `json:"lockedSeconds"`
}

type weekResponse struct {
	StartDate            string            `json:"startDate"`
	EndDate              string            `json:"endDate"`
	Days                 []weekDayResponse `json:"days"`
	AverageActiveSeconds int64             `json:"averageActiveSeconds"`
	TotalActiveSeconds   int64             `json:"totalActiveSeconds"`
}

func Register(mux *http.ServeMux, files embed.FS, db *store.DB, tracker *tracker.Tracker) {
	static, err := fs.Sub(files, "web")
	if err != nil {
		static = files
	}
	mux.Handle("/", http.FileServer(http.FS(static)))
	mux.HandleFunc("/api/day", func(w http.ResponseWriter, r *http.Request) {
		day := parseDate(r.URL.Query().Get("date"))
		totals, err := db.TotalsForDate(day)
		if err != nil {
			writeError(w, err)
			return
		}
		sessions, err := db.SessionsForDate(day)
		if err != nil {
			writeError(w, err)
			return
		}
		programs, err := db.ProgramUsageForDate(day)
		if err != nil {
			writeError(w, err)
			return
		}
		sort.Slice(programs, func(i, j int) bool {
			return programs[i].TotalSeconds > programs[j].TotalSeconds
		})
		writeJSON(w, dayResponse{
			Date:      day.Format("2006-01-02"),
			Totals:    totals,
			Sessions:  sessions,
			Programs:  programs,
			Tracking:  tracker.Running(),
			LastError: tracker.LastError(),
		})
	})
	mux.HandleFunc("/api/week", func(w http.ResponseWriter, r *http.Request) {
		end := parseDate(r.URL.Query().Get("date"))
		start := dayStart(end).AddDate(0, 0, -6)
		var days []weekDayResponse
		var total int64
		for i := 0; i < 7; i++ {
			day := start.AddDate(0, 0, i)
			totals, err := db.TotalsForDate(day)
			if err != nil {
				writeError(w, err)
				return
			}
			total += totals.ActiveSeconds
			days = append(days, weekDayResponse{
				Date:          day.Format("2006-01-02"),
				ActiveSeconds: totals.ActiveSeconds,
				IdleSeconds:   totals.IdleSeconds + totals.UnknownSeconds,
				LockedSeconds: totals.LockedSeconds,
			})
		}
		writeJSON(w, weekResponse{
			StartDate:            start.Format("2006-01-02"),
			EndDate:              dayStart(end).Format("2006-01-02"),
			Days:                 days,
			AverageActiveSeconds: total / 7,
			TotalActiveSeconds:   total,
		})
	})
	mux.HandleFunc("/api/tracking", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, map[string]any{"enabled": tracker.Running()})
		case http.MethodPost:
			var body struct {
				Enabled bool `json:"enabled"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.Enabled {
				tracker.Start()
			} else {
				tracker.Stop()
			}
			writeJSON(w, map[string]any{"enabled": tracker.Running()})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/startup", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			enabled, err := win.StartupEnabled(app.Name)
			if err != nil {
				enabled, _ = db.BoolSetting(store.SettingStartWithWin, false)
			}
			writeJSON(w, map[string]any{"enabled": enabled})
		case http.MethodPost:
			var body struct {
				Enabled bool `json:"enabled"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if err := win.SetStartup(app.Name, body.Enabled); err != nil {
				writeError(w, err)
				return
			}
			_ = db.SetBoolSetting(store.SettingStartWithWin, body.Enabled)
			writeJSON(w, map[string]any{"enabled": body.Enabled})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
}

func parseDate(value string) time.Time {
	if value == "" {
		return time.Now()
	}
	t, err := time.ParseInLocation("2006-01-02", value, time.Local)
	if err != nil {
		return time.Now()
	}
	return t
}

func dayStart(t time.Time) time.Time {
	y, m, d := t.Local().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Local().Location())
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
