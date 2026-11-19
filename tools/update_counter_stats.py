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

The window starts at the latest gameplay patch, found in the patch notes as the newest post
that names an item or a hero followed by a colon ("Healbane: ..."), so the numbers describe the
items as they are now. Lane counters repeat the measurement for buyers in the same lane as the
enemy hero, counting only items bought in the first ten minutes.
"""

import argparse
from datetime import datetime, timedelta, timezone
import html
import json
import math
import re
from pathlib import Path
import tempfile
import time
from urllib.parse import urlencode
from urllib.request import Request, urlopen

ROOT = Path(__file__).resolve().parents[1]
API = "https://api.deadlock-api.com/v1/analytics/item-stats"
PATCHES = "https://api.deadlock-api.com/v2/patches"
MIN_MATCHES = 20000
LANE_MIN_MATCHES = 4000
LANE_SECONDS = 600
LANE_PICKS = 3
LANE_MAX_COST = 3200
MAX_WINDOW_DAYS = 45
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


def download(params, attempts, url=API):
    request = Request(f"{url}?{urlencode(params)}" if params else url, headers={"User-Agent": "deadlock-counter-intelligence"})
    for attempt in range(attempts):
        try:
            with urlopen(request, timeout=60) as response:
                rows = json.load(response)
            if not isinstance(rows, list) or not rows:
                raise ValueError(f"No rows returned for {url} {params}")
            return rows
        except (OSError, ValueError):
            if attempt == attempts - 1:
                raise
            time.sleep(2 ** attempt)


def latest_gameplay_patch(catalog, patches):
    """Return the newest patch-notes post that changes an item or a hero, or None."""
    names = sorted({row[2] for row in catalog["items"]} | {row[2] for row in catalog["heroes"]}, key=len, reverse=True)
    change = re.compile(r"^\s*[-•*]?\s*(?:" + "|".join(map(re.escape, names)) + r")\s*:", re.IGNORECASE | re.MULTILINE)
    for patch in sorted(patches, key=lambda entry: entry.get("pub_date") or "", reverse=True):
        text = html.unescape(re.sub(r"<[^>]+>", "\n", patch.get("content") or ""))
        if patch.get("pub_date") and change.search(text):
            published = datetime.fromisoformat(patch["pub_date"].replace("Z", "+00:00"))
            return {"title": " ".join((patch.get("title") or "").split()), "date": published.date().isoformat(),
                    "timestamp": int(published.timestamp())}
    return None


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


def compute(catalog, counters, base_rows, hero_rows, window, min_matches=MIN_MATCHES, min_z=MIN_Z,
            lane_base_rows=None, lane_rows=None, patch=None):
    items = {row[0]: row[1] for row in catalog["items"]}
    tiers = {row[0]: row[4] for row in catalog["items"] if row[4] in TIERS}
    costs = {row[0]: row[4] for row in catalog["items"]}
    heroes = {row[0]: row[1] for row in catalog["heroes"]}
    base = {row["item_id"]: row for row in base_rows if row["item_id"] in items}
    measured = {}
    for hero_id, rows in sorted(hero_rows.items()):
        versus = {row["item_id"]: row for row in rows if row["item_id"] in items}
        measured[heroes[hero_id]] = {items[item_id]: value for item_id, value in
                                     lifts(base, versus, tiers, min_matches, min_z).items()}

    # Lane counters: early purchases by players laning against the hero.
    lane = {}
    if lane_base_rows and lane_rows:
        lane_base = {row["item_id"]: row for row in lane_base_rows if row["item_id"] in items}
        early_tiers = {item_id: tier for item_id, tier in tiers.items() if costs[item_id] <= LANE_MAX_COST}
        for hero_id, rows in sorted(lane_rows.items()):
            versus = {row["item_id"]: row for row in rows if row["item_id"] in items}
            values = lifts(lane_base, versus, early_tiers, LANE_MIN_MATCHES, min_z)
            best = sorted(((lift, items[item_id]) for item_id, (lift, _) in values.items() if lift > 0), reverse=True)
            lane[heroes[hero_id]] = [[item, lift] for lift, item in best[:LANE_PICKS]]

    # Columns: every reference counter item plus each hero's strongest measured counters.
    columns = {item for picks in counters["heroes"].values() for item in picks}
    for values in measured.values():
        best = sorted((lift, item) for item, (lift, _) in values.items() if lift > 0)[::-1]
        columns.update(item for _, item in best[:PICKS_PER_HERO])
    columns = sorted(columns)
    table = {hero: [values[item][0] if item in values else None for item in columns]
             for hero, values in sorted(measured.items())}
    # Typical buy minute of every item the HUD can show.
    shown = set(columns) | {item for picks in lane.values() for item, _ in picks}
    ids = {key: item_id for item_id, key in items.items()}
    minutes = {item: round(base[ids[item]]["avg_buy_time_s"] / 60) for item in sorted(shown)
               if ids.get(item) in base and base[ids[item]].get("avg_buy_time_s")}
    stats = {
        "source": "deadlock-api /v1/analytics/item-stats (ranked and unranked, all ranks)",
        "window": window,
        "minMatches": min_matches,
        "unit": "hundredths of a win-rate point; 0 = not significant, null = too few purchases",
        "items": columns,
        "minutes": minutes,
        "lane": lane,
        "heroes": table,
    }
    if patch:
        stats["patch"] = {"title": patch["title"], "date": patch["date"]}
    return stats, measured


def report(catalog, counters, stats, measured):
    names = {row[1]: row[2] for row in catalog["items"]}
    heroes = {row[1]: row[2] for row in catalog["heroes"]}

    def points(value):
        return "n/a" if value is None else f"{value / 100:+.2f}"

    since = ""
    if stats.get("patch"):
        since = f" (since the {stats['patch']['title']} patch)"
    lines = [
        "# Counter item match data",
        "",
        f"Measured from deadlock-api match data, {stats['window']['from']} to {stats['window']['to']}{since}, "
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
    if stats.get("lane"):
        lines += [
            "",
            "## Lane counters",
            "",
            f"Items bought in the first {LANE_SECONDS // 60} minutes by players in the same lane as the hero, "
            "costing at most 3,200 souls, measured the same way against other early purchases of the same price.",
            "",
            "| Lane opponent | Best early items (lift) |",
            "| --- | --- |",
        ]
        for hero, picks in sorted(stats["lane"].items(), key=lambda entry: heroes[entry[0]]):
            lines.append(f"| {heroes[hero]} | {' · '.join(f'{names[item]} {points(lift)}' for item, lift in picks) or '—'} |")
    return "\n".join(lines) + "\n"


def format_stats(stats):
    """One line per hero keeps the file reviewable in diffs."""
    def rows(mapping):
        return ",\n".join(f'    "{key}": {json.dumps(value)}' for key, value in mapping.items())
    header = {key: value for key, value in stats.items() if key not in ("heroes", "lane", "minutes")}
    text = json.dumps(header, indent=2, ensure_ascii=False)[:-2]
    text += ',\n  "minutes": ' + json.dumps(stats.get("minutes", {}), separators=(", ", ": "))
    text += ',\n  "lane": {\n' + rows(stats.get("lane", {})) + "\n  }"
    text += ',\n  "heroes": {\n' + rows(stats["heroes"]) + "\n  }\n}\n"
    return text


def write_atomic(path, text):
    with tempfile.TemporaryDirectory(dir=path.parent) as temporary_dir:
        pending = Path(temporary_dir) / path.name
        pending.write_text(text, encoding="utf-8")
        pending.replace(path)


def main():
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--days", type=int, help="Use a window of this many days instead of starting at the latest "
                        "gameplay patch")
    parser.add_argument("--stats", type=Path, default=ROOT / "src" / "stats.json")
    parser.add_argument("--report", type=Path, default=ROOT / "docs" / "counter-data.md")
    parser.add_argument("--cache", type=Path, help="Directory for raw API responses, reused when present")
    args = parser.parse_args()
    catalog = json.loads((ROOT / "src" / "catalog.json").read_text(encoding="utf-8"))
    counters = json.loads((ROOT / "src" / "counters.json").read_text(encoding="utf-8"))
    end = datetime.now(timezone.utc).replace(hour=0, minute=0, second=0, microsecond=0)
    try:
        patch = None
        if args.days:
            start = end - timedelta(days=args.days)
        else:
            patch = latest_gameplay_patch(catalog, download(None, 4, PATCHES))
            if not patch:
                raise ValueError("no gameplay patch found in the patch notes; pass --days")
            start = max(datetime.fromtimestamp(patch["timestamp"], timezone.utc), end - timedelta(days=MAX_WINDOW_DAYS))
        if (end - start).days < 3:
            print(f"Warning: the window has only {(end - start).days} days of matches; expect fewer significant results")
        params = {"min_unix_timestamp": int(start.timestamp()), "max_unix_timestamp": int(end.timestamp())}
        window = {"from": start.date().isoformat(), "to": end.date().isoformat()}
        base = fetch(params, args.cache)
        hero_rows = {hero_id: fetch(dict(params, enemy_hero_ids=hero_id), args.cache)
                     for hero_id, *_ in catalog["heroes"]}
        early = dict(params, max_bought_at_s=LANE_SECONDS)
        lane_base = fetch(early, args.cache)
        lane_rows = {hero_id: fetch(dict(early, enemy_hero_ids=hero_id, same_lane_filter="true"), args.cache)
                     for hero_id, *_ in catalog["heroes"]}
        stats, measured = compute(catalog, counters, base, hero_rows, window, lane_base_rows=lane_base,
                                  lane_rows=lane_rows, patch=patch)
        text = format_stats(stats)
        args.report.parent.mkdir(parents=True, exist_ok=True)
        write_atomic(args.stats, text)
        write_atomic(args.report, report(catalog, counters, stats, measured))
    except (ValueError, OSError, KeyError) as error:
        parser.exit(1, f"Counter stats update failed: {error}\n")
    since = f" (since {patch['title']})" if patch else ""
    print(f"{args.stats} | {window['from']} to {window['to']}{since} | {len(stats['items'])} items x "
          f"{len(stats['heroes'])} heroes | lane picks for {sum(1 for picks in stats['lane'].values() if picks)} heroes")


if __name__ == "__main__":
    main()
