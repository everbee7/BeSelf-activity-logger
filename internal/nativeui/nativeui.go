package nativeui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"io"
	"net"
	"net/http"
	"os/exec"
	"sort"
	"strings"
	"time"
	"unsafe"

	"beself/internal/store"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"github.com/lxn/win"
)

const commandAddr = "127.0.0.1:49382"

var (
	cBg      = walk.RGB(11, 14, 17)
	cPanel   = walk.RGB(24, 26, 32)
	cPanel2  = walk.RGB(30, 35, 41)
	cPanel3  = walk.RGB(43, 49, 57)
	cLine    = walk.RGB(71, 77, 87)
	cText    = walk.RGB(234, 236, 239)
	cMuted   = walk.RGB(132, 142, 156)
	cOrange  = walk.RGB(240, 185, 11)
	cOrange2 = walk.RGB(252, 213, 53)
	cOrange3 = walk.RGB(201, 148, 0)
	cLocked  = walk.RGB(246, 70, 93)
	cIdle    = walk.RGB(94, 102, 115)
	cUnknown = walk.RGB(71, 77, 87)
	cGrid    = walk.RGB(43, 49, 57)
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

type Dashboard struct {
	*walk.MainWindow
	apiURL string
	canvas *walk.CustomWidget

	day             time.Time
	data            dayResponse
	week            weekResponse
	detail          string
	hoverSession    *store.Session
	selectedSession *store.Session
	selectingRange  bool
	resizing        bool
	resizeStart     walk.Point
	resizeBounds    walk.Rectangle
	rangeStart      *time.Time
	rangeEnd        *time.Time
	programScroll   int
	regions         map[string]walk.Rectangle
	iconCache       map[string]*walk.Icon
	appIcon         *walk.Icon
	fonts           map[string]*walk.Font
}

func Run(apiURL string) error {
	initCommonControls()
	if focusExistingDashboard() {
		return nil
	}

	d := &Dashboard{
		apiURL:    apiURL,
		day:       dayStart(time.Now()),
		regions:   map[string]walk.Rectangle{},
		iconCache: map[string]*walk.Icon{},
		fonts:     map[string]*walk.Font{},
	}
	if err := d.initFonts(); err != nil {
		return err
	}
	defer d.disposeFonts()

	if err := (MainWindow{
		AssignTo:   &d.MainWindow,
		Title:      "BeSelf",
		MinSize:    Size{1040, 760},
		Size:       Size{1220, 840},
		Background: SolidColorBrush{Color: cBg},
		Layout:     VBox{MarginsZero: true},
		Children: []Widget{
			CustomWidget{
				AssignTo:            &d.canvas,
				Background:          SolidColorBrush{Color: cBg},
				InvalidatesOnResize: true,
				PaintMode:           PaintBuffered,
				Paint:               d.paint,
				OnMouseMove:         d.mouseMove,
				OnMouseDown:         d.mouseDown,
				OnMouseUp:           d.mouseUp,
			},
		},
	}).Create(); err != nil {
		return err
	}

	d.setAppIcon()
	d.setBorderless()
	go d.commandServer()
	d.canvas.MouseWheel().Attach(d.mouseWheel)
	d.refresh()
	go d.refreshLoop()
	d.Show()
	d.Run()
	return nil
}

func focusExistingDashboard() bool {
	conn, err := net.DialTimeout("tcp", commandAddr, 150*time.Millisecond)
	if err != nil {
		return false
	}
	defer conn.Close()
	_, _ = io.WriteString(conn, "focus\n")
	return true
}

func NotifyDashboardRefresh() {
	conn, err := net.DialTimeout("tcp", commandAddr, 150*time.Millisecond)
	if err != nil {
		return
	}
	defer conn.Close()
	_, _ = io.WriteString(conn, "refresh\n")
}

func (d *Dashboard) commandServer() {
	ln, err := net.Listen("tcp", commandAddr)
	if err != nil {
		return
	}
	defer ln.Close()
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go func(c net.Conn) {
			defer c.Close()
			buf := make([]byte, 32)
			n, _ := c.Read(buf)
			cmd := strings.TrimSpace(string(buf[:n]))
			d.Synchronize(func() {
				switch cmd {
				case "focus":
					d.focus()
				case "refresh":
					d.refresh()
				}
			})
		}(conn)
	}
}

func (d *Dashboard) focus() {
	win.ShowWindow(d.Handle(), win.SW_RESTORE)
	win.BringWindowToTop(d.Handle())
	win.SetForegroundWindow(d.Handle())
	d.refresh()
}

func initCommonControls() {
	ctrls := win.INITCOMMONCONTROLSEX{
		DwSize: uint32(unsafe.Sizeof(win.INITCOMMONCONTROLSEX{})),
		DwICC:  win.ICC_WIN95_CLASSES | win.ICC_DATE_CLASSES | win.ICC_STANDARD_CLASSES,
	}
	win.InitCommonControlsEx(&ctrls)
}

