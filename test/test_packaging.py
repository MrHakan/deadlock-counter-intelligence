import json
from pathlib import Path
from struct import unpack_from
import tempfile
import unittest
from xml.etree import ElementTree as ET
from zipfile import ZipFile
from zlib import crc32

from tools.build import ROOT, SCRIPT, STYLE, build, patch_hud
from tools.update_catalog import merge, write_catalog
from tools.patch_notes import collect, format_notes
from tools.update_counter_stats import compute, format_stats, latest_gameplay_patch, report

STOCK = """
<root><styles><include src="stock.vcss"/></styles><CitadelHud>
  <CitadelHudTopBar id="TopBar"/>
  <CitadelHudHeroShop id="CitadelHudHeroShop"/>
</CitadelHud></root>
"""


def unpack_vpk(data):
    signature, version, tree_size, data_size, archive_md5, other_md5, signature_size = unpack_from("<7I", data)
    assert signature == 0x55AA1234 and version == 2
    assert archive_md5 == other_md5 == signature_size == 0
    data_start = 28 + tree_size
    assert len(data[data_start:]) == data_size
    position = 28

    def next_string():
        nonlocal position
        end = data.index(0, position, data_start)
        token = data[position:end].decode("ascii")
        position = end + 1
        return token

    files = {}
    while (extension := next_string()):
        while (directory := next_string()):
            while (stem := next_string()):
                crc, preload, index, offset, length, end_marker = unpack_from("<IHHIIH", data, position)
                assert preload == 0 and index == 0x7FFF and end_marker == 0xFFFF
                position += 18
                file = data[data_start + offset:data_start + offset + length]
                assert len(file) == length and crc32(file) == crc
                files[f"{directory}/{stem}.{extension}"] = file
    assert position == data_start
    return files


def read_resource(data):
    total, header_version, version, table_offset, count = unpack_from("<IHHII", data)
    assert total == len(data) and header_version == 12 and count == 1
    block_pos = 8 + table_offset
    block, offset, length = unpack_from("<4sII", data, block_pos)
    start = block_pos + 4 + offset
    assert block == b"DATA" and start + length == len(data)
    payload = data[start:]
    if version == 3:
        checksum, separator = unpack_from("<IH", payload)
        payload = payload[6:]
        assert separator == 0 and checksum == crc32(payload)
    else:
        assert version == 2
    return payload.decode("utf-8")


