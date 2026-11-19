package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var errNoTUI = errors.New("this console cannot show the full-screen interface")

var (
	pasteStart = []byte("\x1b[200~")
	pasteEnd   = []byte("\x1b[201~")
)

var arrowKeys = map[byte]string{'A': "up", 'B': "down", 'C': "right", 'D': "left", 'H': "home", 'F': "end"}

type key struct {
	name string // "char" for printable characters
	r    rune
}

// parseKeys turns raw terminal input into key presses. Escape sequences that dlmod does not use
// (function keys, focus reports) are dropped.
func parseKeys(data []byte) []key {
	var keys []key
	for len(data) > 0 {
		switch b := data[0]; {
		case b == 0x1b && len(data) >= 3 && (data[1] == '[' || data[1] == 'O'):
			end := 2
			for end < len(data) && (data[end] < 0x40 || data[end] > 0x7e) {
				end++
			}
			if end == len(data) {
				return append(keys, key{name: "esc"})
			}
			sequence := string(data[2 : end+1])
			data = data[end+1:]
			if name, ok := arrowKeys[sequence[len(sequence)-1]]; ok {
				keys = append(keys, key{name: name})
				continue
			}
			switch sequence {
			case "1~", "7~":
				keys = append(keys, key{name: "home"})
			case "4~", "8~":
				keys = append(keys, key{name: "end"})
			case "5~":
				keys = append(keys, key{name: "pgup"})
			case "6~":
				keys = append(keys, key{name: "pgdn"})
			case "3~":
				keys = append(keys, key{name: "delete"})
			}
		case b == 0x1b:
			keys = append(keys, key{name: "esc"})
			data = data[1:]
		case b == '\r' || b == '\n':
			keys = append(keys, key{name: "enter"})
			if b == '\r' && len(data) > 1 && data[1] == '\n' {
				data = data[1:]
			}
			data = data[1:]
		case b == 0x7f || b == 0x08:
			keys = append(keys, key{name: "backspace"})
			data = data[1:]
		case b == 0x03:
			keys = append(keys, key{name: "ctrl+c"})
			data = data[1:]
		case b == 0x15:
			keys = append(keys, key{name: "ctrl+u"})
			data = data[1:]
		case b < 0x20:
			data = data[1:]
		default:
			r, size := utf8.DecodeRune(data)
			keys = append(keys, key{name: "char", r: r})
			data = data[size:]
		}
	}
	return keys
}

// splitPaste separates text pasted between bracketed-paste markers from typed keys.
func splitPaste(data []byte) (typed []byte, pasted string, ok bool) {
	start := bytes.Index(data, pasteStart)
	if start < 0 {
		return data, "", false
	}
	rest := data[start+len(pasteStart):]
	end := bytes.Index(rest, pasteEnd)
	if end < 0 {
		end = len(rest)
	}
	typed = append(append([]byte{}, data[:start]...), rest[min(end+len(pasteEnd), len(rest)):]...)
	return typed, string(rest[:end]), true
}

// looksPasted recognises a dropped file in consoles without bracketed paste: several characters
// in one burst that contain a path separator. Nobody types that fast.
func looksPasted(data []byte) bool {
	text := strings.TrimRight(string(data), "\r\n")
	return len(text) >= 3 && !strings.ContainsAny(text, "\x1b\x03") && strings.ContainsAny(text, `\/`)
}

// droppedFiles turns pasted text into file paths. Windows quotes dropped paths that contain
// spaces and separates several files with spaces.
func droppedFiles(text string) []string {
	text = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), "& "))
	if text == "" {
		return nil
	}
	if isFile(strings.Trim(text, `"'`)) {
		return []string{strings.Trim(text, `"'`)}
	}
	var files []string
	var current strings.Builder
	quote := rune(0)
	flush := func() {
		if current.Len() > 0 {
			files = append(files, current.String())
			current.Reset()
		}
	}
	for _, r := range text {
		switch {
		case quote != 0 && r == quote:
			quote = 0
			flush()
		case quote == 0 && (r == '"' || r == '\''):
			quote = r
		case quote == 0 && unicode.IsSpace(r):
			flush()
		default:
			current.WriteRune(r)
		}
	}
	flush()
	return files
}