func (d *Dashboard) initFonts() error {
	specs := map[string]struct {
		size int
		bold bool
	}{
		"title":   {22, true},
		"metric":  {24, true},
		"section": {11, true},
		"body":    {9, false},
		"small":   {8, false},
		"button":  {9, true},
	}
	for key, spec := range specs {
		var flags walk.FontStyle
		if spec.bold {
			flags = walk.FontBold
		}
		font, err := walk.NewFont("Segoe UI", spec.size, flags)
		if err != nil {
			return err
		}
		d.fonts[key] = font
	}
	return nil
}

func (d *Dashboard) disposeFonts() {
	for _, font := range d.fonts {
		font.Dispose()
	}
	for _, icon := range d.iconCache {
		icon.Dispose()
	}
	if d.appIcon != nil {
		d.appIcon.Dispose()
	}
}

func (d *Dashboard) setBorderless() {
	hwnd := d.Handle()
	style := win.GetWindowLong(hwnd, win.GWL_STYLE)
	style &^= win.WS_CAPTION | win.WS_THICKFRAME | win.WS_BORDER | win.WS_DLGFRAME
	style |= int32(-2147483648)
	win.SetWindowLong(hwnd, win.GWL_STYLE, style)
	win.SetWindowPos(hwnd, 0, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_FRAMECHANGED)
}

func (d *Dashboard) setAppIcon() {
	if icon, err := createAppIcon(); err == nil {
		d.appIcon = icon
		_ = d.SetIcon(icon)
	} else if icon, err := walk.NewIconFromResourceId(7); err == nil {
		d.appIcon = icon
		_ = d.SetIcon(icon)
	}
}

func (d *Dashboard) refreshLoop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if d.MainWindow == nil {
			return
		}
		d.Synchronize(d.refresh)
	}
}

func (d *Dashboard) refresh() {
	var data dayResponse
	if err := getJSON(d.apiURL+"/api/day?date="+d.day.Format("2006-01-02"), &data); err != nil {
		d.detail = err.Error()
		d.canvas.Invalidate()
		return
	}
	sort.Slice(data.Programs, func(i, j int) bool {
		return data.Programs[i].TotalSeconds > data.Programs[j].TotalSeconds
	})
	var week weekResponse
	if err := getJSON(d.apiURL+"/api/week?date="+d.day.Format("2006-01-02"), &week); err == nil {
		d.week = week
	}
	d.data = data
	if data.LastError != "" {
		d.detail = data.LastError
	} else {
		d.detail = "Move across the timeline to inspect activity blocks."
	}
	d.canvas.Invalidate()
}

func (d *Dashboard) paint(canvas *walk.Canvas, updateBounds walk.Rectangle) error {
	b := d.canvas.ClientBounds()
	d.regions = map[string]walk.Rectangle{}
	fill(canvas, cBg, b)
	d.drawHeader(canvas, b)
	content := walk.Rectangle{X: 22, Y: 86, Width: b.Width - 44, Height: b.Height - 108}
	d.drawMetrics(canvas, content)
	d.drawWeekStats(canvas, content)
	d.drawTimeline(canvas, content)
	d.drawPrograms(canvas, content)
	d.drawResizeGrip(canvas, b)
	return nil
}

func (d *Dashboard) drawHeader(canvas *walk.Canvas, b walk.Rectangle) {
	header := walk.Rectangle{X: 0, Y: 0, Width: b.Width, Height: 70}
	_ = canvas.GradientFillRectangle(walkColor(cPanel2), walkColor(cBg), walk.Vertical, header)
	_ = canvas.GradientFillRectangle(walkColor(cOrange3), walkColor(cOrange2), walk.Horizontal, walk.Rectangle{X: 22, Y: 68, Width: b.Width - 44, Height: 2})

	d.drawLogo(canvas, 22, 18)
	text(canvas, "BeSelf", d.fonts["title"], cText, walk.Rectangle{X: 66, Y: 14, Width: 160, Height: 30}, walk.TextLeft)
	text(canvas, "Native activity dashboard", d.fonts["body"], cMuted, walk.Rectangle{X: 68, Y: 43, Width: 230, Height: 20}, walk.TextLeft)

	x := b.Width - 700
	d.iconButton(canvas, "prev", "left", walk.Rectangle{X: x, Y: 19, Width: 38, Height: 34})
	d.button(canvas, "date", d.day.Format("Jan 02, 2006"), walk.Rectangle{X: x + 44, Y: 19, Width: 142, Height: 34}, false)
	d.iconButton(canvas, "next", "right", walk.Rectangle{X: x + 192, Y: 19, Width: 38, Height: 34})
	d.button(canvas, "today", "Today", walk.Rectangle{X: x + 240, Y: 19, Width: 76, Height: 34}, false)
	trackingText := "Start tracking"
	if d.data.Tracking {
		trackingText = "Stop tracking"
	}
	d.button(canvas, "tracking", trackingText, walk.Rectangle{X: x + 326, Y: 19, Width: 120, Height: 34}, true)
	d.button(canvas, "web", "Open web", walk.Rectangle{X: x + 456, Y: 19, Width: 92, Height: 34}, false)

	d.iconButton(canvas, "min", "min", walk.Rectangle{X: b.Width - 76, Y: 18, Width: 30, Height: 30})
	d.iconButton(canvas, "close", "close", walk.Rectangle{X: b.Width - 40, Y: 18, Width: 30, Height: 30})
	d.regions["drag"] = walk.Rectangle{X: 0, Y: 0, Width: max(0, x-10), Height: 70}
}

