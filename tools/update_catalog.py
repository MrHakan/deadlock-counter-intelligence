"""Fetch a matched Turkish/English Deadlock catalog for an explicit client version."""

import argparse
import json
import re
from pathlib import Path
import tempfile
from urllib.parse import urlencode
from urllib.request import Request, urlopen

API = "https://api.deadlock-api.com/v1/assets"
CATEGORIES = ("weapon", "vitality", "spirit")


def fetch(path, version, language, **params):
    query = urlencode(dict(language=language, client_version=version, **params))
    request = Request(f"{API}/{path}?{query}", headers={"User-Agent": "deadlock-counter-intelligence"})
    with urlopen(request, timeout=30) as response:
        rows = json.load(response)
    if not isinstance(rows, list) or not rows:
        raise ValueError(f"No rows returned for {path} ({language})")
    return rows


def icon_path(url):
    """The game texture behind deadlock-api's shop image, e.g. .../images/items/vitality/healbane.png
    -> s2r://panorama/images/items/vitality/healbane_psd.vtex. The HUD loads it from the game files,
    so it needs no network during a match. Empty when the URL has another shape."""
    match = re.search(r"/assets-api-res/(images/items/[a-z_]+/[a-z0-9_]+)\.(?:png|webp)$", url or "")
    return f"s2r://panorama/{match.group(1)}_psd.vtex" if match else ""


def merge(english, turkish, shops=False):
    if shops:
        def eligible(row):
            return row["shopable"] is True and row.get("disabled") is not True
        english = filter(eligible, english)
        turkish = filter(eligible, turkish)
    source = {row["id"]: row for row in english}
    translations = {row["id"]: row for row in turkish}
    if not source or source.keys() != translations.keys():
        raise ValueError("Catalog languages returned different ID sets")
    merged = []
    for identifier, row in sorted(source.items()):
        translated = translations[identifier]
        if translated["class_name"] != row["class_name"] or not row["name"] or not translated["name"]:
            raise ValueError(f"Invalid localization for {identifier}")
        entry = [identifier, row["class_name"], row["name"], translated["name"]]
        if shops:
            cost = row["cost"]
            if type(cost) is not int or cost < 0 or cost != translated["cost"]:
                raise ValueError(f"Invalid cost for {identifier}")
            slot = row.get("item_slot_type")
            if slot not in CATEGORIES or slot != translated.get("item_slot_type"):
                raise ValueError(f"Invalid category for {identifier}")
            entry.extend([cost, slot, icon_path(row.get("shop_image"))])
        merged.append(entry)
    return merged


def write_catalog(path, version, items, heroes):
    def format_rows(rows):
        return ",\n".join("    " + json.dumps(row, ensure_ascii=False) for row in rows)

    data = ('{\n  "clientVersion": ' + str(version) +
            ',\n  "items": [\n' + format_rows(items) +
            '\n  ],\n  "heroes": [\n' + format_rows(heroes) + '\n  ]\n}\n')
    with tempfile.TemporaryDirectory(dir=path.parent) as temporary_dir:
        pending = Path(temporary_dir) / "catalog.json"
        pending.write_text(data, encoding="utf-8")
        pending.replace(path)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("client_version", type=int)
    parser.add_argument("--output", type=Path,
                        default=Path(__file__).resolve().parents[1] / "src" / "catalog.json")
    args = parser.parse_args()
    version = args.client_version
    try:
        items = merge(*(fetch("items/by-type/upgrade", version, lang) for lang in ("english", "turkish")),
                      shops=True)
        heroes = merge(*(fetch("heroes", version, lang, only_active="true") for lang in ("english", "turkish")))
        write_catalog(args.output, version, items, heroes)
    except (ValueError, OSError, KeyError) as error:
        parser.exit(1, f"Catalog update failed: {error}\n")


if __name__ == "__main__":
    main()
