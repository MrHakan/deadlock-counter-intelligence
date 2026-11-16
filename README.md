# Deadlock Counter Intelligence

An experimental Panorama HUD for Deadlock Mod Manager. Open the scoreboard or shop to see up to three counter-item suggestions with their soul prices, the effect that makes each item useful, and the enemy hero or purchase that prompted it.

The panel sits above the minimap on the scoreboard and in the lower-left corner of the shop. It uses Deadlock's category and soul icons. While the shop is open, the mod checks its visible recent-purchase list every half second and matches those entries to the enemy lineup. When purchase evidence is unavailable, suggestions use the visible enemy lineup.

The recommendation engine, HUD state changes, and archive format are covered by local tests. Loading the VPK in Deadlock Mod Manager and displaying it in Deadlock still require in-game verification. Purchases are observations from the shop list. The HUD does not track full inventories or item sales.

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

The bundled catalog targets Deadlock client build **6861** and contains 173 shop items and 44 hero records. The HUD displays English names. Localized aliases help match item and hero names read from the game client. When targeting a new build, refresh the catalog and use the stock HUD from that build.

```sh
python tools/update_catalog.py 6861
node --test test/intelligence.test.js
python -m unittest discover -s test -v
```

The initial rules cover healing, bullet damage, spirit damage, and airborne targets. Ambiguous labels are skipped. Shop observations expire when the shop closes. During a match, the mod works from the visible HUD and the bundled catalog.

For in-game verification, use a clean Mod Manager profile and a private lobby. Check that a visible enemy purchase changes a relevant recommendation, an allied purchase is ignored, and disabling the mod restores the stock HUD.

## References

- [Deadlock Mod Manager local mod guide](https://docs.deadlockmods.app/using-mod-manager/library#add-local-mods) and [Live bridge source](https://github.com/deadlock-mod-manager/deadlock-mod-manager/blob/v2.3.0/apps/desktop/src-tauri/src/live_bridge/dmm_live_bridge.js)
- [Deadlock API](https://api.deadlock-api.com/docs) and [GameTracking-Deadlock HUD layout](https://github.com/SteamTracking/GameTracking-Deadlock/blob/master/game/citadel/pak01_dir/panorama/layout/hud.xml)
- [Threat HUD shop layout](https://github.com/antonborndev/deadlock-threat-hud/blob/9fc4cbfd7290c834eb81490ecc004c578ae4fbba/threathud/panorama/layout/citadel_hud_hero_shop.xml) and [BiggerIdleMap build example](https://github.com/regulad/BiggerIdleMap)
- `third_party/panorama.py` comes from the [MIT-licensed Panorama compiler in BiggerIdleMap](https://github.com/regulad/BiggerIdleMap/blob/d54483205cbc01a3f496d536e338f58b919c2caa/panorama.py). The original copyright and license notices are included in that file.