func truncate(text string, width int) string {
	if width <= 0 {
		return ""
	}
	if utf8.RuneCountInString(text) <= width {
		return text
	}
	runes := []rune(text)
	return string(runes[:width-1]) + "…"
}

// truncateMiddle keeps both ends of a path: D:\SteamLibrary\…\common\Deadlock.
func truncateMiddle(text string, width int) string {
	runes := []rune(text)
	if len(runes) <= width || width < 9 {
		return truncate(text, width)
	}
	head := (width - 1) / 2
	return string(runes[:head]) + "…" + string(runes[len(runes)-(width-1-head):])
}

func wrap(text string, width int) []string {
	if width < 10 {
		return []string{truncate(text, width)}
	}
	var lines []string
	line := ""
	for _, word := range strings.Fields(text) {
		switch {
		case line == "":
			line = word
		case utf8.RuneCountInString(line)+1+utf8.RuneCountInString(word) <= width:
			line += " " + word
		default:
			lines = append(lines, line)
			line = word
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	for i, l := range lines {
		lines[i] = truncate(l, width)
	}
	return lines
}

// canvas collects screen lines; each segment is clipped so a line never exceeds the width.
type canvas struct {
	width int
	p     palette
	lines []string
}

type seg struct{ text, style string }

func (c *canvas) line(segments ...seg) {
	var b strings.Builder
	room := c.width
	for _, s := range segments {
		if room <= 0 {
			break
		}
		text := truncate(s.text, room)
		room -= utf8.RuneCountInString(text)
		b.WriteString(c.p.paint(s.style, text))
	}
	c.lines = append(c.lines, b.String())
}

func (c *canvas) blank() { c.lines = append(c.lines, "") }

func pad(text string, width int) string {
	if gap := width - utf8.RuneCountInString(text); gap > 0 {
		return text + strings.Repeat(" ", gap)
	}
	return truncate(text, width)
}

const (
	modeList = iota
	modePrompt
	modeHelp
	modeQuestion
)

const (
	toneInfo = iota
	toneGood
	toneWarn
	toneBad
)

type note struct {
	text string
	tone int
}

type tui struct {
	game     Game
	store    *Store
	entries  []*Entry
	clashes  map[*Entry][]Conflict
	pairs    int // conflicting pairs of mods
	state    gameinfoState
	stateErr error
	build    int
	running  bool

	selected, top int
	mode          int
	input         []rune
	question      string
	options       []string
	choice        int
	busy          string
	notes         []note
	width, height int
	size          func() (int, int)
	stamp         [4]fileStamp
	ticks         int

	keys  <-chan []byte
	out   *bufio.Writer
	p     palette
	frame bytes.Buffer
	shown []byte
}

func readInput(r io.Reader, keys chan<- []byte) {
	buffer := make([]byte, 4096)
	for {
		n, err := r.Read(buffer)
		if n > 0 {
			keys <- append([]byte(nil), buffer[:n]...)
		}
		if err != nil {
			close(keys)
			return
		}
	}
}

// runTUI shows the full-screen interface. It sleeps between key presses and checks for outside
// changes (a game update resetting gameinfo.gi, mods added by hand, the game starting) once a
// second, redrawing only when something changed.
func runTUI(game Game) error {
	if runtime.GOOS != "windows" && os.Getenv("TERM") == "dumb" {
		return errNoTUI
	}
	restore, err := enterRaw()
	if err != nil {
		return errNoTUI
	}
	defer restore()
	keys := make(chan []byte, 64)
	go readInput(os.Stdin, keys)
	t := &tui{game: game, keys: keys, out: bufio.NewWriterSize(os.Stdout, 32<<10), p: newPalette(os.Getenv("NO_COLOR") == ""),
		size: terminalSize}
	t.out.WriteString("\x1b[?1049h\x1b[?25l\x1b[?2004h") // alternate screen, hidden cursor, bracketed paste
	defer func() {
		t.out.WriteString("\x1b[?2004l\x1b[?25h\x1b[?1049l")
		t.out.Flush()
	}()
	t.reload()
	t.render()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case data, ok := <-keys:
			if ok && mightContinue(data) {
				data = t.gather(data)
			}
			if !ok || t.handle(data) {
				return nil
			}
		case <-ticker.C:
			if !t.refresh() {
				continue
			}
		}
		t.render()
	}
}

