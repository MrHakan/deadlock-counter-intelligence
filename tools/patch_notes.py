"""Collect the hero and item changes the counter table predates, from patch notes and game data.

counters.json records under "reviewed" the date the counter table was last checked against the
game and the game build whose data it was checked against, with optional per-hero dates and builds
for heroes checked later. Two sources feed the warnings:

- Patch notes. Every gameplay patch published after the review date is read from the patch notes
  (deadlock-api mirrors Valve's posts), and each line of the form "- <Hero or item>: <change>" is
  kept for the hero or item it names.
- Game data. Valve also ships balance changes without notes (build 6833 raised Spiritual Overflow's
  spirit lifesteal from 13% to 15% with none). The hero, ability, weapon and item values of the
  reviewed build and of the current build are compared (deadlock-api parses them from each build's
  game files), and every hero or item whose values changed without a patch-note warning gets an
  entry for "an update without patch notes".

The HUD warns about those heroes and items, and shows the changes on hover, until the table is
reviewed again.
"""

import argparse
from collections import Counter
import html
import json
from pathlib import Path
import re
import tempfile
from urllib.parse import urlencode
from urllib.request import Request, urlopen

ROOT = Path(__file__).resolve().parents[1]
PATCHES = "https://api.deadlock-api.com/v2/patches"
ASSETS = "https://api.deadlock-api.com/v1/assets"
# What one game build defines for heroes, their abilities and weapons, and the shop items.
ASSET_KINDS = ("heroes", "items/by-type/ability", "items/by-type/weapon", "items/by-type/upgrade")
HEADERS = {"User-Agent": "deadlock-counter-intelligence"}
LINES_PER_ENTRY = 3
LINE_LENGTH = 110
# Abilities more heroes than this share (jump, slide, zip line, ...) are not changes to one hero.
SHARED_ABILITY = 3
# Weapon values the game derives from others (damage per second from bullet damage and fire rate).
DERIVED_WEAPON = re.compile(r"per_second|per_shot|per_magazine|^recoil_seed$")
UNANNOUNCED = "an update without patch notes"


def fetch_json(url):
    with urlopen(Request(url, headers=HEADERS), timeout=60) as response:
        return json.load(response)


def fetch_patches():
    patches = fetch_json(PATCHES)
    if not isinstance(patches, list):
        raise ValueError("unexpected answer from the patch-notes API")
    return patches


def fetch_build(build):
    """The hero, ability, weapon and shop-item records of one game build."""
    data = {}
    for kind in ASSET_KINDS:
        rows = fetch_json(f"{ASSETS}/{kind}?" + urlencode({"client_version": build, "language": "english"}))
        if not isinstance(rows, list) or not rows:
            raise ValueError(f"deadlock-api has no {kind} data for build {build}")
        data[kind] = rows
    return data


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


def words(name):
    """'AbilityLifestealPercentHero', 'max_move_speed' or 'MODIFIER_VALUE_TECH_RESIST' as words.

    The game calls spirit "tech" internally; the words use the name players see."""
    name = re.sub(r"^(?:MODIFIER_VALUE_|E(?=[A-Z][a-z]))", "", name)
    name = re.sub(r"(?<=[a-z0-9])(?=[A-Z])", " ", name).replace("_", " ")
    name = re.sub(r"\btech\b", "spirit", " ".join(name.split()).lower()).replace("armor damage reduction", "resist")
    return name[:1].upper() + name[1:]


def number(value, unit=""):
    """'40', '5.5m', '13%': values come as numbers or strings, some with their unit already."""
    text = f"{value:g}" if isinstance(value, float) else str(value)
    unit = unit.strip()
    return text if not unit or text.endswith(unit) else text + unit


