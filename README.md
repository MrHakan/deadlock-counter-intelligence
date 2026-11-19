# Deadlock Counter Intelligence

An experimental Panorama HUD for Deadlock Mod Manager. It does two things:

- **Counter items for the enemy lineup.** Open the scoreboard or shop to see the three counter items that answer the most enemy heroes, with soul price, item category, and the enemy heroes each item counters.
- **Enemy counter-purchase alerts.** When an enemy buys an item that is on the counter list of a hero on your team, a banner names the buyer, the item, and the allies it counters. It stays up for 8 seconds and turns red when the item counters your own hero. Each enemy purchase alerts once per match.

## Reference counter table

Suggestions come from `src/counters.json`: five counter items for each of 42 heroes, taken from the *Counter Eşyalar: kime karşı ne alınır?* series. Each item counts once for every enemy hero whose list includes it. Ties go to the item listed earlier on those heroes' lists, then to the cheaper item. Pocket and Rat King are not in the series, so they add nothing to suggestions or alerts.

To change the reference, edit the item class names in `src/counters.json`. The tests check that every entry exists in the catalog.

## How it reads the game

The suggestion panel sits above the minimap on the scoreboard and in the lower-left corner of the shop. Both teams' heroes come from the top bar, and the card marked as the local player identifies your hero. Enemy purchases come from the shop's recent-purchase list, which the mod reads every half second while the shop is open, so alerts appear when you open the shop. Purchases by allied heroes are ignored. When the enemy lineup changes, the mod treats it as a new match and clears its alert history.

The recommendation engine, HUD state changes, and archive format are covered by local tests. Loading the VPK in Deadlock Mod Manager and displaying it in Deadlock still require in-game verification. The HUD does not track full inventories or item sales.

## Install

1. Download the HUD ZIP from [GitHub Releases](https://github.com/MrHakan/deadlock-counter-intelligence/releases).
2. Close Deadlock. In Deadlock Mod Manager v2.3.0, open **Mods Library → Add Local Mod** and import the ZIP.
3. Enable the mod and launch Deadlock through Mod Manager.

Use Mod Manager to disable or remove the mod. Other mods that replace `panorama/layout/hud.vxml_c` can conflict with this package.

## Build the VPK

Building requires Python 3.9+, a Steam installation of Deadlock, and [Source2Viewer-CLI](https://github.com/ValveResourceFormat/ValveResourceFormat/releases). The build reads the base HUD layout from the installed game.

```powershell
py -3 tools/build.py --deadlock-dir "C:\Program Files (x86)\Steam\steamapps\common\Deadlock" --vrf "C:\Tools\Source2Viewer-CLI.exe"
```

If you already have the extracted stock layout, use `--hud-xml path/to/hud.xml`. The build produces `dist/deadlock-counter-intelligence-experimental.zip` with one `pak01_dir.vpk` inside.

## Catalog and tests

The bundled catalog targets Deadlock client build **6861** and contains 173 shop items, with price and shop category, and 44 hero records. The HUD displays English names. Turkish aliases help match item and hero names read from the game client. When targeting a new build, refresh the catalog and use the stock HUD from that build.

```sh
python tools/update_catalog.py 6861
node --test test/intelligence.test.js
python -m unittest discover -s test -v
```

Ambiguous labels are skipped. During a match, the mod works from the visible HUD, the bundled catalog, and the counter table.

For in-game verification, use a clean Mod Manager profile and a private lobby. Check that the suggestions match the enemy lineup, that an enemy buying an item from an ally's counter list (for example Healbane against Infernus) shows the banner, that an allied purchase is ignored, and that disabling the mod restores the stock HUD.

## References

- [Deadlock Mod Manager local mod guide](https://docs.deadlockmods.app/using-mod-manager/library#add-local-mods) and [Live bridge source](https://github.com/deadlock-mod-manager/deadlock-mod-manager/blob/v2.3.0/apps/desktop/src-tauri/src/live_bridge/dmm_live_bridge.js)
- [Deadlock API](https://api.deadlock-api.com/docs) and [GameTracking-Deadlock HUD layout](https://github.com/SteamTracking/GameTracking-Deadlock/blob/master/game/citadel/pak01_dir/panorama/layout/hud.xml)
- [Threat HUD shop layout](https://github.com/antonborndev/deadlock-threat-hud/blob/9fc4cbfd7290c834eb81490ecc004c578ae4fbba/threathud/panorama/layout/citadel_hud_hero_shop.xml) and [BiggerIdleMap build example](https://github.com/regulad/BiggerIdleMap)
- `third_party/panorama.py` comes from the [MIT-licensed Panorama compiler in BiggerIdleMap](https://github.com/regulad/BiggerIdleMap/blob/d54483205cbc01a3f496d536e338f58b919c2caa/panorama.py). The original copyright and license notices are included in that file.
