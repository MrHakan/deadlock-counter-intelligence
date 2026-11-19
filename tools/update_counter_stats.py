"""Measure how much each item's win rate rises against each hero, from deadlock-api match data.

For an item I and an enemy hero H the lift is

    win rate of I's buyers facing H  -  win rate of I's buyers overall  -  shift(H)

where shift(H) is how much every buyer's win rate moves when H is on the enemy
team. Subtracting the item's overall win rate removes buyer bias (late items
are bought by players who are already ahead); subtracting shift(H) removes the
hero's overall strength. The lift is then taken relative to the purchase-weighted
average lift of the same tier, which removes the hero's power curve (against a
hero who falls off late, every late item looks good). Lifts are stored in
hundredths of a win-rate point.
"""

import argparse
from datetime import datetime, timedelta, timezone
import json
import math
from pathlib import Path
import tempfile
import time
from urllib.parse import urlencode
from urllib.request import Request, urlopen

ROOT = Path(__file__).resolve().parents[1]
API = "https://api.deadlock-api.com/v1/analytics/item-stats"
MIN_MATCHES = 20000
MIN_Z = 2.0
PICKS_PER_HERO = 8
TIERS = (800, 1600, 3200, 6400)


def fetch(params, cache=None, attempts=4):
    cached = cache / ("items-" + "-".join(f"{key}={value}" for key, value in sorted(params.items())) + ".json") \
        if cache else None
    if cached and cached.is_file():
        return json.loads(cached.read_text(encoding="utf-8"))
    rows = download(params, attempts)
    if cached:
        cached.parent.mkdir(parents=True, exist_ok=True)
        cached.write_text(json.dumps(rows), encoding="utf-8")
        time.sleep(0.4)
    return rows


def download(params, attempts):
    request = Request(f"{API}?{urlencode(params)}", headers={"User-Agent": "deadlock-counter-intelligence"})
    for attempt in range(attempts):
        try:
            with urlopen(request, timeout=60) as response:
                rows = json.load(response)
            if not isinstance(rows, list) or not rows:
                raise ValueError(f"No item stats returned for {params}")
            return rows
        except (OSError, ValueError):
            if attempt == attempts - 1:
                raise
            time.sleep(2 ** attempt)


def pooled(rows):
    matches = sum(row["matches"] for row in rows.values())
    return sum(row["wins"] for row in rows.values()) / matches if matches else 0.5


def lifts(base, versus, tiers, min_matches=MIN_MATCHES, min_z=MIN_Z):
    """Return {item_id: (lift in hundredths of a point, matches)} for one enemy hero.

    Each lift is measured against the other items of the same tier, because items bought at
    the same stage of a match compete for the same souls and share the same timing bias.
    """
    shift = pooled(versus) - pooled(base)
    raw = {}
    for item_id, row in versus.items():
        overall = base.get(item_id)
        if item_id not in tiers or not overall or row["matches"] < min_matches or overall["matches"] < min_matches:
            continue
        rate = row["wins"] / row["matches"]
        baseline = overall["wins"] / overall["matches"]
        error = math.sqrt(rate * (1 - rate) / row["matches"] + baseline * (1 - baseline) / overall["matches"])
        raw[item_id] = (rate - baseline - shift, error, row["matches"])

    weights = {}
    for item_id, (lift, _, matches) in raw.items():
        total = weights.setdefault(tiers[item_id], [0.0, 0])
        total[0] += lift * matches
        total[1] += matches
    result = {}
    for item_id, (lift, error, matches) in raw.items():
        total = weights[tiers[item_id]]
        relative = lift - total[0] / total[1]
        result[item_id] = (round(relative * 10000) if abs(relative) >= min_z * error else 0, matches)
    return result


def compute(catalog, counters, base_rows, hero_rows, window, min_matches=MIN_MATCHES, min_z=MIN_Z):
    items = {row[0]: row[1] for row in catalog["items"]}
    tiers = {row[0]: row[4] for row in catalog["items"] if row[4] in TIERS}
    heroes = {row[0]: row[1] for row in catalog["heroes"]}
    base = {row["item_id"]: row for row in base_rows if row["item_id"] in items}
    measured = {}
    for hero_id, rows in sorted(hero_rows.items()):
        versus = {row["item_id"]: row for row in rows if row["item_id"] in items}
        measured[heroes[hero_id]] = {items[item_id]: value for item_id, value in
                                     lifts(base, versus, tiers, min_matches, min_z).items()}

    # Columns: every reference counter item plus each hero's strongest measured counters.
    columns = {item for picks in counters["heroes"].values() for item in picks}
    for values in measured.values():
        best = sorted((lift, item) for item, (lift, _) in values.items() if lift > 0)[::-1]
        columns.update(item for _, item in best[:PICKS_PER_HERO])
    columns = sorted(columns)
    table = {hero: [values[item][0] if item in values else None for item in columns]
             for hero, values in sorted(measured.items())}
    return {
        "source": "deadlock-api /v1/analytics/item-stats (ranked and unranked, all ranks)",
        "window": window,
        "minMatches": min_matches,
        "unit": "hundredths of a win-rate point; 0 = not significant, null = too few purchases",
        "items": columns,
        "heroes": table,
    }, measured


