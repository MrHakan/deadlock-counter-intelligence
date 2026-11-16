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

    def test_patch_refuses_an_incompatible_or_already_patched_layout(self):
        with self.assertRaises(ValueError):
            patch_hud(STOCK.replace("TopBar\"", "Different\""))
        with self.assertRaises(ValueError):
            patch_hud(patch_hud(STOCK))

    def test_zip_contains_a_self_contained_vpk_with_real_source2_resources(self):
        with tempfile.TemporaryDirectory() as tmp:
            output = Path(tmp) / "dci.zip"
            self.assertEqual(build(STOCK, ROOT / "src/catalog.json", output), 6861)
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
            self.assertIn('shop: "Recent purchases"', script)
            self.assertNotIn("DCI_LANGUAGE", script)
            self.assertIn(".gShopOpen .DCIBox", read_resource(resources["panorama/styles/dci_hud.vcss_c"]))

    def test_catalog_requires_matching_locales_and_rejects_disabled_items(self):
        english = [{"id": 1, "class_name": "item", "name": "Item", "shopable": True, "cost": 800},
                   {"id": 2, "class_name": "disabled", "name": "Disabled", "shopable": True, "disabled": True}]
        turkish = [dict(english[0], name="Eşya"), english[1]]
        items = merge(english, turkish, shops=True)
        self.assertEqual(items, [[1, "item", "Item", "Eşya", 800]])
        with self.assertRaises(ValueError):
            merge(english, [dict(turkish[0], cost=1600)], shops=True)
        with tempfile.TemporaryDirectory() as tmp:
            output = Path(tmp) / "catalog.json"
            write_catalog(output, 6861, items, [])
            self.assertEqual(json.loads(output.read_text())["items"], items)


if __name__ == "__main__":
    unittest.main()
