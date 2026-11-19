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
from tools.update_counter_stats import compute, report

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
        self.assertEqual(root.find(".//*[@id='DCIVisibility']").get("hittestchildren"), "false")
        self.assertEqual(root.find(".//*[@class='DCITitle']").get("text"), "Counter items")
        self.assertIsNotNone(root.find(".//*[@id='DCIPrice2']"))
        self.assertEqual(root.find(".//*[@id='DCIRow2']").get("visible"), "false")
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
            self.assertEqual(build(STOCK, ROOT / "src/catalog.json", output), 6864)
            with ZipFile(output) as archive:
                self.assertEqual(archive.namelist(), ["pak01_dir.vpk"])
                self.assertIsNone(archive.testzip())
                resources = unpack_vpk(archive.read("pak01_dir.vpk"))
            self.assertEqual(sorted(resources), [
                "panorama/layout/hud.vxml_c", "panorama/scripts/dci_hud.vts_c",
                "panorama/styles/dci_hud.vcss_c",
            ])
            self.assertIn("DCIVisibility", read_resource(resources["panorama/layout/hud.vxml_c"]))
            script = read_resource(resources["panorama/scripts/dci_hud.vts_c"])
            self.assertIn('alertTitle: "Enemy counter purchase"', script)
            self.assertIn('var DCI_COUNTERS = {"source"', script)
            self.assertIn('var DCI_STATS = {"source"', script)
            self.assertNotIn("DCI_LANGUAGE", script)
            self.assertIn(".gShopOpen .DCIBox", read_resource(resources["panorama/styles/dci_hud.vcss_c"]))

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


if __name__ == "__main__":
    unittest.main()