func (d *Dashboard) drawMetrics(canvas *walk.Canvas, content walk.Rectangle) {
	gap := 14
	cardW := (content.Width - gap*3) / 4
	cards := []struct {
		title string
		value string
	}{
		{"Worked", formatDuration(d.data.Totals.ActiveSeconds)},
		{"Week avg", formatDuration(d.week.AverageActiveSeconds)},
		{"Locked", formatDuration(d.data.Totals.LockedSeconds)},
		{"Idle", formatDuration(d.data.Totals.IdleSeconds + d.data.Totals.UnknownSeconds)},
	}
	for i, card := range cards {
		r := walk.Rectangle{X: content.X + i*(cardW+gap), Y: content.Y, Width: cardW, Height: 92}
		panel(canvas, r)
		text(canvas, card.title, d.fonts["body"], cMuted, walk.Rectangle{X: r.X + 18, Y: r.Y + 16, Width: r.Width - 36, Height: 18}, walk.TextLeft)
		text(canvas, card.value, d.fonts["metric"], cOrange2, walk.Rectangle{X: r.X + 18, Y: r.Y + 40, Width: r.Width - 36, Height: 38}, walk.TextLeft)
	}
}

func (d *Dashboard) drawWeekStats(canvas *walk.Canvas, content walk.Rectangle) {
	r := walk.Rectangle{X: content.X, Y: content.Y + 112, Width: content.Width, Height: 134}
	panel(canvas, r)
	text(canvas, "Weekly Statistics", d.fonts["section"], cText, walk.Rectangle{X: r.X + 16, Y: r.Y + 14, Width: 180, Height: 22}, walk.TextLeft)
	text(canvas, "Average working hours this week", d.fonts["body"], cMuted, walk.Rectangle{X: r.X + r.Width - 270, Y: r.Y + 14, Width: 250, Height: 22}, walk.TextRight)
	text(canvas, formatDuration(d.week.AverageActiveSeconds), d.fonts["metric"], cOrange2, walk.Rectangle{X: r.X + r.Width - 180, Y: r.Y + 38, Width: 160, Height: 34}, walk.TextRight)

	graph := walk.Rectangle{X: r.X + 16, Y: r.Y + 48, Width: r.Width - 220, Height: 70}
	fill(canvas, cPanel2, graph)
	border(canvas, graph, cGrid)
	maxSeconds := int64(1)
	for _, day := range d.week.Days {
		if day.ActiveSeconds > maxSeconds {
			maxSeconds = day.ActiveSeconds
		}
	}
	if len(d.week.Days) == 0 {
		text(canvas, "No weekly data yet.", d.fonts["body"], cMuted, graph, walk.TextCenter)
		return
	}
	barGap := 10
	barW := (graph.Width - barGap*(len(d.week.Days)+1)) / len(d.week.Days)
	if barW < 8 {
		barW = 8
	}
	for i, day := range d.week.Days {
		x := graph.X + barGap + i*(barW+barGap)
		h := int(float64(day.ActiveSeconds) / float64(maxSeconds) * float64(graph.Height-22))
		if h < 2 && day.ActiveSeconds > 0 {
			h = 2
		}
		bar := walk.Rectangle{X: x, Y: graph.Y + graph.Height - 18 - h, Width: barW, Height: h}
		_ = canvas.GradientFillRectangle(walkColor(cOrange2), walkColor(cOrange3), walk.Vertical, bar)
		label := ""
		if t, err := time.Parse("2006-01-02", day.Date); err == nil {
			label = t.Format("Mon")
		}
		text(canvas, label, d.fonts["small"], cMuted, walk.Rectangle{X: x - 8, Y: graph.Y + graph.Height - 30, Width: barW + 16, Height: 14}, walk.TextCenter)
		text(canvas, formatDuration(day.ActiveSeconds), d.fonts["small"], cText, walk.Rectangle{X: x - 12, Y: graph.Y + graph.Height - 16, Width: barW + 24, Height: 14}, walk.TextCenter)
	}
}

