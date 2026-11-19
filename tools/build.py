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
SCRIPT_SOURCE = "panorama/scripts/dci_hud.vts"
# Valve's layouts name scripts by source path (s2r://panorama/scripts/async.vts), like styles.
SCRIPT = f"s2r://{SCRIPT_SOURCE}"
ROWS = 4


def patch_hud(stock):
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

    def add(parent, tag, attributes, hover=False, hidden=False):
        """Every panel lets clicks through except the patch icons, which show a tooltip on hover."""
        if hidden:
            attributes = {**attributes, "class": attributes["class"] + " DCIHidden"}
        return ET.SubElement(parent, tag, {**attributes, "hittest": "true" if hover else "false"})

    listener = ET.SubElement(hud, "GlobalClassListener", id="DCIVisibility",
                             classes="gScoreboardOpen gShopOpen", hittest="false",
                             hittestchildren="true")
    alert = add(listener, "Panel", {"id": "DCIAlert", "class": "DCIAlert"})
    add(alert, "Label", {"id": "DCIAlertTitle", "class": "DCIAlertTitle", "text": "Enemy counter purchase"})
    add(alert, "Label", {"id": "DCIAlertText", "class": "DCIAlertText"})
    box = add(listener, "Panel", {"class": "DCIBox"})
    tip = add(box, "Panel", {"id": "DCITip", "class": "DCITip"}, hidden=True)
    add(tip, "Label", {"id": "DCITipTitle", "class": "DCITipTitle"})
    add(tip, "Label", {"id": "DCITipText", "class": "DCITipText"})
    header = add(box, "Panel", {"class": "DCIHeader"})
    add(header, "Label", {"class": "DCITitle", "text": "Counter items"})
    add(header, "Label", {"id": "DCIPatchBadge", "class": "DCIPatchBadge", "text": "PATCHED"}, hover=True, hidden=True)
    add(header, "Label", {"id": "DCISource", "class": "DCISource"}, hidden=True)
    add(box, "Label", {"id": "DCIEmpty", "class": "DCIEmpty", "text": "Waiting for enemy lineup."})
    for index in range(ROWS):
        row = add(box, "Panel", {"id": f"DCIRow{index}", "class": "DCIRow"}, hidden=True)
        add(row, "Panel", {"id": f"DCICategory{index}", "class": "DCICategory"})
        details = add(row, "Panel", {"class": "DCIDetails"})
        line = add(details, "Panel", {"class": "DCIItemLine"})
        add(line, "Label", {"id": f"DCIName{index}", "class": "DCIName"})
        add(line, "Label", {"id": f"DCIPatch{index}", "class": "DCIPatch", "text": "!"}, hover=True, hidden=True)
        add(line, "Label", {"id": f"DCIBuy{index}", "class": "DCIBuy", "text": "BUY NOW"}, hidden=True)
        cost = add(line, "Panel", {"class": "DCICost"})
        add(cost, "Panel", {"class": "DCISoul"})
        add(cost, "Label", {"id": f"DCIPrice{index}", "class": "DCIPrice"})
        add(details, "Label", {"id": f"DCIReason{index}", "class": "DCIReason"})
        evidence = add(details, "Panel", {"class": "DCIEvidenceLine"})
        add(evidence, "Label", {"id": f"DCIEvidence{index}", "class": "DCIEvidence"})
        add(evidence, "Label", {"id": f"DCIImpact{index}", "class": "DCIImpact"}, hidden=True)
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


def build(stock, catalog_path, output, counters_path=ROOT / "src" / "counters.json",
          stats_path=ROOT / "src" / "stats.json", patches_path=ROOT / "src" / "patches.json"):
    src = ROOT / "src"
    catalog = json.loads(catalog_path.read_text(encoding="utf-8"))
    counters = json.loads(counters_path.read_text(encoding="utf-8"))
    stats = json.loads(stats_path.read_text(encoding="utf-8"))
    patches = json.loads(patches_path.read_text(encoding="utf-8"))
    sources = [
        ("panorama/layout/hud.xml", patch_hud(stock)),
        ("panorama/styles/dci_hud.css", (src / "hud.css").read_text(encoding="utf-8")),
    ]
    script = '"use strict";\n'
    script += f"var DCI_CATALOG = {json.dumps(catalog, separators=(',', ':'), ensure_ascii=True)};\n"
    script += f"var DCI_COUNTERS = {json.dumps(counters, separators=(',', ':'), ensure_ascii=True)};\n"
    script += f"var DCI_STATS = {json.dumps(stats, separators=(',', ':'), ensure_ascii=True)};\n"
    script += f"var DCI_PATCHES = {json.dumps(patches, separators=(',', ':'), ensure_ascii=True)};\n"
    for filename in ("engine.js", "observer.js", "hud.js"):
        script += (src / filename).read_text(encoding="utf-8") + "\n"
    sources.append((SCRIPT_SOURCE, script))
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
    parser.add_argument("--output", type=Path, default=ROOT / "dist" / "deadlock-counter-intelligence-experimental.zip")
    args = parser.parse_args()
    if args.deadlock_dir and not args.vrf:
        parser.error("--deadlock-dir requires --vrf")
    try:
        stock = (args.hud_xml.read_text(encoding="utf-8-sig") if args.hud_xml
                 else decompile(args.deadlock_dir, args.vrf.resolve()))
        version = build(stock, args.catalog, args.output)
    except (ET.ParseError, OSError, ValueError, KeyError, subprocess.SubprocessError) as error:
        parser.exit(1, f"Build failed: {error}\n")
    print(f"{args.output} | catalog {version} | experimental, not tested in Deadlock")


if __name__ == "__main__":
    main()
