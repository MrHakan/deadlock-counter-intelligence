"""Collect the hero and item changes from Deadlock patch notes that the counter table predates.

counters.json records under "reviewed" the date the counter table was last checked against the
game, with optional per-hero dates for heroes checked later. Every gameplay patch published after
that date is read from the patch notes (deadlock-api mirrors Valve's posts), and each line of the
form "- <Hero or item>: <change>" is kept for the hero or item it names. The HUD warns about those
heroes and items, and shows the changes on hover, until the table is reviewed again.
"""

import argparse
import html
import json
from pathlib import Path
import re
import tempfile
from urllib.request import Request, urlopen

ROOT = Path(__file__).resolve().parents[1]
PATCHES = "https://api.deadlock-api.com/v2/patches"
LINES_PER_ENTRY = 3
LINE_LENGTH = 110


def fetch_patches():
    request = Request(PATCHES, headers={"User-Agent": "deadlock-counter-intelligence"})
    with urlopen(request, timeout=60) as response:
        patches = json.load(response)
    if not isinstance(patches, list):
        raise ValueError("unexpected answer from the patch-notes API")
    return patches


def shorten(text, length=LINE_LENGTH):
    text = " ".join(text.split())
    return text if len(text) <= length else text[:length - 1].rstrip() + "…"


def name_index(catalog):
    """Map lowercase English names (and "Doorman" for "The Doorman") to catalog keys."""
    names = {}
    for row in catalog["items"]:
        names[row[2].lower()] = row[1]
    for row in catalog["heroes"]:
        names[row[2].lower()] = row[1]
        if row[2].lower().startswith("the "):
            names[row[2][4:].lower()] = row[1]
    return names


def changes(patch, names):
    """Return {key: [change, ...]} for one patch, in the order the notes list them."""
    alternatives = "|".join(re.escape(name) for name in sorted(names, key=len, reverse=True))
    line = re.compile(r"^\s*[-•*]?\s*(" + alternatives + r")\s*:\s*(.+?)\s*$", re.IGNORECASE)
    text = html.unescape(re.sub(r"<[^>]+>", "\n", patch.get("content") or ""))
    found = {}
    for raw in text.split("\n"):
        match = line.match(raw)
        if match:
            found.setdefault(names[match.group(1).lower()], []).append(match.group(2))
    return found


def collect(catalog, counters, patches):
    reviewed = counters.get("reviewed", {})
    since = reviewed.get("date", "")
    per_hero = reviewed.get("heroes", {})
    names = name_index(catalog)
    heroes = {row[1] for row in catalog["heroes"]}
    result = []
    for patch in sorted(patches, key=lambda entry: entry.get("pub_date") or ""):
        published = (patch.get("pub_date") or "")[:10]
        if not published or published <= since:
            continue
        found = changes(patch, names)
        if not found:
            continue  # not a gameplay patch
        entry = {"title": " ".join((patch.get("title") or "").split()), "date": published, "heroes": {}, "items": {}}
        for key, lines in found.items():
            if key in heroes and per_hero.get(key, "") >= published:
                continue  # this hero was checked after the patch
            group = "heroes" if key in heroes else "items"
            entry[group][key] = {"lines": [shorten(text) for text in lines[:LINES_PER_ENTRY]],
                                 "more": max(0, len(lines) - LINES_PER_ENTRY)}
        if entry["heroes"] or entry["items"]:
            result.append(entry)
    return {"reviewed": since, "patches": result}


def format_notes(notes):
    """One line per hero or item keeps the file reviewable in diffs."""
    parts = ['{\n  "reviewed": ' + json.dumps(notes["reviewed"]) + ',\n  "patches": [']
    patches = []
    for patch in notes["patches"]:
        body = [f'      "title": {json.dumps(patch["title"], ensure_ascii=False)},', f'      "date": {json.dumps(patch["date"])},']
        for group in ("heroes", "items"):
            rows = ",\n".join(f'        "{key}": {json.dumps(value, ensure_ascii=False)}'
                              for key, value in sorted(patch[group].items()))
            body.append(f'      "{group}": {{\n{rows}\n      }}' + ("," if group == "heroes" else ""))
        patches.append("    {\n" + "\n".join(body) + "\n    }")
    parts.append(",\n".join(patches))
    return "\n".join(parts) + "\n  ]\n}\n"


def main():
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--output", type=Path, default=ROOT / "src" / "patches.json")
    args = parser.parse_args()
    catalog = json.loads((ROOT / "src" / "catalog.json").read_text(encoding="utf-8"))
    counters = json.loads((ROOT / "src" / "counters.json").read_text(encoding="utf-8"))
    try:
        notes = collect(catalog, counters, fetch_patches())
        with tempfile.TemporaryDirectory(dir=args.output.parent) as temporary_dir:
            pending = Path(temporary_dir) / args.output.name
            pending.write_text(format_notes(notes), encoding="utf-8")
            pending.replace(args.output)
    except (OSError, ValueError, KeyError) as error:
        parser.exit(1, f"Patch notes update failed: {error}\n")
    heroes = sorted({key for patch in notes["patches"] for key in patch["heroes"]})
    items = sorted({key for patch in notes["patches"] for key in patch["items"]})
    print(f"{args.output} | {len(notes['patches'])} patches since {notes['reviewed'] or 'the beginning'} | "
          f"{len(heroes)} heroes, {len(items)} items to review")


if __name__ == "__main__":
    main()