func (d *Dashboard) drawTimeline(canvas *walk.Canvas, content walk.Rectangle) {
	r := walk.Rectangle{X: content.X, Y: content.Y + 266, Width: content.Width, Height: 186}
	panel(canvas, r)
	text(canvas, "Timeline", d.fonts["section"], cText, walk.Rectangle{X: r.X + 16, Y: r.Y + 14, Width: 120, Height: 22}, walk.TextLeft)
	d.legend(canvas, r.X+r.Width-285, r.Y+16, cOrange, "Active")
	d.legend(canvas, r.X+r.Width-195, r.Y+16, cLocked, "Locked")
	d.legend(canvas, r.X+r.Width-105, r.Y+16, cIdle, "Idle")

	bar := walk.Rectangle{X: r.X + 16, Y: r.Y + 50, Width: r.Width - 32, Height: 92}
	d.regions["timeline"] = bar
	fill(canvas, cPanel3, bar)
	pen, _ := walk.NewCosmeticPen(walk.PenSolid, cGrid)
	defer pen.Dispose()
	for h := 0; h <= 24; h++ {
		x := bar.X + h*bar.Width/24
		_ = canvas.DrawLine(pen, walk.Point{X: x, Y: bar.Y}, walk.Point{X: x, Y: bar.Y + bar.Height})
		if h%3 == 0 && h < 24 {
			text(canvas, fmt.Sprintf("%02d", h), d.fonts["small"], cMuted, walk.Rectangle{X: x + 4, Y: bar.Y + 5, Width: 32, Height: 16}, walk.TextLeft)
		}
	}
	d.drawTimelineBlocks(canvas, bar)
	d.drawRangeSelection(canvas, bar)
	border(canvas, bar, cLine)
	text(canvas, d.detail, d.fonts["body"], cMuted, walk.Rectangle{X: r.X + 16, Y: r.Y + 152, Width: r.Width - 32, Height: 22}, walk.TextLeft)
}

func (d *Dashboard) drawTimelineBlocks(canvas *walk.Canvas, bar walk.Rectangle) {
	if d.data.Date == "" {
		return
	}
	day, _ := time.ParseInLocation("2006-01-02", d.data.Date, time.Local)
	start := day
	end := start.Add(24 * time.Hour)
	now := time.Now()
	for i := range d.data.Sessions {
		s := d.data.Sessions[i]
		from := clampTime(s.StartTime, start, end)
		to := now
		if s.EndTime != nil {
			to = *s.EndTime
		}
		to = clampTime(to, start, end)
		if !to.After(from) {
			continue
		}
		x := bar.X + int(from.Sub(start).Seconds()*float64(bar.Width)/86400)
		w := int(to.Sub(from).Seconds() * float64(bar.Width) / 86400)
		if w < 2 {
			w = 2
		}
		rect := walk.Rectangle{X: x, Y: bar.Y + 26, Width: w, Height: bar.Height - 34}
		_ = canvas.GradientFillRectangle(walkColor(lighten(statusColor(s.Status))), walkColor(statusColor(s.Status)), walk.Vertical, rect)
		if d.selectedSession != nil && d.selectedSession.ID == s.ID {
			border(canvas, rect, cOrange2)
		}
	}
}

func (d *Dashboard) drawRangeSelection(canvas *walk.Canvas, bar walk.Rectangle) {
	if d.rangeStart == nil || d.rangeEnd == nil || d.data.Date == "" {
		return
	}
	day, _ := time.ParseInLocation("2006-01-02", d.data.Date, time.Local)
	a, b := orderedTimes(*d.rangeStart, *d.rangeEnd)
	if !b.After(a) {
		return
	}
	x1 := bar.X + int(a.Sub(day).Seconds()*float64(bar.Width)/86400)
	x2 := bar.X + int(b.Sub(day).Seconds()*float64(bar.Width)/86400)
	if x2 < x1 {
		x1, x2 = x2, x1
	}
	selection := walk.Rectangle{X: x1, Y: bar.Y + 1, Width: max(2, x2-x1), Height: bar.Height - 2}
	border(canvas, selection, cOrange2)
}

