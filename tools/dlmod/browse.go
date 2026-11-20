package main

import (
	"fmt"
	"image"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// The browser lists the online catalog (the one Deadlock Mod Manager uses) inside the
// full-screen view: filter by category and hero, search by name, see the selected mod's
// preview, and install it with Enter.

type previewResult struct {
	url     string
	picture image.Image
	err     error
}

// imageArea is where the preview goes on screen, for the sixel and kitty modes, which draw
// pictures over character cells after the text is written.
type imageArea struct {
	row, col, cols, rows int
	url                  string
}

type browser struct {
	mods                  []RemoteMod
	categories, heroes    []string
	category, hero        int // index into the lists above, -1 for all
	query                 []rune
	typing                bool
	nsfw                  bool
	results               []RemoteMod
	selected, top         int
	imageMode             int
	autoMode              bool // DLMOD_IMAGES did not choose, so the terminal's answer may upgrade blocks
	fullPreview           bool
	pictures              map[string]image.Image
	failed                map[string]string
	pending               string
	cellWidth, cellHeight int
	payloadKey, payload   string
}

func (b *browser) filter() filter {
	f := filter{text: string(b.query), nsfw: b.nsfw}
	if b.category >= 0 {
		f.category = b.categories[b.category]
	}
	if b.hero >= 0 {
		f.hero = b.heroes[b.hero]
	}
	return f
}

func (b *browser) current() (RemoteMod, bool) {
	if b.selected < 0 || b.selected >= len(b.results) {
		return RemoteMod{}, false
	}
	return b.results[b.selected], true
}

func (b *browser) refilter() {
	b.results = search(b.mods, b.filter())
	b.selected, b.top = 0, 0
}

// openBrowser shows the catalog, downloading it the first time (about 15 MB, kept for six hours).
func (t *tui) openBrowser(refresh bool) {
	if t.browse == nil || refresh {
		t.busy = "Downloading the list of mods from deadlockmods.app (about 15 MB)…"
		t.render()
		mods, _, err := loadCatalog(t.game, refresh)
		t.busy = ""
		if err != nil {
			t.note(toneBad, err.Error())
			return
		}
		b := &browser{mods: mods, category: -1, hero: -1, imageMode: detectImageMode(os.Getenv),
			autoMode: os.Getenv("DLMOD_IMAGES") == "",
			pictures: map[string]image.Image{}, failed: map[string]string{}, cellWidth: 10, cellHeight: 20}
		if t.browse != nil {
			b.imageMode, b.autoMode, b.query, b.nsfw = t.browse.imageMode, t.browse.autoMode, t.browse.query, t.browse.nsfw
		} else if b.autoMode && b.imageMode == imagesBlocks {
			// Ask which graphics the terminal draws (DA1). Windows Terminal 1.22+ answers with 4,
			// sixel, and previews then switch to full resolution.
			t.out.WriteString("\x1b[c")
			t.out.Flush()
		}
		b.categories, b.heroes = facets(mods)
		for i, category := range b.categories {
			if category == "Skins" {
				b.category = i // skins are what most people browse for
			}
		}
		t.browse = b
		b.refilter()
	}
	t.mode = modeBrowse
	t.notes = nil
	if t.browse.imageMode == imagesSixel {
		t.out.WriteString("\x1b[16t") // ask the terminal for its cell size in pixels
		t.out.Flush()
	}
	t.requestPreview()
}

var (
	cellSizeReply   = regexp.MustCompile(`\x1b\[6;(\d+);(\d+)t`)
	attributesReply = regexp.MustCompile(`\x1b\[\?([\d;]*)c`)
)

// takeCellSize consumes the terminal's answers to dlmod's questions (cell size in pixels, and
// the device attributes that tell whether it draws sixel graphics), so they are not read as keys.
func (t *tui) takeCellSize(data []byte) []byte {
	if match := attributesReply.FindSubmatchIndex(data); match != nil {
		attributes := ";" + string(data[match[2]:match[3]]) + ";"
		if b := t.browse; b != nil && b.autoMode && b.imageMode == imagesBlocks && strings.Contains(attributes, ";4;") {
			b.imageMode, b.payloadKey = imagesSixel, ""
			t.note(toneInfo, "This terminal draws sixel graphics, so previews are shown at full resolution (m changes it).")
			t.out.WriteString("\x1b[16t")
			t.out.Flush()
		}
		data = append(append([]byte(nil), data[:match[0]]...), data[match[1]:]...)
	}
	match := cellSizeReply.FindSubmatchIndex(data)
	if match == nil {
		return data
	}
	if t.browse != nil {
		height, _ := strconv.Atoi(string(data[match[2]:match[3]]))
		width, _ := strconv.Atoi(string(data[match[4]:match[5]]))
		if width > 0 && height > 0 {
			t.browse.cellWidth, t.browse.cellHeight = width, height
			t.browse.payloadKey = ""
		}
	}
	return append(append([]byte(nil), data[:match[0]]...), data[match[1]:]...)
}

func (t *tui) requestPreview() {
	b := t.browse
	mod, ok := b.current()
	if !ok || len(mod.Images) == 0 || b.imageMode == imagesOff {
		return
	}
	address := mod.Images[0]
	if b.pictures[address] != nil || b.failed[address] != "" || b.pending == address {
		return
	}
	if len(b.pictures) > 48 {
		b.pictures = map[string]image.Image{} // previews are cached on disk; keep memory small
	}
	b.pending = address
	load := t.loadImage
	if load == nil {
		load = func(address string) (image.Image, error) { return loadPreview(t.game, address) }
	}
	if t.previewDone == nil {
		picture, err := load(address)
		t.receivePreview(previewResult{address, picture, err})
		return
	}
	done := t.previewDone
	go func() {
		picture, err := load(address)
		done <- previewResult{address, picture, err}
	}()
}

func (t *tui) receivePreview(result previewResult) {
	if t.browse == nil {
		return
	}
	if result.err != nil {
		t.browse.failed[result.url] = result.err.Error()
	} else {
		t.browse.pictures[result.url] = result.picture
	}
	if t.browse.pending == result.url {
		t.browse.pending = ""
	}
}

func (t *tui) pressBrowse(k key) bool {
	b := t.browse
	if b.typing {
		switch k.name {
		case "char":
			b.query = append(b.query, k.r)
		case "backspace":
			if len(b.query) > 0 {
				b.query = b.query[:len(b.query)-1]
			}
		case "ctrl+u":
			b.query = nil
		case "enter", "esc", "up", "down":
			b.typing = false
		}
		b.refilter()
		t.requestPreview()
		return false
	}
	move := func(delta int) {
		b.selected = max(0, min(len(b.results)-1, b.selected+delta))
		t.requestPreview()
	}
	cycle := func(index, count, delta int) int {
		// -1 (all) comes before the first entry.
		return (index+1+delta+count+1)%(count+1) - 1
	}
	switch k.name {
	case "up":
		move(-1)
	case "down":
		move(1)
	case "pgup":
		move(-10)
	case "pgdn":
		move(10)
	case "home":
		move(-len(b.results))
	case "end":
		move(len(b.results))
	case "left", "right":
		delta := 1
		if k.name == "left" {
			delta = -1
		}
		b.hero = cycle(b.hero, len(b.heroes), delta)
		b.refilter()
		t.requestPreview()
	case "enter":
		if mod, ok := b.current(); ok {
			t.installFromBrowser(mod)
		}
	case "esc", "ctrl+c":
		t.leaveBrowser()
	case "char":
		switch k.r {
		case 'q', 'Q':
			t.leaveBrowser()
		case 'k':
			move(-1)
		case 'j':
			move(1)
		case '/', 's':
			b.typing = true
		case 'c', 'C':
			delta := 1
			if k.r == 'C' {
				delta = -1
			}
			b.category = cycle(b.category, len(b.categories), delta)
			b.refilter()
			t.requestPreview()
		case 'v', ' ':
			b.fullPreview = !b.fullPreview
			b.payloadKey = ""
			t.clearPicture()
		case 'm':
			b.autoMode = false
			b.imageMode = (b.imageMode + 1) % len(imageModeNames)
			b.payloadKey = ""
			t.clearPicture()
			t.note(toneInfo, "Previews: "+imageModeNames[b.imageMode]+
				" (set DLMOD_IMAGES="+imageModeNames[b.imageMode]+" to start with it)")
			if b.imageMode == imagesSixel {
				t.out.WriteString("\x1b[16t")
				t.out.Flush()
			}
			t.requestPreview()
		case 'n':
			b.nsfw = !b.nsfw
			b.refilter()
			t.requestPreview()
		case 'R':
			t.openBrowser(true)
		case '?', 'h':
			t.mode = modeHelp
		}
	}
	return false
}

func (t *tui) leaveBrowser() {
	t.clearPicture()
	t.mode = modeList
	t.notes = nil
}

// clearPicture removes a kitty preview, which stays on screen until deleted.
func (t *tui) clearPicture() {
	if t.browse != nil && t.browse.imageMode == imagesKitty {
		t.out.WriteString("\x1b_Ga=d,d=i,i=7,q=2\x1b\\")
		t.out.Flush()
	}
	t.imageAt = imageArea{}
}

func (t *tui) installFromBrowser(mod RemoteMod) {
	if !t.confirm(fmt.Sprintf("Download and install %s by %s?", mod.Name, mod.Author), true) {
		return
	}
	t.act(func() error {
		t.busy = "Looking up the files of " + mod.Name + "…"
		t.render()
		return t.store.installRemote(t, mod)
	})
	t.mode = modeBrowse
}

// drawBrowse lays out the filters, the result list on the left and the selected mod on the right.
func (t *tui) drawBrowse(c *canvas, room int) {
	p, b := t.p, t.browse
	all := func(index int, names []string, none string) string {
		if index < 0 {
			return none
		}
		return names[index]
	}
	search := string(b.query)
	searchStyle := ""
	if b.typing {
		search += "▏"
		searchStyle = p.bold
	} else if search == "" {
		search, searchStyle = "(press /)", p.dim
	}
	c.line(seg{" Category ", p.dim}, seg{all(b.category, b.categories, "All"), p.bold + p.cyan},
		seg{"   Hero ", p.dim}, seg{all(b.hero, b.heroes, "All heroes"), p.bold + p.cyan},
		seg{"   Search ", p.dim}, seg{search, searchStyle},
		seg{fmt.Sprintf("   %s", plural(len(b.results), "%d mod", "%d mods")), p.dim})
	c.line(seg{strings.Repeat("─", c.width), p.dim})
	rows := max(room-2, 1)

	left := min(max(c.width*46/100, 30), c.width-24)
	right := c.width - left - 3
	if b.selected < b.top {
		b.top = b.selected
	}
	if b.selected >= b.top+rows {
		b.top = b.selected - rows + 1
	}
	if b.fullPreview {
		// The whole area shows the picture: many more cells, so blocks are much sharper.
		for i, line := range t.browseDetails(c.width-2, rows) {
			if i < rows {
				c.line(append([]seg{{" ", ""}}, line...)...)
			}
		}
		if t.imageAt.url != "" {
			t.imageAt.row, t.imageAt.col = 2, 1
		}
		return
	}
	details := t.browseDetails(right, rows)
	if t.imageAt.url != "" {
		t.imageAt.row, t.imageAt.col = 2, left+3 // below the filter line and the rule, right of the list
	}
	for i := 0; i < rows; i++ {
		var line []seg
		index := b.top + i
		if index < len(b.results) {
			mod := b.results[index]
			style, marker := "", "  "
			if index == b.selected {
				style, marker = p.row+p.bold, "› "
			}
			count := formatCount(mod.Downloads)
			hero := truncate(mod.Hero, 11)
			nameWidth := left - 2 - 12 - 7
			line = append(line, seg{marker + pad(mod.Name, nameWidth) + " " + pad(hero, 11) + " " +
				fmt.Sprintf("%6s", count), style})
		} else if i == 0 {
			line = append(line, seg{pad("  No mods match. Change the hero (←/→), category (c) or search (/).", left), p.dim})
		} else {
			line = append(line, seg{strings.Repeat(" ", left), ""})
		}
		line = append(line, seg{" │ ", p.dim})
		if i < len(details) {
			line = append(line, details[i]...)
		}
		c.line(line...)
	}
}

// browseDetails is the right-hand pane: the preview, then the mod's facts.
func (t *tui) browseDetails(width, rows int) [][]seg {
	p, b := t.p, t.browse
	mod, ok := b.current()
	if !ok || width < 12 {
		t.imageAt = imageArea{}
		return nil
	}
	var lines [][]seg
	imageRows := min(rows-7, width/2+1)
	if b.fullPreview {
		imageRows = rows - 2 // only the name stays below the picture
	}
	address := ""
	if len(mod.Images) > 0 {
		address = mod.Images[0]
	}
	t.imageAt = imageArea{}
	picture := b.pictures[address]
	switch {
	case b.imageMode == imagesOff || imageRows < 4:
	case address == "":
		lines = append(lines, []seg{{"no preview", p.dim}})
	case b.failed[address] != "":
		lines = append(lines, []seg{{truncate(b.failed[address], width), p.dim}})
	case picture == nil:
		lines = append(lines, []seg{{"loading preview…", p.dim}})
		for len(lines) < imageRows {
			lines = append(lines, nil)
		}
	case b.imageMode == imagesBlocks:
		lines = append(lines, blockLines(picture, width, imageRows)...)
	default:
		// sixel and kitty draw over these blank rows once the text is on screen.
		cols, rowsUsed := fit(picture.Bounds(), width*b.cellWidth, imageRows*b.cellHeight)
		cols, rowsUsed = max(1, (cols+b.cellWidth-1)/b.cellWidth), max(1, (rowsUsed+b.cellHeight-1)/b.cellHeight)
		t.imageAt = imageArea{cols: min(cols, width), rows: min(rowsUsed, imageRows), url: address}
		for i := 0; i < t.imageAt.rows; i++ {
			lines = append(lines, nil)
		}
		lines = append(lines, []seg{{"preview: " + imageModeNames[b.imageMode], p.dim}})
	}
	if b.fullPreview {
		return append(lines, []seg{{truncate(mod.Name+" · "+mod.Author, width), p.bold}})
	}
	if len(lines) > 0 {
		lines = append(lines, nil)
	}
	lines = append(lines, []seg{{truncate(mod.Name, width), p.bold}})
	facts := mod.Category
	if mod.Hero != "" {
		facts = mod.Hero + " · " + facts
	}
	lines = append(lines, []seg{{truncate(facts+" · by "+mod.Author, width), ""}})
	updated := mod.Updated
	if len(updated) >= 10 {
		updated = updated[:10]
	}
	lines = append(lines, []seg{{truncate(fmt.Sprintf("%s downloads · %s likes · updated %s",
		formatCount(mod.Downloads), formatCount(mod.Likes), updated), width), p.dim}})
	if mod.NSFW {
		lines = append(lines, []seg{{"NSFW", p.red}})
	}
	lines = append(lines, []seg{{truncate(mod.URL, width), p.dim}})
	return lines
}

// drawPicture writes a sixel or kitty preview over its blank rows, after the text frame.
func (t *tui) drawPicture() {
	b := t.browse
	area := t.imageAt
	if b == nil || area.url == "" || (b.imageMode != imagesSixel && b.imageMode != imagesKitty) {
		return
	}
	picture := b.pictures[area.url]
	if picture == nil {
		return
	}
	key := fmt.Sprintf("%s|%d|%d|%d|%d|%d", area.url, b.imageMode, area.cols, area.rows, b.cellWidth, b.cellHeight)
	if key != b.payloadKey {
		width, height := fit(picture.Bounds(), area.cols*b.cellWidth, area.rows*b.cellHeight)
		if b.imageMode == imagesSixel {
			b.payload = encodeSixel(picture, width, height-height%6)
		} else {
			b.payload = encodeKitty(picture, width, height, area.cols, area.rows)
		}
		b.payloadKey = key
	}
	fmt.Fprintf(t.out, "\x1b7\x1b[%d;%dH%s\x1b8", area.row+1, area.col+1, b.payload)
}

func (t *tui) browseHints() [][2]string {
	if t.browse != nil && t.browse.typing {
		return [][2]string{{"type", "search"}, {"Enter", "done"}, {"Ctrl+U", "clear"}}
	}
	return [][2]string{{"↑↓", "select"}, {"←→", "hero"}, {"c", "category"}, {"/", "search"}, {"Enter", "install"},
		{"v", "big preview"}, {"m", "preview mode"}, {"n", "NSFW"}, {"R", "refresh"}, {"Esc", "back"}}
}