def item_values(row):
    """{id: (label, value)} for the gameplay values of one ability, weapon or shop item."""
    properties = row.get("properties") or {}
    values = {}

    def label(name):
        prop = properties.get(name) if isinstance(properties.get(name), dict) else {}
        return prop.get("label") or words(name), prop.get("postfix") or ""

    for name, prop in properties.items():
        if isinstance(prop, dict) and "value" in prop:
            text, unit = label(name)
            values[name] = (text, number(prop["value"], unit))
    for tier, upgrade in enumerate(row.get("upgrades") or [], 1):
        for bonus in upgrade.get("property_upgrades") or []:
            text, unit = label(bonus.get("name") or "")
            values[f"T{tier}/{bonus.get('name')}"] = (f"T{tier} {text}", number(bonus.get("bonus"), unit))
    for name, value in (row.get("weapon_info") or {}).items():
        if isinstance(value, (int, float)) and not isinstance(value, bool) and not DERIVED_WEAPON.search(name):
            values[f"weapon/{name}"] = (words(name), number(value))
    if row.get("type") == "upgrade" and isinstance(row.get("cost"), int):
        values["cost"] = ("Cost", str(row["cost"]))
    return values


def snapshot(data, catalog):
    """{key: {id: (label, value)}} for the catalog's heroes and shop items in one build's data.

    A hero's values are its base stats, its level-up and scaling stats, which abilities fill its
    slots, and the values of its own weapon and abilities."""
    abilities = {row.get("class_name"): row
                 for kind in ("items/by-type/ability", "items/by-type/weapon") for row in data[kind]}
    owners = Counter(ability for hero in data["heroes"] for ability in set((hero.get("items") or {}).values()))
    heroes = {row.get("class_name"): row for row in data["heroes"]}
    items = {row.get("class_name"): row for row in data["items/by-type/upgrade"]}
    result = {}
    for row in catalog["heroes"]:
        hero = heroes.get(row[1])
        if not hero:
            continue
        values = {}
        for name, stat in (hero.get("starting_stats") or {}).items():
            if isinstance(stat, dict) and "value" in stat:
                values[f"stat/{name}"] = (words(name), number(stat["value"]))
        for name, value in (hero.get("standard_level_up_upgrades") or {}).items():
            values[f"level/{name}"] = (words(name), number(value))
        for name, scaling in (hero.get("scaling_stats") or {}).items():
            values[f"scaling/{name}"] = (words(name) + " scaling",
                                         f"{number(scaling.get('scale'))} × {words(scaling.get('scaling_stat') or '').lower()}")
        for slot, ability in (hero.get("items") or {}).items():
            if owners[ability] > SHARED_ABILITY:
                continue
            record = abilities.get(ability) or {}
            name = record.get("name") or words(ability)
            values[f"slot/{slot}"] = (words(slot), name)
            for key, (text, value) in item_values(record).items():
                values[f"{ability}/{key}"] = (f"{name} - {text}", value)
        result[row[1]] = values
    for row in catalog["items"]:
        if row[1] in items:
            result[row[1]] = item_values(items[row[1]])
    return result


def value_changes(before, after):
    """["<label> <old> → <new>", ...] for one hero or item, in the order the game data lists them."""
    lines = []
    for name in list(after) + [name for name in before if name not in after]:
        old, new = before.get(name, ("", None))[1], after.get(name, ("", None))[1]
        if old != new:
            label = (after.get(name) or before[name])[0]
            lines.append(f"{label} {'none' if old is None else old} → {'none' if new is None else new}")
    return lines


def entry_lines(lines):
    return {"lines": [shorten(text) for text in lines[:LINES_PER_ENTRY]], "more": max(0, len(lines) - LINES_PER_ENTRY)}