func (d *Dashboard) drawPrograms(canvas *walk.Canvas, content walk.Rectangle) {
	r := walk.Rectangle{X: content.X, Y: content.Y + 472, Width: content.Width, Height: content.Height - 472}
	panel(canvas, r)
	title := "Program Usage"
	if d.rangeStart != nil && d.rangeEnd != nil {
		a, b := orderedTimes(*d.rangeStart, *d.rangeEnd)
		title = fmt.Sprintf("Program Usage (%s - %s)", a.Format("15:04"), b.Format("15:04"))
	}
	text(canvas, title, d.fonts["section"], cText, walk.Rectangle{X: r.X + 16, Y: r.Y + 14, Width: 360, Height: 22}, walk.TextLeft)
	head := walk.Rectangle{X: r.X + 16, Y: r.Y + 48, Width: r.Width - 32, Height: 34}
	fill(canvas, cPanel3, head)
	headers := []string{"Program", "Total", "First", "Last", "% Active", "Path"}
	xs := []int{head.X + 48, head.X + 318, head.X + 428, head.X + 528, head.X + 628, head.X + 738}
	for i, h := range headers {
		text(canvas, h, d.fonts["button"], cOrange2, walk.Rectangle{X: xs[i], Y: head.Y + 8, Width: 150, Height: 18}, walk.TextLeft)
	}
	rowsTop := head.Y + head.Height
	rowH := 34
	maxRows := (r.Y + r.Height - rowsTop - 12) / rowH
	programs := d.filteredPrograms()
	d.regions["programs"] = r
	totalRows := len(programs)
	maxScroll := max(0, totalRows-maxRows)
	if d.programScroll > maxScroll {
		d.programScroll = maxScroll
	}
	if d.programScroll < 0 {
		d.programScroll = 0
	}
	for i := 0; i < maxRows; i++ {
		row := walk.Rectangle{X: head.X, Y: rowsTop + i*rowH, Width: head.Width, Height: rowH}
		if i%2 == 0 {
			fill(canvas, cPanel, row)
		} else {
			fill(canvas, cPanel2, row)
		}
		border(canvas, walk.Rectangle{X: row.X, Y: row.Y + row.Height - 1, Width: row.Width, Height: 1}, cLine)
		rowIndex := i + d.programScroll
		if rowIndex >= totalRows {
			continue
		}
		p := programs[rowIndex]
		if !d.drawExecutableIcon(canvas, p.ExecutablePath, walk.Rectangle{X: row.X + 12, Y: row.Y + 7, Width: 20, Height: 20}) {
			drawProgramIcon(canvas, p.ProcessName, walk.Rectangle{X: row.X + 12, Y: row.Y + 7, Width: 20, Height: 20}, d.fonts["small"])
		}
		values := []string{
			emptyAs(p.ProcessName, "Unknown"),
			formatDuration(p.TotalSeconds),
			formatTime(p.FirstUsed),
			formatTime(p.LastUsed),
			fmt.Sprintf("%.1f%%", p.PercentActiveTime),
			p.ExecutablePath,
		}
		widths := []int{250, 90, 80, 80, 90, row.Width - 738}
		for col, value := range values {
			color := cText
			if col == 1 || col == 4 {
				color = cOrange2
			}
			text(canvas, value, d.fonts["body"], color, walk.Rectangle{X: xs[col], Y: row.Y + 8, Width: widths[col], Height: 18}, walk.TextLeft)
		}
	}
	if totalRows == 0 {
		msg := "No active program time recorded for this date."
		if d.rangeStart != nil && d.rangeEnd != nil {
			msg = "No active programs in selected timeline period."
		}
		text(canvas, msg, d.fonts["body"], cMuted, walk.Rectangle{X: head.X + 12, Y: rowsTop + 14, Width: head.Width - 24, Height: 24}, walk.TextLeft)
	}
	if totalRows > maxRows {
		track := walk.Rectangle{X: head.X + head.Width - 8, Y: rowsTop + 4, Width: 4, Height: maxRows*rowH - 8}
		fill(canvas, cPanel3, track)
		thumbH := max(18, track.Height*maxRows/totalRows)
		thumbY := track.Y + (track.Height-thumbH)*d.programScroll/max(1, maxScroll)
		fill(canvas, cOrange2, walk.Rectangle{X: track.X, Y: thumbY, Width: track.Width, Height: thumbH})
	}
}

func (d *Dashboard) button(canvas *walk.Canvas, id, label string, r walk.Rectangle, primary bool) {
	d.regions[id] = r
	top, bottom := cPanel3, cPanel2
	textColor := cText
	if primary {
		top, bottom = cOrange2, cOrange3
		textColor = walk.RGB(31, 17, 8)
	}
	_ = canvas.GradientFillRectangle(walkColor(top), walkColor(bottom), walk.Vertical, r)
	border(canvas, r, cLine)
	text(canvas, label, d.fonts["button"], textColor, r, walk.TextCenter)
}

func (d *Dashboard) iconButton(canvas *walk.Canvas, id, icon string, r walk.Rectangle) {
	d.regions[id] = r
	_ = canvas.GradientFillRectangle(walkColor(cPanel3), walkColor(cPanel2), walk.Vertical, r)
	border(canvas, r, cLine)
	pen, _ := walk.NewCosmeticPen(walk.PenSolid, cText)
	defer pen.Dispose()
	cx := r.X + r.Width/2
	cy := r.Y + r.Height/2
	switch icon {
	case "left":
		_ = canvas.DrawLine(pen, walk.Point{X: cx + 5, Y: cy - 8}, walk.Point{X: cx - 4, Y: cy})
		_ = canvas.DrawLine(pen, walk.Point{X: cx - 4, Y: cy}, walk.Point{X: cx + 5, Y: cy + 8})
	case "right":
		_ = canvas.DrawLine(pen, walk.Point{X: cx - 5, Y: cy - 8}, walk.Point{X: cx + 4, Y: cy})
		_ = canvas.DrawLine(pen, walk.Point{X: cx + 4, Y: cy}, walk.Point{X: cx - 5, Y: cy + 8})
	case "min":
		_ = canvas.DrawLine(pen, walk.Point{X: cx - 7, Y: cy + 5}, walk.Point{X: cx + 7, Y: cy + 5})
	case "close":
		_ = canvas.DrawLine(pen, walk.Point{X: cx - 6, Y: cy - 6}, walk.Point{X: cx + 6, Y: cy + 6})
		_ = canvas.DrawLine(pen, walk.Point{X: cx + 6, Y: cy - 6}, walk.Point{X: cx - 6, Y: cy + 6})
	}
}