// mightContinue reports whether input could be the start of something longer: an open
// bracketed paste, a lone Esc that may begin an arrow key, or the first character of a dropped
// path (a quote, a slash or a drive letter). Everything else is handled at once.
func mightContinue(data []byte) bool {
	switch {
	case bytes.Contains(data, pasteStart):
		return !bytes.Contains(data, pasteEnd)
	case len(data) == 1 && data[0] == 0x1b:
		return true
	case len(data) > 0 && (strings.IndexByte("\"'/\\~", data[0]) >= 0 || (data[0] >= 'A' && data[0] <= 'Z')):
		return true
	}
	return false
}

// gather waits briefly for the rest of a burst, so a dropped file arrives as one paste.
func (t *tui) gather(data []byte) []byte {
	for {
		wait := 12 * time.Millisecond
		if bytes.Contains(data, pasteStart) && !bytes.Contains(data, pasteEnd) {
			wait = 300 * time.Millisecond
		}
		select {
		case more, ok := <-t.keys:
			if !ok {
				return data
			}
			data = append(data, more...)
		case <-time.After(wait):
			return data
		}
	}
}

func (t *tui) handle(data []byte) bool {
	typed, pasted, ok := splitPaste(data)
	if !ok && t.mode == modeList && looksPasted(data) {
		typed, pasted, ok = nil, string(data), true
	}
	if ok {
		t.paste(pasted)
	}
	for _, k := range parseKeys(typed) {
		if t.press(k) {
			return true
		}
	}
	return false
}

// paste installs dropped mod files straight away; anything else lands in the path prompt.
func (t *tui) paste(text string) {
	if t.mode != modeList && t.mode != modePrompt {
		return
	}
	files := droppedFiles(text)
	ready := len(files) > 0
	for _, file := range files {
		extension := strings.ToLower(filepath.Ext(file))
		ready = ready && isFile(file) && (extension == ".zip" || extension == ".vpk")
	}
	if ready {
		t.install(files)
		return
	}
	t.mode = modePrompt
	t.input = append(t.input, []rune(strings.TrimRight(text, "\r\n"))...)
}

func (t *tui) press(k key) bool {
	switch t.mode {
	case modeHelp:
		t.mode = modeList
		return false
	case modePrompt:
		switch k.name {
		case "char":
			t.input = append(t.input, k.r)
		case "backspace":
			if len(t.input) > 0 {
				t.input = t.input[:len(t.input)-1]
			}
		case "ctrl+u":
			t.input = nil
		case "enter":
			files := droppedFiles(string(t.input))
			t.input = nil
			if len(files) == 0 {
				t.mode = modeList
				return false
			}
			t.install(files)
		case "esc", "ctrl+c":
			t.mode, t.input = modeList, nil
		}
		return false
	}
	switch k.name {
	case "up":
		t.move(-1)
	case "down":
		t.move(1)
	case "pgup":
		t.move(-10)
	case "pgdn":
		t.move(10)
	case "home":
		t.move(-len(t.entries))
	case "end":
		t.move(len(t.entries))
	case "enter":
		t.toggle()
	case "delete":
		t.remove()
	case "esc", "ctrl+c":
		return true
	case "char":
		switch unicode.ToLower(k.r) {
		case 'q':
			return true
		case 'k':
			t.move(-1)
		case 'j':
			t.move(1)
		case ' ':
			t.toggle()
		case 'i', '+':
			t.mode, t.notes = modePrompt, nil
		case 'd', '-':
			t.remove()
		case 'r':
			t.repair()
		case 'u':
			t.unpatch()
		case 'p':
			t.play()
		case '?', 'h':
			t.mode = modeHelp
		}
	}
	return false
}

func (t *tui) move(delta int) {
	t.selected = max(0, min(len(t.entries)-1, t.selected+delta))
}

func (t *tui) current() *Entry {
	if t.selected < 0 || t.selected >= len(t.entries) {
		return nil
	}
	return t.entries[t.selected]
}