def collect(catalog, counters, patches, load_build=None):
    """The warnings for patches.json; without load_build, from the patch notes alone."""
    reviewed = counters.get("reviewed", {})
    since = reviewed.get("date", "")
    per_hero = {key: value if isinstance(value, dict) else {"date": value}
                for key, value in reviewed.get("heroes", {}).items()}
    names = name_index(catalog)
    heroes = {row[1] for row in catalog["heroes"]}
    result = []
    named = set()  # everything a patch note after the review names, warned about or not
    for patch in sorted(patches, key=lambda entry: entry.get("pub_date") or ""):
        published = (patch.get("pub_date") or "")[:10]
        if not published or published <= since:
            continue
        found = changes(patch, names)
        if not found:
            continue  # not a gameplay patch
        named.update(found)
        entry = {"title": " ".join((patch.get("title") or "").split()), "date": published, "heroes": {}, "items": {}}
        for key, lines in found.items():
            if key in heroes and per_hero.get(key, {}).get("date", "") >= published:
                continue  # this hero was checked after the patch
            entry["heroes" if key in heroes else "items"][key] = entry_lines(lines)
        if entry["heroes"] or entry["items"]:
            result.append(entry)
    notes = {"reviewed": since, "patches": result}

    base, current = reviewed.get("build"), catalog.get("clientVersion")
    if load_build and base and current:
        warned = {key for patch in result for group in ("heroes", "items") for key in patch[group]}
        builds = {}

        def values(build):
            if build not in builds:
                builds[build] = snapshot(load_build(build), catalog)
            return builds[build]

        silent = {"title": UNANNOUNCED, "build": current, "heroes": {}, "items": {}}
        for key in [row[1] for row in catalog["heroes"]] + [row[1] for row in catalog["items"]]:
            if key in warned:
                continue  # the patch notes already explain what changed
            start = per_hero.get(key, {}).get("build") if key in heroes else None
            if start is None and key in per_hero and key in named:
                continue  # checked after the notes that name it, against a build nobody recorded
            before, after = values(start or base).get(key), values(current).get(key)
            lines = value_changes(before, after) if before and after else []  # new heroes and items have nothing to review
            if lines:
                silent["heroes" if key in heroes else "items"][key] = entry_lines(lines)
        notes = {"reviewed": since, "reviewedBuild": base, "build": current, "patches": result}
        if silent["heroes"] or silent["items"]:
            result.append(silent)
    return notes


def format_notes(notes):
    """One line per hero or item keeps the file reviewable in diffs."""
    header = [f'  "{key}": {json.dumps(notes[key])},' for key in ("reviewed", "reviewedBuild", "build") if key in notes]
    parts = ["{\n" + "\n".join(header) + '\n  "patches": [']
    patches = []
    for patch in notes["patches"]:
        body = [f'      "title": {json.dumps(patch["title"], ensure_ascii=False)},']
        body += [f'      "{key}": {json.dumps(patch[key])},' for key in ("date", "build") if key in patch]
        for group in ("heroes", "items"):
            rows = ",\n".join(f'        "{key}": {json.dumps(value, ensure_ascii=False)}'
                              for key, value in sorted(patch[group].items()))
            body.append(f'      "{group}": ' + (f"{{\n{rows}\n      }}" if rows else "{}") + ("," if group == "heroes" else ""))
        patches.append("    {\n" + "\n".join(body) + "\n    }")
    parts.append(",\n".join(patches))
    return "\n".join(parts) + "\n  ]\n}\n"


def main():
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--output", type=Path, default=ROOT / "src" / "patches.json")
    parser.add_argument("--notes-only", action="store_true", help="skip the game-data comparison")
    args = parser.parse_args()
    catalog = json.loads((ROOT / "src" / "catalog.json").read_text(encoding="utf-8"))
    counters = json.loads((ROOT / "src" / "counters.json").read_text(encoding="utf-8"))
    try:
        notes = collect(catalog, counters, fetch_patches(), None if args.notes_only else fetch_build)
        with tempfile.TemporaryDirectory(dir=args.output.parent) as temporary_dir:
            pending = Path(temporary_dir) / args.output.name
            pending.write_text(format_notes(notes), encoding="utf-8")
            pending.replace(args.output)
    except (OSError, ValueError, KeyError) as error:
        parser.exit(1, f"Patch notes update failed: {error}\n")
    noted = [patch for patch in notes["patches"] if "date" in patch]
    silent = [patch for patch in notes["patches"] if "date" not in patch]
    heroes = sorted({key for patch in noted for key in patch["heroes"]})
    items = sorted({key for patch in noted for key in patch["items"]})
    print(f"{args.output} | {len(noted)} patches since {notes['reviewed'] or 'the beginning'} | "
          f"{len(heroes)} heroes, {len(items)} items to review")
    if "build" in notes:
        changed = [key for patch in silent for group in ("heroes", "items") for key in patch[group]]
        print(f"Game data, build {notes['reviewedBuild']} → {notes['build']}: "
              + (f"changed without patch notes: {', '.join(changed)}" if changed else "no changes without patch notes"))


if __name__ == "__main__":
    main()
