package main

import (
	"strings"
	"testing"
)

const sampleHeroText = "\xef\xbb\xbf\"lang\"\r\n{\r\n\t\"Language\"\t\t\"english\"\r\n\t\"Tokens\"\r\n\t{\r\n" +
	"\t\t//-----------------------------------------------------------------------------\r\n" +
	"\t\t// Wraith\r\n" +
	"\t\t\"Citadel_Wraith_HeroTag_1\" \"Duelist\"\r\n" +
	"\t\t\"citadel_ability_projectmind\"\t\t\t\"Project Mind\"\r\n" +
	"\t\t\"citadel_ability_projectmind_desc\"\t\"<span class=\\\"highlight\\\">Teleport</span> to the targeted location.\"\r\n" +
	"\t\t//\"citadel_ability_old\" \"Old\"\r\n" +
	"\t\t//-----------------------------------------------------------------------------\r\n" +
	"\t\t// Haze\r\n" +
	"\t\t\"citadel_ability_sleep_dagger\"\t\"Sleep Dagger\"\r\n" +
	"\t}\r\n}\r\n"

func TestLocalizationTokens(t *testing.T) {
	tokens := parseTokens(sampleHeroText)
	mine := heroTokens(tokens, "wraith")
	if len(mine) != 3 || mine[1].Key != "citadel_ability_projectmind" || mine[1].Section != "Wraith" ||
		mine[2].Value != `<span class="highlight">Teleport</span> to the targeted location.` {
		t.Fatalf("Wraith's lines: %+v", mine)
	}
	if haze := heroTokens(tokens, "haze"); len(haze) != 1 || haze[0].Value != "Sleep Dagger" {
		t.Fatalf("Haze's lines: %+v", haze)
	}
	edited := applyEdits(sampleHeroText, map[string]string{
		"citadel_ability_projectmind":      `Blink "fast"`,
		"citadel_ability_projectmind_desc": "Two\nlines",
	})
	if !strings.Contains(edited, `"citadel_ability_projectmind"			"Blink \"fast\""`) ||
		!strings.Contains(edited, `"Two\nlines"`) || !strings.Contains(edited, `"Sleep Dagger"`) || !strings.HasPrefix(edited, "\xef\xbb\xbf") {
		t.Fatalf("edits written into the file as it is:\n%s", edited)
	}
	again := heroTokens(parseTokens(edited), "wraith")
	if again[1].Value != `Blink "fast"` || again[2].Value != "Two\nlines" {
		t.Fatalf("edited lines read back: %+v", again)
	}
}

// The Text tab lists a hero's lines in a language, saves edits as a mod laid over the game's
// file, and an edit back to the game's text drops it.
func TestStudioText(t *testing.T) {
	game, post, _ := studioServer(t, map[string][]byte{})
	game.write("game/citadel/"+localizationFile("citadel_heroes", "english"), []byte(sampleHeroText))
	game.write("game/citadel/"+localizationFile("citadel_heroes", "turkish"), []byte(strings.ReplaceAll(sampleHeroText, "Project Mind", "Zihinsel Seyahat")))
	type listing struct {
		Languages []string
		Language  string
		Entries   []struct {
			File, Key, Text, Original string
			Edited                    bool
		}
	}
	list := func(language string) listing {
		var out listing
		post("text", "application/json", []byte(`{"hero":"wraith","language":"`+language+`"}`), &out)
		return out
	}
	got := list("turkish")
	if len(got.Languages) != 2 || got.Language != "turkish" || len(got.Entries) != 3 || got.Entries[1].Text != "Zihinsel Seyahat" {
		t.Fatalf("Wraith's Turkish lines: %+v", got)
	}
	file := localizationFile("citadel_heroes", "turkish")
	var result struct{ Messages []string }
	if code := post("text/save", "application/json", []byte(`{"edits":[{"file":"`+file+`","key":"citadel_ability_projectmind","text":"Işınlan"}]}`), &result); code != 200 {
		t.Fatalf("save: %d %v", code, result.Messages)
	}
	if got = list("turkish"); !got.Entries[1].Edited || got.Entries[1].Text != "Işınlan" || got.Entries[1].Original != "Zihinsel Seyahat" {
		t.Fatalf("the edit shows: %+v", got.Entries[1])
	}
	vpk := game.installed(t, customTextName)
	data, err := vpk.Read(file)
	if err != nil || !strings.Contains(string(data), `"Işınlan"`) || !strings.Contains(string(data), `"Sleep Dagger"`) {
		t.Fatalf("the mod holds the whole file with the edit: %v\n%s", err, data)
	}
	post("text/save", "application/json", []byte(`{"edits":[{"file":"`+file+`","key":"citadel_ability_projectmind","text":"Zihinsel Seyahat"}]}`), &result)
	if got = list("turkish"); got.Entries[1].Edited || game.hasMod(t, customTextName) {
		t.Fatalf("back to the game's text, the edit and the mod go: %+v", got.Entries[1])
	}
}