func (d *Dashboard) drawResizeGrip(canvas *walk.Canvas, b walk.Rectangle) {
	r := walk.Rectangle{X: b.Width - 22, Y: b.Height - 22, Width: 18, Height: 18}
	d.regions["resize"] = r
	pen, _ := walk.NewCosmeticPen(walk.PenSolid, cMuted)
	defer pen.Dispose()
	for i := 0; i < 3; i++ {
		offset := 4 + i*5
		_ = canvas.DrawLine(pen, walk.Point{X: r.X + offset, Y: r.Y + r.Height - 2}, walk.Point{X: r.X + r.Width - 2, Y: r.Y + offset})
	}
}

func panel(canvas *walk.Canvas, r walk.Rectangle) {
	_ = canvas.GradientFillRectangle(walkColor(cPanel2), walkColor(cPanel), walk.Vertical, r)
	border(canvas, r, cLine)
}

func (d *Dashboard) drawLogo(canvas *walk.Canvas, x, y int) {
	r := walk.Rectangle{X: x, Y: y, Width: 32, Height: 32}
	_ = canvas.GradientFillRectangle(walkColor(cOrange2), walkColor(cOrange3), walk.Vertical, r)
	text(canvas, "B", d.fonts["title"], walk.RGB(40, 20, 8), walk.Rectangle{X: x, Y: y + 1, Width: 32, Height: 30}, walk.TextCenter)
}

func (d *Dashboard) legend(canvas *walk.Canvas, x, y int, color walk.Color, label string) {
	fill(canvas, color, walk.Rectangle{X: x, Y: y + 4, Width: 11, Height: 11})
	text(canvas, label, d.fonts["small"], cMuted, walk.Rectangle{X: x + 17, Y: y, Width: 76, Height: 18}, walk.TextLeft)
}

func drawProgramIcon(canvas *walk.Canvas, name string, r walk.Rectangle, font *walk.Font) {
	fill(canvas, cPanel3, r)
	border(canvas, r, cLine)
	initial := "?"
	for _, ch := range name {
		if ch != '.' && ch != ' ' {
			initial = string(ch)
			break
		}
	}
	text(canvas, initial, font, cOrange2, r, walk.TextCenter)
}

func (d *Dashboard) drawExecutableIcon(canvas *walk.Canvas, path string, r walk.Rectangle) bool {
	if path == "" {
		return false
	}
	icon := d.iconCache[path]
	if icon == nil {
		loaded, err := walk.NewIconExtractedFromFileWithSize(path, 0, 20)
		if err != nil {
			return false
		}
		icon = loaded
		d.iconCache[path] = icon
	}
	return canvas.DrawImage(icon, walk.Point{X: r.X, Y: r.Y}) == nil
}

func (d *Dashboard) filteredPrograms() []store.ProgramUsage {
	if d.rangeStart == nil || d.rangeEnd == nil {
		return d.data.Programs
	}
	start, end := orderedTimes(*d.rangeStart, *d.rangeEnd)
	type agg struct {
		store.ProgramUsage
	}
	byProgram := map[string]*agg{}
	var totalActive int64
	now := time.Now()
	for _, s := range d.data.Sessions {
		if s.Status != store.StatusActive {
			continue
		}
		sessionEnd := now
		if s.EndTime != nil {
			sessionEnd = *s.EndTime
		}
		from := maxTime(s.StartTime, start)
		to := minTime(sessionEnd, end)
		if !to.After(from) {
			continue
		}
		seconds := int64(to.Sub(from).Seconds())
		totalActive += seconds
		key := strings.ToLower(s.ProcessName) + "\x00" + strings.ToLower(s.ExecutablePath)
		item := byProgram[key]
		if item == nil {
			item = &agg{ProgramUsage: store.ProgramUsage{ProcessName: s.ProcessName, ExecutablePath: s.ExecutablePath}}
			byProgram[key] = item
		}
		item.TotalSeconds += seconds
		if item.FirstUsed == nil || from.Before(*item.FirstUsed) {
			v := from
			item.FirstUsed = &v
		}
		if item.LastUsed == nil || to.After(*item.LastUsed) {
			v := to
			item.LastUsed = &v
		}
	}
	out := make([]store.ProgramUsage, 0, len(byProgram))
	for _, item := range byProgram {
		if totalActive > 0 {
			item.PercentActiveTime = float64(item.TotalSeconds) * 100 / float64(totalActive)
		}
		out = append(out, item.ProgramUsage)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].TotalSeconds > out[j].TotalSeconds
	})
	return out
}

func orderedTimes(a, b time.Time) (time.Time, time.Time) {
	if a.After(b) {
		return b, a
	}
	return a, b
}