// act runs one change and shows its messages; changes wait until the game is closed.
// reload keeps the cursor on the same mod even when the change moves it.
func (t *tui) act(change func() error) {
	t.notes = nil
	if t.running = gameRunning(); t.running {
		t.note(toneBad, "Deadlock is running. Close it to change mods.")
		return
	}
	if err := change(); err != nil {
		t.note(toneBad, err.Error())
	}
	t.busy = ""
	t.reload()
}

func (t *tui) toggle() {
	entry := t.current()
	if entry == nil {
		return
	}
	command := "off"
	if !entry.Enabled() {
		command = "on"
	}
	t.act(func() error { return change(t, t.store, entry, command) })
}

func (t *tui) remove() {
	entry := t.current()
	if entry == nil || !t.confirm(fmt.Sprintf("Delete '%s'? This removes its files.", entry.Name()), false) {
		return
	}
	t.act(func() error { return change(t, t.store, entry, "remove") })
}

func (t *tui) repair() {
	t.act(func() error {
		changed, err := t.game.patch()
		if err == nil && !changed {
			t.note(toneGood, "gameinfo.gi is fine; the game loads mods from game/citadel/addons.")
		} else if err == nil {
			t.note(toneGood, "Repaired gameinfo.gi; the game loads mods again.")
		}
		return err
	})
}

func (t *tui) unpatch() {
	if !t.confirm("Undo dlmod's gameinfo.gi change? Mods stay installed but stop loading.", false) {
		return
	}
	t.act(func() error {
		changed, err := t.game.unpatch()
		if err == nil && changed {
			t.note(toneInfo, "gameinfo.gi is back to Valve's version; mods no longer load. Press r to load them again.")
		} else if err == nil {
			t.note(toneInfo, "gameinfo.gi has no dlmod changes.")
		}
		return err
	})
}

func (t *tui) play() {
	t.act(func() error {
		if err := ensurePatched(t, t.game); err != nil {
			return err
		}
		t.note(toneGood, "Starting Deadlock through Steam…")
		return launchGame()
	})
}

func (t *tui) install(files []string) {
	t.mode, t.input = modeList, nil
	newest := 0
	t.act(func() error {
		for _, file := range files {
			if err := t.store.install(t, file, ""); err != nil {
				return fmt.Errorf("%s: %v", filepath.Base(file), err)
			}
		}
		for _, mod := range t.store.manifest.Mods {
			newest = max(newest, mod.ID)
		}
		return ensurePatched(t, t.game)
	})
	for i, entry := range t.entries {
		if entry.Mod != nil && entry.Mod.ID == newest {
			t.selected = i
		}
	}
}

// --- prompter ---

func (t *tui) note(tone int, text string) { t.notes = append(t.notes, note{text, tone}) }

func (t *tui) say(format string, args ...any) {
	text := fmt.Sprintf(format, args...)
	tone := toneGood
	if strings.HasPrefix(text, "Conflict") || strings.HasPrefix(text, "Skipped") {
		tone = toneWarn
	} else if strings.HasPrefix(text, "Removed") || strings.Contains(text, "already") {
		tone = toneInfo
	}
	t.note(tone, text)
}

// waitKey blocks until the next key press; questions are answered inside the action that asked them.
func (t *tui) waitKey() (key, bool) {
	for {
		data, ok := <-t.keys
		if !ok {
			return key{}, false
		}
		if keys := parseKeys(data); len(keys) > 0 {
			return keys[0], true
		}
	}
}

func (t *tui) ask(question string, options []string) func() {
	saved := t.mode
	t.mode, t.question, t.options, t.choice = modeQuestion, question, options, 0
	return func() { t.mode, t.question, t.options = saved, "", nil }
}

func (t *tui) confirm(question string, fallback bool) bool {
	defer t.ask(question, nil)()
	for {
		t.render()
		k, ok := t.waitKey()
		switch {
		case !ok || k.name == "esc" || k.name == "ctrl+c":
			return false
		case k.name == "enter":
			return fallback
		case k.name == "char" && strings.ContainsRune("yYeE", k.r):
			return true
		case k.name == "char" && strings.ContainsRune("nNhH", k.r):
			return false
		}
	}
}

