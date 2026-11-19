// dlmod installs Deadlock mods without a mod manager: it copies .vpk mods into
// game/citadel/addons under free pakNN_dir.vpk names and makes gameinfo.gi load that folder.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
)

var version = "dev"

const usage = `dlmod - installs Deadlock mods without a mod manager

Usage:
  dlmod                        open the menu
  dlmod <mod.zip|mod.vpk>...   install mods (on Windows, drag files onto dlmod.exe)
  dlmod install <file> [-name NAME]
  dlmod list                   show mods, conflicts and whether the game loads mods
  dlmod off <mod>              keep a mod but stop loading it
  dlmod on <mod>               load a switched-off mod again
  dlmod remove <mod>           delete a mod
  dlmod patch                  make the game load mods again after a game update
  dlmod unpatch                undo dlmod's gameinfo.gi change
  dlmod play                   patch if needed, then start Deadlock through Steam

<mod> is the number shown by "dlmod list", a slot such as pak03, or part of the name.

Options:
  -game DIR   the Deadlock folder (found through Steam when omitted; DEADLOCK_DIR also works)
`

type options struct {
	game, name string
	help       bool
}

func parseArgs(argv []string) ([]string, options, error) {
	var opts options
	var rest []string
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			rest = append(rest, arg)
			continue
		}
		key, value, hasValue := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		switch key {
		case "game", "name":
			if !hasValue {
				if i+1 >= len(argv) {
					return nil, opts, fmt.Errorf("%s needs a value", arg)
				}
				i++
				value = argv[i]
			}
			if key == "game" {
				opts.game = value
			} else {
				opts.name = value
			}
		case "h", "help":
			opts.help = true
		case "v", "version":
			fmt.Println("dlmod", version)
			os.Exit(0)
		default:
			return nil, opts, fmt.Errorf("unknown option %s", arg)
		}
	}
	return rest, opts, nil
}

var commands = map[string]bool{
	"install": true, "list": true, "status": true, "on": true, "enable": true, "off": true,
	"disable": true, "remove": true, "uninstall": true, "patch": true, "repair": true,
	"unpatch": true, "play": true, "help": true,
}

// UI reads answers from the console; without a console every question takes its default.
type UI struct {
	in          *bufio.Reader
	out         io.Writer
	interactive bool
	eof         bool
}

func newUI() *UI {
	info, err := os.Stdin.Stat()
	interactive := err == nil && info.Mode()&os.ModeCharDevice != 0
	return &UI{in: bufio.NewReader(os.Stdin), out: os.Stdout, interactive: interactive}
}

func (u *UI) say(format string, args ...any) { fmt.Fprintf(u.out, format+"\n", args...) }

func (u *UI) ask(question string) string {
	fmt.Fprint(u.out, question)
	line, err := u.in.ReadString('\n')
	if err != nil {
		u.eof = true
	}
	return cleanInput(line)
}

// cleanInput removes the quotes Windows adds around dragged paths.
func cleanInput(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimSpace(strings.TrimPrefix(text, "& "))
	return strings.Trim(text, `"'`)
}

func (u *UI) confirm(question string, fallback bool) bool {
	if !u.interactive {
		return fallback
	}
	hint := " [y/N] "
	if fallback {
		hint = " [Y/n] "
	}
	switch strings.ToLower(u.ask(question + hint)) {
	case "":
		return fallback
	case "y", "yes", "e", "evet":
		return true
	default:
		return false
	}
}

func main() {
	args, opts, err := parseArgs(os.Args[1:])
	ui := newUI()
	code := 0
	if err != nil {
		ui.say("Error: %v\n\n%s", err, usage)
		code = 2
	} else {
		code = run(ui, args, opts)
	}
	// A window opened by dragging files onto dlmod.exe, or by double-clicking it when something
	// went wrong, would close before the message can be read.
	dragged := len(args) > 0 && !commands[args[0]]
	if runtime.GOOS == "windows" && ui.interactive && !ui.eof && (dragged || (len(args) == 0 && code != 0)) {
		ui.ask("\nPress Enter to close.")
	}
	os.Exit(code)
}

