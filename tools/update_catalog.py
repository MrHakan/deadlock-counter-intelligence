"""Fetch a matched Turkish/English Deadlock catalog for an explicit client version."""

import argparse
import json
from pathlib import Path
import tempfile
from urllib.parse import urlencode
from urllib.request import urlopen

API = "https://api.deadlock-api.com/v1/assets"


def fetch(path, version, language, **params):
    query = urlencode(dict(language=language, client_version=version, **params))
    with urlopen(f"{API}/{path}?{query}", timeout=30) as response:
        rows = json.load(response)
    if not isinstance(rows, list) or not rows:
        raise ValueError(f"No rows returned for {path} ({language})")
    return rows


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
            entry.append(cost)
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