func (t *tui) choose(question string, options []string) (int, bool, bool) {
	defer t.ask(question, options)()
	for {
		t.render()
		k, ok := t.waitKey()
		switch {
		case !ok || k.name == "esc" || k.name == "ctrl+c":
			return 0, false, false
		case k.name == "up":
			t.choice = max(0, t.choice-1)
		case k.name == "down":
			t.choice = min(len(options)-1, t.choice+1)
		case k.name == "enter":
			return t.choice, false, true
		case k.name == "char" && (k.r == 'a' || k.r == 'A'):
			return 0, true, true
		case k.name == "char" && k.r >= '1' && k.r <= '9' && int(k.r-'1') < len(options):
			return int(k.r - '1'), false, true
		}
	}
}

func (t *tui) progress(label string, done, total int64) {
	bar := 24
	filled := int(done * int64(bar) / max(total, 1))
	t.busy = fmt.Sprintf("Installing %s  %s%s %3d%%  %s / %s", truncate(label, 32), strings.Repeat("█", filled),
		strings.Repeat("░", bar-filled), done*100/max(total, 1), formatSize(done), formatSize(total))
	t.render()
}

// --- state ---

type fileStamp struct{ size, modified int64 }

// snapshot records the files whose change means the screen is out of date: gameinfo.gi, the
// addons folder, dlmod's list and steam.inf (a game update).
func (t *tui) snapshot() [4]fileStamp {
	var stamps [4]fileStamp
	for i, file := range [4]string{t.game.GameInfo(), t.game.Addons(), filepath.Join(t.game.StateDir(), "mods.json"),
		filepath.Join(t.game.Citadel(), "steam.inf")} {
		if info, err := os.Stat(file); err == nil {
			stamps[i] = fileStamp{info.Size(), info.ModTime().UnixNano()}
		}
	}
	return stamps
}

// identity names a mod across reloads: its dlmod ID, or its slot for mods added by hand.
func identity(entry *Entry) string {
	switch {
	case entry == nil:
		return ""
	case entry.Mod != nil:
		return fmt.Sprintf("id %d", entry.Mod.ID)
	default:
		return fmt.Sprintf("slot %d", entry.Slot)
	}
}

func (t *tui) reselect(name string) {
	for i, entry := range t.entries {
		if name != "" && identity(entry) == name {
			t.selected = i
			return
		}
	}
	t.move(0)
}

// reload reads everything again; the cursor stays on the same mod.
func (t *tui) reload() {
	selected := identity(t.current())
	defer t.reselect(selected)
	store, err := openStore(t.game)
	if err != nil {
		t.note(toneBad, err.Error())
		return
	}
	t.store = store
	if t.entries, err = store.entries(); err != nil {
		t.note(toneBad, err.Error())
	}
	t.clashes = map[*Entry][]Conflict{}
	all := conflicts(t.entries)
	t.pairs = len(all)
	for _, clash := range all {
		t.clashes[clash.A] = append(t.clashes[clash.A], clash)
		t.clashes[clash.B] = append(t.clashes[clash.B], clash)
	}
	_, t.state, t.stateErr = t.game.readGameinfo()
	t.build = t.game.Version()
	t.running = gameRunning()
	t.stamp = t.snapshot()
}

// refresh notices changes made outside dlmod and reports whether the screen must be redrawn.
func (t *tui) refresh() bool {
	changed := false
	if width, height := t.size(); width != t.width || height != t.height {
		changed = true
	}
	t.ticks++
	if runtime.GOOS == "windows" || t.ticks%3 == 0 {
		if running := gameRunning(); running != t.running {
			t.running, changed = running, true
		}
	}
	if stamp := t.snapshot(); stamp != t.stamp {
		t.reload()
		changed = true
	}
	return changed
}

// --- drawing ---

func (t *tui) render() {
	t.width, t.height = t.size()
	lines := t.lines()
	t.frame.Reset()
	t.frame.WriteString("\x1b[H")
	for i, line := range lines {
		if i > 0 {
			t.frame.WriteString("\r\n")
		}
		t.frame.WriteString(line)
		t.frame.WriteString("\x1b[K")
	}
	t.frame.WriteString("\x1b[J")
	if bytes.Equal(t.frame.Bytes(), t.shown) {
		return // nothing changed on screen
	}
	t.shown = append(t.shown[:0], t.frame.Bytes()...)
	t.out.Write(t.shown)
	t.out.Flush()
}

