package tray

import (
	"encoding/binary"
	"log"
	"os"
	"os/exec"

	"beself/internal/app"
	"beself/internal/nativeui"
	"beself/internal/store"
	"beself/internal/tracker"
	"beself/internal/win"

	"github.com/getlantern/systray"
)

type Controller struct {
	db      *store.DB
	tracker *tracker.Tracker
	url     string
}

func New(db *store.DB, tracker *tracker.Tracker, url string) *Controller {
	return &Controller{db: db, tracker: tracker, url: url}
}

func (c *Controller) Run() {
	systray.Run(c.onReady, func() {})
}

func (c *Controller) Quit() {
	systray.Quit()
}

func (c *Controller) onReady() {
	systray.SetIcon(iconBytes(c.tracker.Running()))
	systray.SetTitle(app.Name)
	systray.SetTooltip("BeSelf time tracker")

	open := systray.AddMenuItem("Open app", "Open BeSelf Windows dashboard")
	openWeb := systray.AddMenuItem("Open web dashboard", "Open BeSelf dashboard in browser")
	toggleTracking := systray.AddMenuItem("", "Start or stop tracking")
	startup := systray.AddMenuItem("", "Toggle start with Windows")
	systray.AddSeparator()
	exit := systray.AddMenuItem("Exit", "Exit BeSelf")

	refreshTracking := func() {
		if c.tracker.Running() {
			toggleTracking.SetTitle("Stop tracking")
			systray.SetIcon(iconBytes(true))
			systray.SetTooltip("BeSelf time tracker: tracking")
		} else {
			toggleTracking.SetTitle("Start tracking")
			systray.SetIcon(iconBytes(false))
			systray.SetTooltip("BeSelf time tracker: stopped")
		}
	}
	refreshStartup := func() {
		enabled, err := win.StartupEnabled(app.Name)
		if err != nil {
			enabled, _ = c.db.BoolSetting(store.SettingStartWithWin, false)
		}
		if enabled {
			startup.SetTitle("Disable start with Windows")
		} else {
			startup.SetTitle("Enable start with Windows")
		}
	}
	refreshTracking()
	refreshStartup()

	for {
		select {
		case <-open.ClickedCh:
			OpenNativeDashboard(c.url)
		case <-openWeb.ClickedCh:
			openBrowser(c.url)
		case <-toggleTracking.ClickedCh:
			if c.tracker.Running() {
				c.tracker.Stop()
			} else {
				c.tracker.Start()
			}
			refreshTracking()
			nativeui.NotifyDashboardRefresh()
		case <-startup.ClickedCh:
			enabled, _ := win.StartupEnabled(app.Name)
			next := !enabled
			if err := win.SetStartup(app.Name, next); err != nil {
				log.Printf("startup setting: %v", err)
			} else {
				_ = c.db.SetBoolSetting(store.SettingStartWithWin, next)
			}
			refreshStartup()
		case <-exit.ClickedCh:
			c.tracker.Shutdown()
			systray.Quit()
			return
		}
	}
}

func OpenNativeDashboard(url string) {
	exe, err := os.Executable()
	if err != nil {
		log.Printf("native dashboard executable: %v", err)
		return
	}
	if err := exec.Command(exe, "--native-dashboard", "--dashboard-url", url).Start(); err != nil {
		log.Printf("open native dashboard: %v", err)
	}
}

func openBrowser(url string) {
	if err := exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start(); err != nil {
		log.Printf("open dashboard: %v", err)
	}
}

func iconBytes(enabled bool) []byte {
	const size = 16
	const pixels = size * size
	xorBytes := pixels * 4
	andBytes := size * size / 8
	imageBytes := 40 + xorBytes + andBytes
	out := make([]byte, 6+16+imageBytes)

	binary.LittleEndian.PutUint16(out[0:], 0)
	binary.LittleEndian.PutUint16(out[2:], 1)
	binary.LittleEndian.PutUint16(out[4:], 1)
	out[6] = size
	out[7] = size
	out[8] = 0
	out[9] = 0
	binary.LittleEndian.PutUint16(out[10:], 1)
	binary.LittleEndian.PutUint16(out[12:], 32)
	binary.LittleEndian.PutUint32(out[14:], uint32(imageBytes))
	binary.LittleEndian.PutUint32(out[18:], 22)

	img := out[22:]
	binary.LittleEndian.PutUint32(img[0:], 40)
	binary.LittleEndian.PutUint32(img[4:], size)
	binary.LittleEndian.PutUint32(img[8:], size*2)
	binary.LittleEndian.PutUint16(img[12:], 1)
	binary.LittleEndian.PutUint16(img[14:], 32)
	p := img[40:]
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			i := ((size-1-y)*size + x) * 4
			dx := x - 8
			dy := y - 8
			if dx*dx+dy*dy > 54 {
				p[i+3] = 0
				continue
			}
			if !enabled {
				v := byte(136)
				if y >= 6 {
					v = 96
				}
				p[i+0] = v
				p[i+1] = v
				p[i+2] = v
			} else if y < 6 {
				p[i+0] = 53
				p[i+1] = 213
				p[i+2] = 252
			} else {
				p[i+0] = 0
				p[i+1] = 148
				p[i+2] = 201
			}
			if x >= 5 && x <= 10 && y >= 4 && y <= 11 {
				p[i+0] = 17
				p[i+1] = 14
				p[i+2] = 11
			}
			if enabled && x >= 6 && x <= 9 && y >= 5 && y <= 6 {
				p[i+0] = 80
				p[i+1] = 229
				p[i+2] = 255
			}
			p[i+3] = 255
		}
	}
	return out
}
