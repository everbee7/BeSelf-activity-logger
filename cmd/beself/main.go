package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"beself/internal/app"
	"beself/internal/nativeui"
	"beself/internal/store"
	"beself/internal/tracker"
	"beself/internal/tray"
	"beself/internal/ui"
	"beself/internal/win"
)

//go:embed all:web
var webFS embed.FS

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	nativeDashboard := flag.Bool("native-dashboard", false, "open native dashboard window")
	dashboardURL := flag.String("dashboard-url", "", "dashboard API URL")
	flag.Parse()
	if *nativeDashboard {
		if *dashboardURL == "" {
			log.Fatal("missing --dashboard-url")
		}
		if err := nativeui.Run(*dashboardURL); err != nil {
			log.Fatal(err)
		}
		return
	}

	dataDir, err := app.DataDir()
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		log.Fatal(err)
	}

	db, err := store.Open(filepath.Join(dataDir, "beself.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if err := db.CloseOpenSessions(time.Now()); err != nil {
		log.Printf("closing previous sessions: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	engine := tracker.New(db, win.NewProbe(), tracker.Config{PollInterval: time.Second, IdleThreshold: 5 * time.Minute})
	if enabled, _ := db.BoolSetting(store.SettingTrackingEnabled, true); enabled {
		engine.Start()
	}

	server, url, err := startDashboard(db, engine)
	if err != nil {
		log.Fatal(err)
	}
	defer server.Shutdown(context.Background())

	log.Printf("dashboard: %s", url)
	controller := tray.New(db, engine, url)
	go controller.Run()
	tray.OpenNativeDashboard(url)

	<-ctx.Done()
	controller.Quit()
	engine.Shutdown()
	if err := db.CloseOpenSessions(time.Now()); err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("closing sessions: %v", err)
	}
}

func startDashboard(db *store.DB, engine *tracker.Tracker) (*http.Server, string, error) {
	mux := http.NewServeMux()
	ui.Register(mux, webFS, db, engine)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, "", err
	}
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("dashboard server: %v", err)
		}
	}()
	return server, fmt.Sprintf("http://%s", listener.Addr().String()), nil
}