// lines lays out the screen. Short windows drop the spacing, the details block and older
// messages so the list keeps at least a few rows.
func (t *tui) lines() []string {
	width, height := max(t.width-1, 20), max(t.height, 10)
	compact := height < 20
	top := &canvas{width: width, p: t.p}
	t.drawHeader(top, compact)
	bottom := &canvas{width: width, p: t.p}
	t.drawBottom(bottom, compact)
	room := height - len(top.lines) - len(bottom.lines)
	middle := &canvas{width: width, p: t.p}
	if t.mode == modeHelp {
		t.drawHelp(middle)
	} else {
		t.drawList(middle, room, compact)
	}
	lines := append(top.lines, middle.lines[:min(len(middle.lines), max(room, 0))]...)
	for len(lines) < height-len(bottom.lines) {
		lines = append(lines, "")
	}
	lines = append(lines, bottom.lines...)
	return lines[max(0, len(lines)-height):]
}

func (t *tui) drawHeader(c *canvas, compact bool) {
	p := t.p
	title := " dlmod " + version + " "
	right := ""
	if t.build > 0 {
		right = fmt.Sprintf("Deadlock build %d ", t.build)
	}
	gap := max(1, c.width-utf8.RuneCountInString(title)-utf8.RuneCountInString(right))
	c.line(seg{title, p.bold + p.cyan}, seg{strings.Repeat(" ", gap), ""}, seg{right, p.dim})
	if !compact {
		c.line(seg{" " + truncateMiddle(t.game.Root, c.width-1), p.dim})
		c.blank()
	}
	switch {
	case t.stateErr != nil:
		c.line(seg{" MODS ? ", p.warnBadge}, seg{"  " + t.stateErr.Error(), p.yellow})
	case !t.state.Loaded:
		c.line(seg{" MODS OFF ", p.offBadge}, seg{"  A game update or Verify integrity reset gameinfo.gi. Press r to repair.", p.red})
	case len(t.state.Missing) > 0:
		c.line(seg{" MODS ON ", p.warnBadge}, seg{"  gameinfo.gi lacks " + strings.Join(t.state.Missing, ", ") +
			", so settings may land in addons. Press r to repair.", p.yellow})
	default:
		c.line(seg{" MODS ON ", p.okBadge}, seg{"  Deadlock loads mods from game/citadel/addons", ""})
	}
	if t.running {
		c.line(seg{" GAME RUNNING ", p.infoBadge}, seg{"  Close Deadlock to install, switch or delete mods.", p.dim})
	}
	if !compact {
		c.blank()
	}
}

func (t *tui) drawList(c *canvas, room int, compact bool) {
	p := t.p
	if len(t.entries) == 0 {
		c.line(seg{"  No mods installed yet.", p.bold})
		c.blank()
		c.line(seg{"  Drag a .zip or .vpk mod file into this window, or press ", p.dim}, seg{"i", p.bold}, seg{" to type its path.", p.dim})
		return
	}
	enabled := 0
	for _, entry := range t.entries {
		if entry.Enabled() {
			enabled++
		}
	}
	var details [][]seg
	if !compact {
		details = t.details(c.width)
	}
	rows := max(1, room-2-len(details))
	t.top = max(0, min(t.top, t.selected))
	if t.selected >= t.top+rows {
		t.top = t.selected - rows + 1
	}
	t.top = min(t.top, max(0, len(t.entries)-rows))

	// Columns: marker 3, state 5, slot 8, name, flag 2, files and size 18.
	nameWidth := max(8, c.width-36)
	summary := fmt.Sprintf("NAME · %d mods, %d on", len(t.entries), enabled)
	summary += plural(t.pairs, ", %d conflict", ", %d conflicts")
	c.line(seg{fmt.Sprintf("   %-5s %-7s%s  %7s %9s ", "STATE", "SLOT", pad(summary, nameWidth), "FILES", "SIZE"), p.dim})
	for i := t.top; i < min(len(t.entries), t.top+rows); i++ {
		entry := t.entries[i]
		selected := i == t.selected
		base := ""
		marker := "   "
		if selected {
			base, marker = p.row, " › "
		}
		state, stateStyle, slot := " ON  ", p.green, fmt.Sprintf("pak%02d  ", entry.Slot)
		if !entry.Enabled() {
			state, stateStyle, slot = " OFF ", p.dim, "       "
		}
		flag, flagStyle := "  ", ""
		switch {
		case entry.Err != nil:
			flag, flagStyle = " ×", p.red
		case len(t.clashes[entry]) > 0 || outdated(entry, t.build):
			flag, flagStyle = " !", p.yellow
		}
		files := ""
		if entry.Err == nil {
			files = fmt.Sprintf("%d", len(entry.Paths))
		}
		c.line(seg{marker, base + p.cyan}, seg{state, base + stateStyle}, seg{" " + slot, base},
			seg{pad(entry.Name(), nameWidth), base + p.bold}, seg{flag, base + flagStyle},
			seg{fmt.Sprintf("%7s %9s ", files, formatSize(entry.Size)), base + p.dim})
	}
	if hidden := len(t.entries) - (t.top + rows); hidden > 0 {
		c.line(seg{fmt.Sprintf("     ↓ %d more", hidden), p.dim})
	} else {
		c.blank()
	}
	for _, line := range details {
		c.line(line...)
	}
}

