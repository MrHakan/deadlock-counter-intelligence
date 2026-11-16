"""Build an experimental Deadlock Mod Manager archive from a stock HUD layout."""

import argparse
import json
from itertools import groupby
from pathlib import Path
import struct
import subprocess
import sys
import tempfile
from xml.etree import ElementTree as ET
from zipfile import ZIP_DEFLATED, ZipFile, ZipInfo
import zlib

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "third_party"))
from panorama import compile_text

STOCK_HUD = "panorama/layout/hud.vxml_c"
STYLE = "s2r://panorama/styles/dci_hud.vcss"
SCRIPT = "s2r://panorama/scripts/dci_hud.vts"


def patch_hud(stock, language):
    root = ET.fromstring(stock)
    huds = root.findall("CitadelHud")
    styles = root.findall("styles")
    scripts = root.findall("scripts")
    if root.tag != "root" or len(huds) != 1 or len(styles) != 1 or len(scripts) > 1:
        raise ValueError("Unexpected stock HUD structure")
    hud = huds[0]
    ids = {panel.get("id") for panel in hud.iter()}
    if not {"TopBar", "CitadelHudHeroShop"} <= ids or any(
        panel_id and panel_id.startswith("DCI") for panel_id in ids
    ):
        raise ValueError("Expected a clean Deadlock HUD with the stock top bar and shop")
    ET.SubElement(styles[0], "include", src=STYLE)
    if scripts:
        script_list = scripts[0]
    else:
        script_list = ET.Element("scripts")
        root.insert(list(root).index(styles[0]) + 1, script_list)
    ET.SubElement(script_list, "include", src=SCRIPT)

    listener = ET.SubElement(hud, "GlobalClassListener", id="DCIVisibility",
                             classes="gScoreboardOpen gShopOpen", hittest="false",
                             hittestchildren="false")
    box = ET.SubElement(listener, "Panel", {"class": "DCIBox"})
    header = ET.SubElement(box, "Panel", {"class": "DCIHeader"})
    title = "Karşı itemler" if language == "turkish" else "Counter items"
    waiting = "Rakip kadrosu bekleniyor." if language == "turkish" else "Waiting for enemy lineup."
    ET.SubElement(header, "Label", {"class": "DCITitle", "text": title})
    ET.SubElement(header, "Label", {"id": "DCISource", "class": "DCISource", "visible": "false"})
    ET.SubElement(box, "Label", {"id": "DCIEmpty", "class": "DCIEmpty", "text": waiting})
    for index in range(3):
        row = ET.SubElement(box, "Panel", {"id": f"DCIRow{index}", "class": "DCIRow", "visible": "false"})
        ET.SubElement(row, "Panel", {"class": "DCICategory"})
        details = ET.SubElement(row, "Panel", {"class": "DCIDetails"})
        line = ET.SubElement(details, "Panel", {"class": "DCIItemLine"})
        ET.SubElement(line, "Label", {"id": f"DCIName{index}", "class": "DCIName"})
        cost = ET.SubElement(line, "Panel", {"class": "DCICost"})
        ET.SubElement(cost, "Panel", {"class": "DCISoul"})
        ET.SubElement(cost, "Label", {"id": f"DCIPrice{index}", "class": "DCIPrice"})
        ET.SubElement(details, "Label", {"id": f"DCIReason{index}", "class": "DCIReason"})
        ET.SubElement(details, "Label", {"id": f"DCIEvidence{index}", "class": "DCIEvidence"})
    return ET.tostring(root, encoding="unicode")


def pack_vpk(resources):
    """Write a VPK v2 with inline data and no external chunks."""
    entries = []
    data = bytearray()
    for name, payload in sorted(resources.items()):
        path = Path(name)
        entries.append((path.suffix[1:], path.parent.as_posix(), path.stem, len(data), payload))
        data.extend(payload)

    tree = bytearray()
    for extension, group in groupby(sorted(entries), key=lambda entry: entry[0]):
        tree.extend(extension.encode("ascii") + b"\0")
        for directory, files in groupby(group, key=lambda entry: entry[1]):
            tree.extend(directory.encode("ascii") + b"\0")
            for _, _, stem, offset, payload in files:
                tree.extend(stem.encode("ascii") + b"\0")
                tree.extend(struct.pack("<IHHIIH", zlib.crc32(payload), 0, 0x7FFF, offset, len(payload), 0xFFFF))
            tree.append(0)
        tree.append(0)
    tree.append(0)
    return struct.pack("<7I", 0x55AA1234, 2, len(tree), len(data), 0, 0, 0) + tree + data


def build(stock, catalog_path, language, output):
    src = ROOT / "src"
    catalog = json.loads(catalog_path.read_text(encoding="utf-8"))
    sources = [
        ("panorama/layout/hud.xml", patch_hud(stock, language)),
        ("panorama/styles/dci_hud.css", (src / "hud.css").read_text(encoding="utf-8")),
    ]
    script = '"use strict";\n'
    script += f"var DCI_CATALOG = {json.dumps(catalog, separators=(',', ':'), ensure_ascii=True)};\n"
    script += f"var DCI_LANGUAGE = {json.dumps(language)};\n"
    for filename in ("engine.js", "observer.js", "hud.js"):
        script += (src / filename).read_text(encoding="utf-8") + "\n"
    sources.append(("panorama/scripts/dci_hud.vts", script))
    vpk = pack_vpk(dict(compile_text(name, source) for name, source in sources))
    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(dir=output.parent) as staging:
        pending = Path(staging) / "mod.zip"
        with ZipFile(pending, "w") as archive:
            archive.writestr(ZipInfo("pak01_dir.vpk"), vpk, compress_type=ZIP_DEFLATED)
        pending.replace(output)
    return catalog["clientVersion"]


def decompile(deadlock_dir, vrf):
    base_vpk = deadlock_dir / "game" / "citadel" / "pak01_dir.vpk"
    if not base_vpk.is_file():
        raise ValueError(f"Deadlock game VPK not found at {base_vpk}")
    with tempfile.TemporaryDirectory() as temporary_dir:
        xml = Path(temporary_dir) / "hud.xml"
        subprocess.run([str(vrf), "-i", str(base_vpk), "-f", STOCK_HUD,
                        "-o", str(xml), "-d"], check=True, timeout=120)
        return xml.read_text(encoding="utf-8-sig")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    input_options = parser.add_mutually_exclusive_group(required=True)
    input_options.add_argument("--deadlock-dir", type=Path)
    input_options.add_argument("--hud-xml", type=Path, help="Already decompiled stock HUD XML")
    parser.add_argument("--vrf", type=Path, help="Source2Viewer-CLI executable")
    parser.add_argument("--catalog", type=Path, default=ROOT / "src" / "catalog.json")
    parser.add_argument("--language", choices=("turkish", "english"), default="turkish")
    parser.add_argument("--output", type=Path, default=ROOT / "dist" / "deadlock-counter-intelligence-experimental.zip")
    args = parser.parse_args()
    if args.deadlock_dir and not args.vrf:
        parser.error("--deadlock-dir requires --vrf")
    try:
        stock = (args.hud_xml.read_text(encoding="utf-8-sig") if args.hud_xml
                 else decompile(args.deadlock_dir, args.vrf.resolve()))
        version = build(stock, args.catalog, args.language, args.output)
    except (ET.ParseError, OSError, ValueError, KeyError, subprocess.SubprocessError) as error:
        parser.exit(1, f"Build failed: {error}\n")
    print(f"{args.output} | catalog {version} | experimental, not tested in Deadlock")


if __name__ == "__main__":
    main()
