#!/usr/bin/env python3
"""Verify a sealed Settlement v3 export and prepare retention-only import CSVs."""

import argparse
import csv
import hashlib
import json
import os
import re
from datetime import UTC, datetime, timedelta
from pathlib import Path
from uuid import UUID


SCHEMA = "seeding.evidence.v3"
WINDOW_FIELDS = (
    "schema_version", "window_start", "window_end", "closure_delay_seconds",
    "max_interval_credit_seconds", "announce_source_stream", "announce_fence_sequence",
    "announce_fence_received_at", "selected_snapshot_id", "selected_snapshot_sequence",
    "selected_snapshot_observed_at", "snapshot_fence_id", "snapshot_fence_sequence",
    "snapshot_fence_observed_at",
)
WINDOW_TIMES = {
    "window_start", "window_end", "announce_fence_received_at",
    "selected_snapshot_observed_at", "snapshot_fence_observed_at",
}


def digest(document):
    data = json.dumps(document, ensure_ascii=False, separators=(",", ":"))
    # Match encoding/json's default escaping used by Settlement's evidence codec.
    for char in "<>&\u2028\u2029":
        data = data.replace(char, f"\\u{ord(char):04x}")
    return hashlib.sha256(data.encode("utf-8")).hexdigest()


def timestamp(value):
    parsed = datetime.fromisoformat(value)
    if parsed.tzinfo is None:
        raise ValueError("a source timestamp is missing its UTC offset")
    return parsed.astimezone(UTC)


def canonical_time(value):
    value = timestamp(value)
    fraction = f".{value.microsecond:06d}".rstrip("0") if value.microsecond else ""
    return value.isoformat(timespec="seconds")[:-6] + fraction + "Z"


def check_item(item, start, end):
    if item["snapshot_seeders"] < 0 or item["snapshot_leechers"] < 0 or item["raw_uploaded"] < 0:
        raise ValueError(f"invalid activity counters in {start.isoformat()}")
    ranges = item["ranges"]
    if (item["torrent_id"] < 1 or UUID(item["user_id"]).int == 0 or not ranges
            or item["source_count"] < 1
            or not re.fullmatch(r"[0-9a-f]{64}", item["source_commitment"])
            or not re.fullmatch(r"[0-9a-f]{40}", item["info_hash_v1"])):
        raise ValueError(f"invalid activity proof in {start.isoformat()}")
    duration = timedelta()
    previous_end = None
    canonical_ranges = []
    for first, last in ranges:
        lower, upper = timestamp(first), timestamp(last)
        if not start <= lower < upper <= end or (previous_end is not None and previous_end >= lower):
            raise ValueError(f"invalid activity ranges in {start.isoformat()}")
        duration += upper - lower
        previous_end = upper
        canonical_ranges.append({"start": canonical_time(first), "end": canonical_time(last)})
    if item["active_seconds"] != duration // timedelta(seconds=1):
        raise ValueError(f"duration mismatch in {start.isoformat()}")
    document = {
        "schema": SCHEMA,
        **{field: item[field] for field in (
            "user_id", "torrent_id", "info_hash_v1", "active_seconds", "raw_uploaded",
            "snapshot_seeders", "snapshot_leechers", "source_count", "source_commitment",
        )},
        "ranges": canonical_ranges,
    }
    if digest(document) != item["evidence_sha256"]:
        raise ValueError(f"item digest mismatch in {start.isoformat()}")


def check_window(window, items):
    start, end = timestamp(window["window_start"]), timestamp(window["window_end"])
    if (start.minute or start.second or start.microsecond or end - start != timedelta(hours=1)
            or window["schema_version"] != SCHEMA or len(items) != window["item_count"]):
        raise ValueError(f"invalid sealed window at {start.isoformat()}")
    document = {field: canonical_time(window[field]) if field in WINDOW_TIMES else window[field]
                for field in WINDOW_FIELDS}
    document["items"] = items
    if digest(document) != window["evidence_sha256"]:
        raise ValueError(f"window digest mismatch at {start.isoformat()}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("source", type=Path)
    parser.add_argument("output", type=Path, help="new directory for verified import files")
    parser.add_argument("--expected-windows", type=int, required=True)
    parser.add_argument("--operator-reference", required=True)
    args = parser.parse_args()
    if not 1 <= len(args.operator_reference.strip()) <= 256 or args.expected_windows < 1:
        parser.error("a valid operator reference and a positive expected window count are required")
    with args.source.open("rb") as source:
        artifact_hash = hashlib.file_digest(source, "sha256").hexdigest()
    os.umask(0o077)
    args.output.mkdir(mode=0o700)
    source_hash = hashlib.sha256()
    hours, entries_count = 0, 0
    with (
        args.source.open("rb") as source,
        (args.output / "windows.csv").open("x", newline="", encoding="utf-8") as window_file,
        (args.output / "items.csv").open("x", newline="", encoding="utf-8") as item_file,
    ):
        windows = csv.writer(window_file, lineterminator="\n")
        entries = csv.writer(item_file, lineterminator="\n")

        def finish_window():
            check_window(window, items)
            windows.writerow((start, canonical_time(window["window_end"]), SCHEMA,
                              "\\x" + window["evidence_sha256"], len(items),
                              "\\x" + artifact_hash, args.operator_reference))

        window, start = None, None
        for line in source:
            source_hash.update(line)
            record = json.loads(line)
            record_hour = canonical_time(record["window_start"])
            if record["kind"] == "window":
                if window is not None:
                    if record_hour <= start:
                        raise ValueError(f"source windows are out of order at {record_hour}")
                    finish_window()
                window, start, items, previous_key = record, record_hour, [], None
                start_time, end_time = timestamp(start), timestamp(record["window_end"])
                hours += 1
            elif record["kind"] == "item" and record_hour == start:
                key = UUID(record["user_id"]).int, record["torrent_id"]
                if previous_key is not None and key <= previous_key:
                    raise ValueError(f"items are duplicated or out of order at {start}")
                previous_key = key
                check_item(record, start_time, end_time)
                items.append({field: record[field] for field in ("user_id", "torrent_id", "evidence_sha256")})
                entries.writerow((start, record["user_id"], record["torrent_id"],
                                  record["active_seconds"], "\\x" + record["evidence_sha256"]))
                entries_count += 1
            else:
                raise ValueError(f"unexpected activity record at {record_hour}")
        if window is not None:
            finish_window()
    if hours != args.expected_windows or source_hash.hexdigest() != artifact_hash:
        raise ValueError(f"source changed or window count was not exact: exported {hours}")
    summary = json.dumps({"hours": hours, "items": entries_count, "source_sha256": artifact_hash})
    with (args.output / "verified.json").open("x", encoding="utf-8") as manifest:
        manifest.write(summary + "\n")
    print(summary)


if __name__ == "__main__":
    main()
