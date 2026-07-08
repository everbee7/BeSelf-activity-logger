package tracker

import (
	"sync"
	"time"

	"beself/internal/store"
	"beself/internal/win"
)

type Config struct {
	PollInterval  time.Duration
	IdleThreshold time.Duration
}

type Tracker struct {
	db     *store.DB
	probe  *win.Probe
	config Config

	mu        sync.RWMutex
	running   bool
	stop      chan struct{}
	current   currentSession
	lastError string
}

type currentSession struct {
	id     int64
	status string
	win.ActiveWindow
}

func New(db *store.DB, probe *win.Probe, config Config) *Tracker {
	if config.PollInterval <= 0 {
		config.PollInterval = time.Second
	}
	if config.IdleThreshold <= 0 {
		config.IdleThreshold = 5 * time.Minute
	}
	return &Tracker{db: db, probe: probe, config: config}
}

func (t *Tracker) Start() {
	t.mu.Lock()
	if t.running {
		t.mu.Unlock()
		return
	}
	t.running = true
	t.stop = make(chan struct{})
	stop := t.stop
	t.mu.Unlock()

	_ = t.db.SetBoolSetting(store.SettingTrackingEnabled, true)
	go t.loop(stop)
}

func (t *Tracker) Stop() {
	t.mu.Lock()
	if !t.running {
		t.mu.Unlock()
		return
	}
	close(t.stop)
	t.running = false
	t.mu.Unlock()

	t.closeCurrent(time.Now())
	_ = t.db.SetBoolSetting(store.SettingTrackingEnabled, false)
}

func (t *Tracker) Shutdown() {
	t.mu.Lock()
	if t.running {
		close(t.stop)
		t.running = false
	}
	t.mu.Unlock()
	t.closeCurrent(time.Now())
}

func (t *Tracker) Running() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.running
}

func (t *Tracker) LastError() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.lastError
}

func (t *Tracker) loop(stop <-chan struct{}) {
	ticker := time.NewTicker(t.config.PollInterval)
	defer ticker.Stop()
	t.sample()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			t.sample()
		}
	}
}

func (t *Tracker) sample() {
	next := currentSession{}
	if t.probe.IsLocked() {
		next.status = store.StatusLocked
	} else if t.probe.IdleFor() >= t.config.IdleThreshold {
		next.status = store.StatusIdle
	} else if active, err := t.probe.ActiveWindow(); err == nil && active.ProcessName != "" {
		next.status = store.StatusActive
		next.ActiveWindow = active
		t.setError("")
	} else {
		next.status = store.StatusUnknown
		t.setError("active window unavailable")
	}
	t.rotate(next)
}

func (t *Tracker) rotate(next currentSession) {
	now := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()

	if sameSession(t.current, next) {
		return
	}
	if t.current.id != 0 {
		if err := t.db.EndSession(t.current.id, now); err != nil {
			t.lastError = err.Error()
		}
	}
	id, err := t.db.StartSession(store.Session{
		StartTime:      now,
		Status:         next.status,
		ProcessName:    next.ProcessName,
		WindowTitle:    next.WindowTitle,
		ExecutablePath: next.ExecutablePath,
	})
	if err != nil {
		t.lastError = err.Error()
		t.current = currentSession{}
		return
	}
	next.id = id
	t.current = next
}

func (t *Tracker) closeCurrent(now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.current.id == 0 {
		return
	}
	if err := t.db.EndSession(t.current.id, now); err != nil {
		t.lastError = err.Error()
	}
	t.current = currentSession{}
}

func sameSession(a, b currentSession) bool {
	return a.status == b.status &&
		a.ProcessName == b.ProcessName &&
		a.WindowTitle == b.WindowTitle &&
		a.ExecutablePath == b.ExecutablePath
}

func (t *Tracker) setError(value string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lastError = value
}