class PackagingTests(unittest.TestCase):
    def test_patch_keeps_the_base_hud_and_adds_only_its_own_controls(self):
        root = ET.fromstring(patch_hud(STOCK))
        self.assertEqual(root.find("styles/include").get("src"), "stock.vcss")
        self.assertEqual(root.findall("styles/include")[-1].get("src"), STYLE)
        self.assertEqual(root.find("scripts/include").get("src"), SCRIPT)
        self.assertIsNotNone(root.find("CitadelHud/CitadelHudTopBar"))
        listener = root.find(".//*[@id='DCIVisibility']")
        self.assertEqual((listener.get("hittest"), listener.get("hittestchildren")), ("false", "true"))
        hoverable = {panel.get("id") for panel in listener.iter() if panel.get("hittest") == "true"}
        self.assertEqual(hoverable, {"DCIPatchBadge", "DCIPatch0", "DCIPatch1", "DCIPatch2", "DCIPatch3"},
                         "only the patch icons may catch the mouse; everything else lets clicks through")
        self.assertTrue(all(panel.get("hittest") in ("true", "false") for panel in listener.iter() if panel is not listener))
        hidden = {panel.get("id") for panel in listener.iter() if "DCIHidden" in panel.get("class", "").split()}
        self.assertEqual(hidden, {"DCITip", "DCIPatchBadge", "DCISource"} | {
            f"DCI{part}{index}" for part in ("Row", "Patch", "Buy", "Impact") for index in range(4)
        })
        self.assertEqual(root.find(".//*[@class='DCITitle']").get("text"), "Counter items")
        self.assertIsNotNone(root.find(".//*[@id='DCIPrice2']"))
        self.assertIsNotNone(root.find(".//*[@id='DCIAlertText']"))
        self.assertIsNotNone(root.find(".//*[@id='DCIImpact3']"))
        self.assertIsNone(root.find(".//*[@id='DCIRow4']"))

    def test_patch_refuses_an_incompatible_or_already_patched_layout(self):
        with self.assertRaises(ValueError):
            patch_hud(STOCK.replace("TopBar\"", "Different\""))
        with self.assertRaises(ValueError):
            patch_hud(patch_hud(STOCK))

    def test_zip_contains_a_self_contained_vpk_with_real_source2_resources(self):
        with tempfile.TemporaryDirectory() as tmp:
            output = Path(tmp) / "dci.zip"
            catalog_build = json.loads((ROOT / "src/catalog.json").read_text(encoding="utf-8"))["clientVersion"]
            self.assertEqual(build(STOCK, ROOT / "src/catalog.json", output), catalog_build)
            with ZipFile(output) as archive:
                self.assertEqual(archive.namelist(), ["pak01_dir.vpk"])
                self.assertIsNone(archive.testzip())
                resources = unpack_vpk(archive.read("pak01_dir.vpk"))
            self.assertEqual(sorted(resources), [
                "panorama/layout/hud.vxml_c", "panorama/scripts/dci_hud.vts_c",
                "panorama/styles/dci_hud.vcss_c",
            ])
            layout = ET.fromstring(read_resource(resources["panorama/layout/hud.vxml_c"]))
            script_path = layout.find("scripts/include").get("src").removeprefix("s2r://")
            script = read_resource(resources[script_path])
            self.assertIn('alertTitle: "Enemy counter purchase"', script)
            self.assertIn('var DCI_COUNTERS = {"source"', script)
            self.assertIn('var DCI_STATS = {"source"', script)
            self.assertIn('var DCI_PATCHES = {"reviewed"', script)
            self.assertNotIn("DCI_LANGUAGE", script)
            style = read_resource(resources["panorama/styles/dci_hud.vcss_c"])
            self.assertIn(".gShopOpen .DCIBox", style)
            self.assertIn(".DCIHidden { visibility: collapse; }", style)

    def test_catalog_requires_matching_locales_and_rejects_disabled_items(self):
        english = [{"id": 1, "class_name": "item", "name": "Item", "shopable": True, "cost": 800,
                    "item_slot_type": "spirit"},
                   {"id": 2, "class_name": "disabled", "name": "Disabled", "shopable": True, "disabled": True}]
        turkish = [dict(english[0], name="Eşya"), english[1]]
        items = merge(english, turkish, shops=True)
        self.assertEqual(items, [[1, "item", "Item", "Eşya", 800, "spirit"]])
        with self.assertRaises(ValueError):
            merge(english, [dict(turkish[0], item_slot_type="weapon")], shops=True)
        with self.assertRaises(ValueError):
            merge(english, [dict(turkish[0], cost=1600)], shops=True)
        with tempfile.TemporaryDirectory() as tmp:
            output = Path(tmp) / "catalog.json"
            write_catalog(output, 6861, items, [])
            self.assertEqual(json.loads(output.read_text())["items"], items)


    def test_counter_stats_correct_for_hero_strength_and_item_timing(self):
        catalog = {"items": [[1, "a", "A", "A", 800, "weapon"], [2, "b", "B", "B", 800, "spirit"],
                             [3, "c", "C", "C", 6400, "vitality"], [4, "d", "D", "D", 6400, "spirit"],
                             [5, "rare", "Rare", "Rare", 800, "weapon"]],
                   "heroes": [[7, "hero_x", "X", "X"]]}
        counters = {"heroes": {"hero_x": ["a", "c"]}}

        def rows(*values):
            return [{"item_id": item, "wins": wins, "matches": matches} for item, wins, matches in values]

        base = rows((1, 50000, 100000), (2, 50000, 100000), (3, 60000, 100000), (4, 60000, 100000), (5, 500, 1000))
        # Facing X lifts every buyer by 1.5 points and lifts late items as a group: neither is a counter.
        versus = rows((1, 52000, 100000), (2, 48000, 100000), (3, 63000, 100000), (4, 63000, 100000),
                      (5, 900, 1000))
        stats, measured = compute(catalog, counters, base, {7: versus}, {"from": "f", "to": "t"})
        self.assertEqual(measured["hero_x"], {"a": (200, 100000), "b": (-200, 100000),
                                              "c": (0, 100000), "d": (0, 100000)})
        self.assertEqual(stats["items"], ["a", "c"])
        self.assertEqual(stats["heroes"], {"hero_x": [200, 0]})
        text = report({"items": [row[:5] for row in catalog["items"]], "heroes": catalog["heroes"]},
                      counters, stats, measured)
        self.assertIn("Of the 2 reference picks, 1 raise", text)
        self.assertIn("| X | A +2.00 · C +0.00 | A +2.00 |", text)


    def test_the_window_starts_at_the_latest_patch_that_changes_items_or_heroes(self):
        catalog = {"items": [[1, "upgrade_healbane", "Healbane", "Şifakıran", 1600, "vitality"]],
                   "heroes": [[6, "hero_atlas", "Abrams", "Abrams"]]}
        patches = [
            {"title": "Ranked - Beta Season 2", "pub_date": "2026-11-05T19:58:14Z", "content": "<p>Season two begins.</p>"},
            {"title": " Minor Update - 11-04-2026", "pub_date": "2026-11-05T05:02:30Z",
             "content": "<p class=\"bb_paragraph\">- Healbane: Stacks now diminish 20% more aggressively</p>"},
            {"title": "Gameplay Update", "pub_date": "2026-10-30T20:51:49Z", "content": "<p>- Abrams: Base health increased</p>"},
        ]
        self.assertEqual(latest_gameplay_patch(catalog, patches),
                         {"title": "Minor Update - 11-04-2026", "date": "2026-11-05", "timestamp": 1793854950})
        self.assertIsNone(latest_gameplay_patch(catalog, patches[:1]))

    def test_lane_counters_and_buy_minutes(self):
        catalog = {"items": [[1, "a", "A", "A", 800, "weapon"], [2, "b", "B", "B", 800, "spirit"],
                             [3, "big", "Big", "Big", 6400, "vitality"]],
                   "heroes": [[7, "hero_x", "X", "X"]]}
        counters = {"heroes": {"hero_x": ["a"]}}

        def rows(*values):
            return [{"item_id": item, "wins": wins, "matches": matches, "avg_buy_time_s": seconds}
                    for item, wins, matches, seconds in values]

        base = rows((1, 50000, 100000, 300), (2, 50000, 100000, 330), (3, 50000, 100000, 1800))
        early = rows((1, 50000, 100000, 200), (2, 50000, 100000, 250), (3, 50000, 100000, 590))
        lane = rows((1, 54000, 100000, 200), (2, 46000, 100000, 250), (3, 60000, 100000, 590))
        stats, _ = compute(catalog, counters, base, {7: base}, {"from": "f", "to": "t"},
                           lane_base_rows=early, lane_rows={7: lane},
                           patch={"title": "Minor Update", "date": "2026-11-05", "timestamp": 0})
        # Big costs more than lane items may (3,200), so only A, which beats B, is a lane pick.
        self.assertEqual(stats["lane"], {"hero_x": [["a", 400]]})
        self.assertEqual(stats["minutes"], {"a": 5})
        self.assertEqual(stats["patch"], {"title": "Minor Update", "date": "2026-11-05"})
        self.assertEqual(json.loads(format_stats(stats)), stats)


    def test_patch_notes_cover_changes_after_the_review_date(self):
        catalog = {"items": [[1, "upgrade_healbane", "Healbane", "Şifakıran", 1600, "vitality"]],
                   "heroes": [[6, "hero_atlas", "Abrams", "Abrams"], [69, "hero_doorman", "The Doorman", "Kapıcı"],
                              [84, "hero_ratking", "Rat King", "Fare Kral"]]}
        counters = {"reviewed": {"date": "2026-10-30", "heroes": {"hero_ratking": "2026-11-19"}}}
        long_line = "Shoulder Charge - " + "much longer stun " * 10
        patches = [
            {"title": "Gameplay Update", "pub_date": "2026-10-30T20:51:49Z", "content": "<p>- Abrams: Old change</p>"},
            {"title": " Minor Update - 11-04-2026", "pub_date": "2026-11-05T05:02:30Z", "content": (
                "<p>- Healbane: Stacks now diminish 20% more aggressively</p><p>- Abrams: Base health increased</p>"
                "<p>- Abrams: " + long_line + "</p><p>- Abrams: Siphon Life radius increased</p>"
                "<p>- Abrams: Infernal Resilience regen increased</p><p>- Doorman: Hotel Guest duration reduced</p>"
                "<p>- Rat King: Ammo increased from 24 to 26</p>")},
            {"title": "Ranked - Beta Season 2", "pub_date": "2026-11-05T19:58:14Z", "content": "<p>Season two</p>"},
        ]
        notes = collect(catalog, counters, patches)
        self.assertEqual(notes["reviewed"], "2026-10-30")
        self.assertEqual(len(notes["patches"]), 1, "the reviewed patch and the non-gameplay post are left out")
        patch = notes["patches"][0]
        self.assertEqual((patch["title"], patch["date"]), ("Minor Update - 11-04-2026", "2026-11-05"))
        self.assertEqual(set(patch["heroes"]), {"hero_atlas", "hero_doorman"}, "Rat King was reviewed after the patch")
        abrams = patch["heroes"]["hero_atlas"]
        self.assertEqual(abrams["more"], 1)
        self.assertEqual(abrams["lines"][0], "Base health increased")
        self.assertTrue(abrams["lines"][1].endswith("…") and len(abrams["lines"][1]) == 110)
        self.assertEqual(patch["items"], {"upgrade_healbane": {"lines": ["Stacks now diminish 20% more aggressively"], "more": 0}})
        self.assertEqual(json.loads(format_notes(notes)), notes)

    def test_game_data_changes_without_patch_notes_are_flagged(self):
        catalog = {"clientVersion": 200,
                   "items": [[10, "upgrade_overflow", "Spiritual Overflow", "Ruhani Taşma", 3200, "spirit"],
                             [11, "upgrade_same", "Same", "Aynı", 800, "weapon"]],
                   "heroes": [[1, "hero_a", "Alpha", "Alfa"], [2, "hero_b", "Beta", "Beta"], [3, "hero_c", "Gamma", "Gama"]]}
        counters = {"reviewed": {"date": "2026-10-30", "build": 100,
                                 "heroes": {"hero_c": {"date": "2026-11-10", "build": 150}}}}
        patches = [{"title": "Minor Update", "pub_date": "2026-11-05T05:02:30Z",
                    "content": "<p>- Alpha: Fireball cooldown reduced from 10s to 8s</p>"}]

        def build(number):
            later = number >= 200
            shared = {"ability_jump": "citadel_ability_jump"}
            def prop(value, label, unit=""):
                return {"value": value, "label": label, "postfix": unit}
            return {
                # Five heroes share the jump, so a jump change is not a change to any one of them.
                "heroes": [{"class_name": key, "items": dict(shared, **slots), "starting_stats": {"max_health": {"value": 600}}}
                           for key, slots in (("hero_a", {"signature1": "ability_fire"}),
                                              ("hero_b", {"signature1": "ability_ice", "weapon_primary": "weapon_b"}),
                                              ("hero_c", {"signature1": "ability_rock"}),
                                              ("hero_d", {}), ("hero_e", {}))],
                "items/by-type/ability": [
                    {"class_name": "ability_fire", "name": "Fireball", "properties": {"AbilityCooldown": prop("8" if later else "10", "Cooldown", "s")}},
                    {"class_name": "ability_ice", "name": "Ice Wall", "properties": {"Radius": prop("6m" if later else "5m", "Radius", "m")},
                     "upgrades": [{"property_upgrades": [{"name": "Radius", "bonus": 2 if later else 1}]}]},
                    {"class_name": "ability_rock", "name": "Rock", "properties": {"Damage": prop("50" if number >= 150 else "40", "Damage")}},
                    {"class_name": "citadel_ability_jump", "name": "Jump", "properties": {"Height": prop("2" if later else "1", "Height")}},
                ],
                "items/by-type/weapon": [{"class_name": "weapon_b", "name": "Beta Gun", "weapon_info": {
                    "bullet_damage": 5.8 if later else 5.7, "damage_per_second": 58.0 if later else 57.0, "can_zoom": True}}],
                "items/by-type/upgrade": [
                    {"class_name": "upgrade_overflow", "type": "upgrade", "cost": 3200,
                     "properties": {"AbilityLifestealPercentHero": prop("15" if later else "13", "Spirit Lifesteal", "%")}},
                    {"class_name": "upgrade_same", "type": "upgrade", "cost": 800, "properties": {"Ammo": prop("20", "Ammo")}},
                ],
            }

        loaded = []
        notes = collect(catalog, counters, patches, lambda number: loaded.append(number) or build(number))
        self.assertEqual(sorted(loaded), [100, 150, 200], "each build is loaded once")
        self.assertEqual((notes["reviewedBuild"], notes["build"]), (100, 200))
        noted, silent = notes["patches"]
        self.assertEqual(set(noted["heroes"]), {"hero_a"})
        self.assertEqual((silent["title"], silent["build"]), ("an update without patch notes", 200))
        # Alpha's change is in the notes, Gamma's came before its own review, and the shared jump
        # and the weapon's derived damage per second do not count.
        self.assertEqual(silent["heroes"], {"hero_b": {"lines": [
            "Ice Wall - Radius 5m → 6m", "Ice Wall - T1 Radius 1m → 2m", "Beta Gun - Bullet damage 5.7 → 5.8"], "more": 0}})
        self.assertEqual(silent["items"], {"upgrade_overflow": {"lines": ["Spirit Lifesteal 13% → 15%"], "more": 0}})
        self.assertEqual(json.loads(format_notes(notes)), notes)

        unchanged = collect(catalog, {"reviewed": {"date": "2026-10-30", "build": 200}}, [], build)
        self.assertEqual(unchanged["patches"], [], "the same build has nothing to report")


if __name__ == "__main__":
    unittest.main()