def report(catalog, counters, stats, measured):
    names = {row[1]: row[2] for row in catalog["items"]}
    heroes = {row[1]: row[2] for row in catalog["heroes"]}

    def points(value):
        return "n/a" if value is None else f"{value / 100:+.2f}"

    lines = [
        "# Counter item match data",
        "",
        f"Measured from deadlock-api match data, {stats['window']['from']} to {stats['window']['to']}, "
        "ranked and unranked matches at all ranks. Values are the change in the buyer's win rate, in "
        "percentage points, when the hero is on the enemy team. They are corrected for the hero's overall "
        "strength and for the item's usual win rate, and compared with other items of the same price. "
        "**0.00** means the change was not statistically significant; **n/a** means fewer than "
        f"{stats['minMatches']:,} purchases against that hero.",
        "",
        "Read negative values with care. Defensive counters are often bought by players who are already "
        "losing to that hero, which pulls their numbers down. A negative value means the match data does not "
        "back the pick, not that the item never works.",
        "",
        "Generated by `tools/update_counter_stats.py`.",
        "",
    ]
    positive = negative = flat = missing = 0
    sections = []
    for hero, picks in sorted(counters["heroes"].items(), key=lambda entry: heroes[entry[0]]):
        values = measured.get(hero, {})
        reference = []
        for item in picks:
            value = values.get(item, (None, 0))[0]
            if value is None:
                missing += 1
            elif value > 0:
                positive += 1
            elif value < 0:
                negative += 1
            else:
                flat += 1
            reference.append(f"{names[item]} {points(value)}")
        best = sorted(((lift, item) for item, (lift, _) in values.items() if lift > 0), reverse=True)[:3]
        sections.append(f"| {heroes[hero]} | {' · '.join(reference)} | "
                        f"{' · '.join(f'{names[item]} {points(lift)}' for lift, item in best) or '—'} |")
    total = positive + negative + flat + missing
    lines += [
        f"Of the {total} reference picks, {positive} raise the buyer's win rate measurably, {flat} show no "
        f"significant change, {negative} lower it, and {missing} lack data.",
        "",
        "| Enemy hero | Reference picks (lift) | Strongest in match data |",
        "| --- | --- | --- |",
    ] + sections
    return "\n".join(lines) + "\n"


def write_atomic(path, text):
    with tempfile.TemporaryDirectory(dir=path.parent) as temporary_dir:
        pending = Path(temporary_dir) / path.name
        pending.write_text(text, encoding="utf-8")
        pending.replace(path)


def main():
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--days", type=int, default=30, help="Window length, ending today (UTC)")
    parser.add_argument("--stats", type=Path, default=ROOT / "src" / "stats.json")
    parser.add_argument("--report", type=Path, default=ROOT / "docs" / "counter-data.md")
    parser.add_argument("--cache", type=Path, help="Directory for raw API responses, reused when present")
    args = parser.parse_args()
    catalog = json.loads((ROOT / "src" / "catalog.json").read_text(encoding="utf-8"))
    counters = json.loads((ROOT / "src" / "counters.json").read_text(encoding="utf-8"))
    end = datetime.now(timezone.utc).replace(hour=0, minute=0, second=0, microsecond=0)
    start = end - timedelta(days=args.days)
    params = {"min_unix_timestamp": int(start.timestamp()), "max_unix_timestamp": int(end.timestamp())}
    window = {"from": start.date().isoformat(), "to": end.date().isoformat()}
    try:
        base = fetch(params, args.cache)
        hero_rows = {hero_id: fetch(dict(params, enemy_hero_ids=hero_id), args.cache)
                     for hero_id, *_ in catalog["heroes"]}
        stats, measured = compute(catalog, counters, base, hero_rows, window)
        compact = ",\n".join(f'    "{hero}": {json.dumps(values)}' for hero, values in stats["heroes"].items())
        header = {key: value for key, value in stats.items() if key != "heroes"}
        text = json.dumps(header, indent=2, ensure_ascii=False)[:-2] + ',\n  "heroes": {\n' + compact + "\n  }\n}\n"
        args.report.parent.mkdir(parents=True, exist_ok=True)
        write_atomic(args.stats, text)
        write_atomic(args.report, report(catalog, counters, stats, measured))
    except (ValueError, OSError, KeyError) as error:
        parser.exit(1, f"Counter stats update failed: {error}\n")
    print(f"{args.stats} | {window['from']} to {window['to']} | {len(stats['items'])} items x {len(stats['heroes'])} heroes")


if __name__ == "__main__":
    main()