func (d *Dashboard) mouseMove(x, y int, button walk.MouseButton) {
	d.updateCursor(x, y)
	if d.resizing {
		dx := x - d.resizeStart.X
		dy := y - d.resizeStart.Y
		bounds := d.resizeBounds
		bounds.Width = max(1040, bounds.Width+dx)
		bounds.Height = max(760, bounds.Height+dy)
		_ = d.SetBounds(bounds)
		d.canvas.Invalidate()
		return
	}
	if d.selectingRange && in(d.regions["timeline"], x, y) {
		t := d.timeAtTimelineX(x)
		d.rangeEnd = &t
		a, b := orderedTimes(*d.rangeStart, *d.rangeEnd)
		d.detail = fmt.Sprintf("Selected period: %s - %s", a.Format("15:04"), b.Format("15:04"))
		d.programScroll = 0
		d.canvas.Invalidate()
		return
	}
	if s := d.sessionAt(x, y); s != nil {
		d.hoverSession = s
		d.detail = sessionSummary(*s)
		d.canvas.Invalidate()
	}
}

func (d *Dashboard) mouseDown(x, y int, button walk.MouseButton) {
	switch {
	case in(d.regions["resize"], x, y):
		d.resizing = true
		d.resizeStart = walk.Point{X: x, Y: y}
		d.resizeBounds = d.Bounds()
		d.canvas.SetCursor(walk.CursorSizeNWSE())
	case in(d.regions["close"], x, y):
		_ = d.Close()
	case in(d.regions["min"], x, y):
		win.ShowWindow(d.Handle(), win.SW_MINIMIZE)
	case in(d.regions["prev"], x, y):
		d.day = d.day.AddDate(0, 0, -1)
		d.refresh()
	case in(d.regions["next"], x, y):
		d.day = d.day.AddDate(0, 0, 1)
		d.refresh()
	case in(d.regions["today"], x, y):
		d.day = dayStart(time.Now())
		d.refresh()
	case in(d.regions["tracking"], x, y):
		_ = postJSON(d.apiURL+"/api/tracking", map[string]bool{"enabled": !d.data.Tracking})
		d.refresh()
	case in(d.regions["web"], x, y):
		_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", d.apiURL).Start()
	case in(d.regions["timeline"], x, y):
		t := d.timeAtTimelineX(x)
		d.rangeStart = &t
		d.rangeEnd = &t
		d.selectingRange = true
		d.selectedSession = d.sessionAt(x, y)
		d.programScroll = 0
		d.detail = fmt.Sprintf("Selected period starts at %s", t.Format("15:04"))
		d.canvas.Invalidate()
	case in(d.regions["drag"], x, y):
		win.ReleaseCapture()
		win.SendMessage(d.Handle(), win.WM_NCLBUTTONDOWN, win.HTCAPTION, 0)
	}
}

func (d *Dashboard) updateCursor(x, y int) {
	switch {
	case in(d.regions["resize"], x, y):
		d.canvas.SetCursor(walk.CursorSizeNWSE())
	case d.isHot(x, y):
		d.canvas.SetCursor(walk.CursorHand())
	case in(d.regions["timeline"], x, y):
		d.canvas.SetCursor(walk.CursorCross())
	default:
		d.canvas.SetCursor(walk.CursorArrow())
	}
}

func (d *Dashboard) isHot(x, y int) bool {
	for _, id := range []string{"close", "min", "prev", "next", "today", "tracking", "web", "date"} {
		if in(d.regions[id], x, y) {
			return true
		}
	}
	return false
}

func (d *Dashboard) mouseUp(x, y int, button walk.MouseButton) {
	if d.resizing {
		d.resizing = false
		d.canvas.SetCursor(walk.CursorArrow())
		return
	}
	if !d.selectingRange {
		return
	}
	d.selectingRange = false
	t := d.timeAtTimelineX(x)
	d.rangeEnd = &t
	if d.rangeStart != nil && d.rangeEnd != nil {
		a, b := orderedTimes(*d.rangeStart, *d.rangeEnd)
		if b.Sub(a) < time.Minute {
			d.rangeStart = nil
			d.rangeEnd = nil
			if s := d.sessionAt(x, y); s != nil {
				d.detail = sessionSummary(*s)
			} else {
				d.detail = "Move across the timeline to inspect activity blocks."
			}
		} else {
			d.detail = fmt.Sprintf("Selected period: %s - %s", a.Format("15:04"), b.Format("15:04"))
		}
	}
	d.programScroll = 0
	d.canvas.Invalidate()
}

func (d *Dashboard) mouseWheel(x, y int, button walk.MouseButton) {
	if !in(d.regions["programs"], x, y) {
		return
	}
	delta := walk.MouseWheelEventDelta(button)
	if delta > 0 {
		d.programScroll -= 3
	} else {
		d.programScroll += 3
	}
	if d.programScroll < 0 {
		d.programScroll = 0
	}
	d.canvas.Invalidate()
}