// details describes the selected mod in up to four lines.
func (t *tui) details(width int) [][]seg {
	entry := t.current()
	if entry == nil || t.mode == modeQuestion {
		return nil
	}
	p := t.p
	var lines [][]seg
	where := "switched off; stored in game/dlmod"
	if entry.Enabled() {
		where = fmt.Sprintf("loads as pak%02d_dir.vpk", entry.Slot)
	}
	lines = append(lines, []seg{{" " + entry.Name(), p.bold}, {"  " + where, p.dim}})
	if mod := entry.Mod; mod != nil && mod.Source != "" {
		when := mod.Installed
		if parsed, err := time.Parse(time.RFC3339, mod.Installed); err == nil {
			when = parsed.Local().Format("2 Jan 2006")
		}
		build := ""
		if mod.GameVersion > 0 {
			build = fmt.Sprintf(" on build %d", mod.GameVersion)
		}
		lines = append(lines, []seg{{fmt.Sprintf(" From %s, installed %s%s", mod.Source, when, build), p.dim}})
	} else if entry.Mod == nil {
		lines = append(lines, []seg{{" Added by hand or by another tool", p.dim}})
	}
	switch {
	case entry.Err != nil:
		lines = append(lines, wrapped("× Cannot read this mod: "+entry.Err.Error(), width, p.red)...)
	case len(entry.Paths) > 0:
		lines = append(lines, []seg{{" Changes " + describePaths(entry.Paths), p.dim}})
	}
	if clashes := t.clashes[entry]; len(clashes) > 0 {
		other := clashes[0].A
		if other == entry {
			other = clashes[0].B
		}
		text := fmt.Sprintf("! Changes the same files as pak%02d '%s' (%s); only one takes effect.",
			other.Slot, other.Name(), describePaths(clashes[0].Paths))
		if len(clashes) > 1 {
			text = fmt.Sprintf("! Changes the same files as %d other mods; only one version of each file takes effect.", len(clashes))
		}
		lines = append(lines, wrapped(text, width, p.yellow)...)
	} else if outdated(entry, t.build) {
		lines = append(lines, wrapped(fmt.Sprintf("! Installed on build %d; the game is on %d. If Deadlock fails to start, "+
			"switch this off.", entry.Mod.GameVersion, t.build), width, p.yellow)...)
	}
	return lines
}

// wrapped splits a warning over at most two lines, indenting the second.
func wrapped(text string, width int, style string) [][]seg {
	var lines [][]seg
	for i, line := range wrap(text, width-3) {
		if i == 2 {
			break
		}
		if i == 0 {
			line = " " + line
		} else {
			line = "   " + line
		}
		lines = append(lines, []seg{{line, style}})
	}
	return lines
}