func run(ui *UI, args []string, opts options) int {
	if opts.help || (len(args) > 0 && args[0] == "help") {
		fmt.Fprint(ui.out, usage)
		return 0
	}
	game, err := findGame(opts.game)
	for err != nil && ui.interactive && opts.game == "" && (len(args) == 0 || !commands[args[0]]) {
		ui.say("%v", err)
		folder := ui.ask("Paste the Deadlock folder (Steam > Deadlock > Manage > Browse local files), or press Enter to quit: ")
		if folder == "" {
			return 1
		}
		game, err = findGame(folder)
	}
	if err != nil {
		ui.say("Error: %v", err)
		return 1
	}
	if len(args) == 0 {
		return menu(ui, game)
	}
	command, rest := args[0], args[1:]
	if !commands[command] {
		command, rest = "install", args
	}
	if err := execute(ui, game, command, rest, opts); err != nil {
		ui.say("Error: %v", err)
		return 1
	}
	return 0
}

func execute(ui *UI, game Game, command string, args []string, opts options) error {
	store, err := openStore(game)
	if err != nil {
		return err
	}
	changesFiles := command != "list" && command != "status"
	if changesFiles && gameRunning() {
		return errors.New("Deadlock is running; close it first")
	}
	switch command {
	case "list", "status":
		return showStatus(ui, game, store)
	case "install":
		if len(args) == 0 {
			return errors.New("name the .zip or .vpk file to install")
		}
		for _, file := range args {
			if err := store.install(ui, file, opts.name); err != nil {
				return err
			}
		}
		return ensurePatched(ui, game)
	case "on", "enable", "off", "disable", "remove", "uninstall":
		if len(args) != 1 {
			return fmt.Errorf("name one mod, for example: dlmod %s 2", command)
		}
		entries, err := store.entries()
		if err != nil {
			return err
		}
		entry, err := resolve(entries, args[0])
		if err != nil {
			return err
		}
		return change(ui, store, entry, command)
	case "patch", "repair":
		return ensurePatched(ui, game)
	case "unpatch":
		changed, err := game.unpatch()
		if err == nil {
			if changed {
				ui.say("gameinfo.gi is back to normal; the game no longer loads mods.")
			} else {
				ui.say("gameinfo.gi has no dlmod changes.")
			}
		}
		return err
	case "play":
		if err := ensurePatched(ui, game); err != nil {
			return err
		}
		ui.say("Starting Deadlock through Steam...")
		return launchGame()
	}
	return fmt.Errorf("unknown command %s", command)
}

func change(ui *UI, store *Store, entry *Entry, command string) error {
	name := entry.Name()
	switch command {
	case "on", "enable":
		if err := store.enable(entry); err != nil {
			return err
		}
		ui.say("'%s' is on (pak%02d).", name, entry.Mod.Slot)
		return ensurePatched(ui, store.game)
	case "off", "disable":
		if err := store.disable(entry); err != nil {
			return err
		}
		ui.say("'%s' is off. It stays in dlmod's folder until you switch it on or remove it.", name)
	default:
		if err := store.remove(entry); err != nil {
			return err
		}
		ui.say("Removed '%s'.", name)
	}
	return nil
}

func ensurePatched(ui *UI, game Game) error {
	changed, err := game.patch()
	if err != nil {
		return err
	}
	if changed {
		ui.say("Updated gameinfo.gi so the game loads mods from game/citadel/addons " +
			"(previous copy: game/dlmod/gameinfo.gi.bak).")
	} else {
		ui.say("The game already loads mods from game/citadel/addons.")
	}
	return nil
}