func (d *Dashboard) sessionAt(x, y int) *store.Session {
	bar, ok := d.regions["timeline"]
	if !ok || !in(bar, x, y) || d.data.Date == "" {
		return nil
	}
	day, _ := time.ParseInLocation("2006-01-02", d.data.Date, time.Local)
	point := day.Add(time.Duration(float64(x-bar.X)/float64(bar.Width)*86400) * time.Second)
	now := time.Now()
	for i := range d.data.Sessions {
		s := &d.data.Sessions[i]
		end := now
		if s.EndTime != nil {
			end = *s.EndTime
		}
		if !point.Before(s.StartTime) && point.Before(end) {
			return s
		}
	}
	return nil
}

func (d *Dashboard) timeAtTimelineX(x int) time.Time {
	bar := d.regions["timeline"]
	if x < bar.X {
		x = bar.X
	}
	if x > bar.X+bar.Width {
		x = bar.X + bar.Width
	}
	day := d.day
	if d.data.Date != "" {
		if parsed, err := time.ParseInLocation("2006-01-02", d.data.Date, time.Local); err == nil {
			day = parsed
		}
	}
	return day.Add(time.Duration(float64(x-bar.X)/float64(bar.Width)*86400) * time.Second)
}

func fill(canvas *walk.Canvas, c walk.Color, r walk.Rectangle) {
	brush, _ := walk.NewSolidColorBrush(c)
	defer brush.Dispose()
	_ = canvas.FillRectangle(brush, r)
}

func border(canvas *walk.Canvas, r walk.Rectangle, c walk.Color) {
	pen, _ := walk.NewCosmeticPen(walk.PenSolid, c)
	defer pen.Dispose()
	_ = canvas.DrawRectangle(pen, r)
}

func text(canvas *walk.Canvas, value string, font *walk.Font, color walk.Color, r walk.Rectangle, format walk.DrawTextFormat) {
	_ = canvas.DrawText(value, font, color, r, format|walk.TextVCenter|walk.TextSingleLine|walk.TextEndEllipsis)
}

func walkColor(c walk.Color) walk.Color {
	return c
}

func statusColor(status string) walk.Color {
	switch status {
	case store.StatusActive:
		return cOrange
	case store.StatusLocked:
		return cLocked
	case store.StatusIdle:
		return cIdle
	default:
		return cUnknown
	}
}

func lighten(c walk.Color) walk.Color {
	r := int(byte(c))
	g := int(byte(c >> 8))
	b := int(byte(c >> 16))
	return walk.RGB(byte(min(255, r+42)), byte(min(255, g+34)), byte(min(255, b+24)))
}

func sessionSummary(s store.Session) string {
	end := time.Now()
	if s.EndTime != nil {
		end = *s.EndTime
	}
	return fmt.Sprintf("%s | %s - %s | %s | %s",
		emptyAs(s.ProcessName, s.Status),
		s.StartTime.Format("15:04"),
		end.Format("15:04"),
		formatDuration(int64(end.Sub(s.StartTime).Seconds())),
		s.WindowTitle)
}

func clampTime(t, start, end time.Time) time.Time {
	if t.Before(start) {
		return start
	}
	if t.After(end) {
		return end
	}
	return t
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

func getJSON(url string, target any) error {
	res, err := http.Get(url)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return fmt.Errorf("request failed: %s", res.Status)
	}
	return json.NewDecoder(res.Body).Decode(target)
}

func postJSON(url string, value any) error {
	body, _ := json.Marshal(value)
	res, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return fmt.Errorf("request failed: %s", res.Status)
	}
	return nil
}

func formatDuration(seconds int64) string {
	if seconds < 0 {
		seconds = 0
	}
	h := seconds / 3600
	m := (seconds % 3600) / 60
	if h > 0 {
		return fmt.Sprintf("%dh %02dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

func formatTime(t *time.Time) string {
	if t == nil {
		return "-"
	}
	return t.Format("15:04")
}

func dayStart(t time.Time) time.Time {
	y, m, d := t.Local().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Local().Location())
}

func in(r walk.Rectangle, x, y int) bool {
	return x >= r.X && x < r.X+r.Width && y >= r.Y && y < r.Y+r.Height
}

func emptyAs(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func createAppIcon() (*walk.Icon, error) {
	const size = 32
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	center := float64(size-1) / 2
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx := float64(x) - center
			dy := float64(y) - center
			if dx*dx+dy*dy > 14.5*14.5 {
				continue
			}
			col := color.RGBA{R: 240, G: 185, B: 11, A: 255}
			if y < 12 {
				col = color.RGBA{R: 252, G: 213, B: 53, A: 255}
			}
			if x >= 11 && x <= 20 && y >= 9 && y <= 23 {
				col = color.RGBA{R: 11, G: 14, B: 17, A: 255}
			}
			if x >= 12 && x <= 19 && y >= 10 && y <= 12 {
				col = color.RGBA{R: 255, G: 229, B: 80, A: 255}
			}
			img.SetRGBA(x, y, col)
		}
	}
	return walk.NewIconFromImageForDPI(img, 96)
}