func (t *tui) drawBottom(c *canvas, compact bool) {
	p := t.p
	switch t.mode {
	case modeQuestion:
		for _, line := range wrap(t.question, c.width-2) {
			c.line(seg{" " + line, p.bold})
		}
		for i, option := range t.options {
			style, marker := "", "   "
			if i == t.choice {
				style, marker = p.row+p.bold, " › "
			}
			c.line(seg{fmt.Sprintf("%s%d  %s ", marker, i+1, option), style})
		}
	case modePrompt:
		c.line(seg{" Install a mod: drag a .zip or .vpk file into this window, or type its path.", p.bold})
		c.line(seg{" > ", p.cyan}, seg{string(t.input), ""}, seg{" ", p.invert})
	}
	if t.busy != "" {
		c.line(seg{" " + t.busy, p.cyan})
	}
	styles := map[int]string{toneInfo: "", toneGood: p.green, toneWarn: p.yellow, toneBad: p.red}
	limit := 5
	if compact {
		limit = 1
	}
	if t.mode == modeQuestion {
		limit = 0 // the question is the only thing to read; messages return afterwards
	}
	var noteLines []seg
	for i := len(t.notes) - 1; i >= 0 && len(noteLines) < limit; i-- {
		wrapped := wrap(t.notes[i].text, c.width-2)
		if compact {
			wrapped = []string{truncate(t.notes[i].text, c.width-2)}
		}
		for j := len(wrapped) - 1; j >= 0 && len(noteLines) < limit; j-- {
			noteLines = append([]seg{{" " + wrapped[j], styles[t.notes[i].tone]}}, noteLines...)
		}
	}
	for _, line := range noteLines {
		c.line(line)
	}
	if !compact {
		c.line(seg{strings.Repeat("─", c.width), p.dim})
	}
	var hints [][2]string
	switch t.mode {
	case modeQuestion:
		if len(t.options) > 0 {
			hints = [][2]string{{"↑↓", "choose"}, {"Enter", "install"}, {"a", "all"}, {"Esc", "cancel"}}
		} else {
			hints = [][2]string{{"y", "yes"}, {"n", "no"}, {"Esc", "cancel"}}
		}
	case modePrompt:
		hints = [][2]string{{"Enter", "install"}, {"Ctrl+U", "clear"}, {"Esc", "cancel"}}
	case modeHelp:
		hints = [][2]string{{"any key", "back"}}
	default:
		hints = [][2]string{{"↑↓", "select"}, {"Space", "on/off"}, {"i", "install"}, {"d", "delete"},
			{"r", "repair"}, {"p", "play"}, {"?", "help"}, {"q", "quit"}}
	}
	full := 0
	for _, hint := range hints {
		full += utf8.RuneCountInString(hint[0]) + utf8.RuneCountInString(hint[1]) + 3
	}
	var segments []seg
	for _, hint := range hints {
		if full <= c.width {
			segments = append(segments, seg{" " + hint[0], p.bold + p.cyan}, seg{" " + hint[1] + " ", p.dim})
		} else {
			segments = append(segments, seg{" " + hint[0], p.bold + p.cyan}) // narrow window: keys only
		}
	}
	if full > c.width {
		segments = append(segments, seg{"   ? help", p.dim})
	}
	c.line(segments...)
}

func (t *tui) drawHelp(c *canvas) {
	p := t.p
	rows := [][2]string{
		{"↑ ↓  j k", "move between mods"},
		{"Space  Enter", "switch the selected mod on or off"},
		{"i", "install a mod; dragging a .zip or .vpk file into the window works too"},
		{"d  Delete", "delete the selected mod"},
		{"r", "repair gameinfo.gi after a game update"},
		{"u", "undo dlmod's gameinfo.gi change; mods stay installed but stop loading"},
		{"p", "start Deadlock through Steam"},
		{"q  Esc", "quit"},
	}
	c.line(seg{" Keys", p.bold})
	for _, row := range rows {
		c.line(seg{"   " + pad(row[0], 14), p.cyan}, seg{row[1], ""})
	}
	c.blank()
	c.line(seg{" How it works", p.bold})
	for _, text := range []string{
		"Mods live in game/citadel/addons as pak01_dir.vpk to pak99_dir.vpk. Switched-off mods wait in game/dlmod, where the game does not look.",
		"Game updates and Verify integrity reset gameinfo.gi. This screen then shows MODS OFF; press r.",
		"When two mods change the same file only one of them takes effect, so a ! marks both.",
		"Interface mods built for an older game build can stop Deadlock from starting after an update.",
	} {
		for _, line := range wrap(text, c.width-4) {
			c.line(seg{"   " + line, p.dim})
		}
	}
}