func showStatus(ui *UI, game Game, store *Store) error {
	build := ""
	if v := game.Version(); v > 0 {
		build = fmt.Sprintf(" (build %d)", v)
	}
	ui.say("Deadlock: %s%s", game.Root, build)
	if _, state, err := game.readGameinfo(); err != nil {
		ui.say("Mods: unknown, %v", err)
	} else if !state.Loaded {
		ui.say("Mods: NOT LOADED. gameinfo.gi does not include citadel/addons; game updates and " +
			"\"Verify integrity\" reset it. Choose Repair in the menu, or run: dlmod patch")
	} else if len(state.Missing) > 0 {
		ui.say("Mods: loaded, but gameinfo.gi lacks %s, so settings may be saved in addons. "+
			"Choose Repair in the menu, or run: dlmod patch", strings.Join(state.Missing, ", "))
	} else {
		ui.say("Mods: loaded from game/citadel/addons")
	}

	entries, err := store.entries()
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		ui.say("\nNo mods installed.")
		return nil
	}
	ui.say("\n  #  slot   mod")
	current := game.Version()
	var warnings []string
	for i, entry := range entries {
		slot := fmt.Sprintf("pak%02d", entry.Slot)
		if !entry.Enabled() {
			slot = "off  "
		}
		detail := ""
		switch {
		case entry.Err != nil:
			detail = "  (unreadable: " + entry.Err.Error() + ")"
		case len(entry.Paths) > 0:
			detail = "  changes " + describePaths(entry.Paths)
		}
		ui.say("%3d  %s  %s%s", i+1, slot, entry.Name(), detail)
		if mod := entry.Mod; mod != nil && entry.Enabled() && mod.GameVersion > 0 && current > 0 &&
			mod.GameVersion != current && changesInterface(entry.Paths) {
			warnings = append(warnings, fmt.Sprintf("'%s' was installed on build %d and the game is now on %d. "+
				"Interface mods can stop the game from starting after an update; if that happens, switch it off "+
				"and look for a newer version.", entry.Name(), mod.GameVersion, current))
		}
	}
	for _, clash := range conflicts(entries) {
		ui.say("\nConflict: pak%02d '%s' and pak%02d '%s' both change %s. Only one of them can take effect.",
			clash.A.Slot, clash.A.Name(), clash.B.Slot, clash.B.Name(), describePaths(clash.Paths))
	}
	for _, warning := range warnings {
		ui.say("\nWarning: %s", warning)
	}
	return nil
}

func changesInterface(paths []string) bool {
	for _, path := range paths {
		if strings.HasPrefix(path, "panorama/") {
			return true
		}
	}
	return false
}

func menu(ui *UI, game Game) int {
	for {
		store, err := openStore(game)
		if err != nil {
			ui.say("Error: %v", err)
			return 1
		}
		ui.say("")
		if err := showStatus(ui, game, store); err != nil {
			ui.say("Error: %v", err)
		}
		ui.say("\n  1  Install a mod (.zip or .vpk)\n  2  Switch a mod on or off\n  3  Remove a mod\n" +
			"  4  Repair after a game update (patch gameinfo.gi)\n  5  Start Deadlock\n  0  Exit")
		choice := ui.ask("\nChoose: ")
		if ui.eof {
			return 0
		}
		var err2 error
		switch choice {
		case "1":
			if file := ui.ask("Drag the mod file into this window, then press Enter: "); file != "" {
				err2 = execute(ui, game, "install", []string{file}, options{})
			}
		case "2", "3":
			entries, err := store.entries()
			if err != nil {
				err2 = err
				break
			}
			if len(entries) == 0 {
				ui.say("No mods installed.")
				break
			}
			answer := ui.ask("Mod number: ")
			if answer == "" {
				break
			}
			entry, err := resolve(entries, answer)
			if err != nil {
				err2 = err
				break
			}
			command := "remove"
			if choice == "2" {
				command = "off"
				if !entry.Enabled() {
					command = "on"
				}
			} else if !ui.confirm(fmt.Sprintf("Delete '%s'?", entry.Name()), false) {
				break
			}
			if gameRunning() {
				err2 = errors.New("Deadlock is running; close it first")
				break
			}
			err2 = change(ui, store, entry, command)
		case "4":
			err2 = execute(ui, game, "patch", nil, options{})
		case "5":
			err2 = execute(ui, game, "play", nil, options{})
		case "0", "q", "exit":
			return 0
		}
		if err2 != nil {
			ui.say("Error: %v", err2)
		}
	}
}
