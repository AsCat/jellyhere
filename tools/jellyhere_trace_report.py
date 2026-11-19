#!/usr/bin/env python3
"""Summarize JellyHere metadata logs without reading screen or input content.

Example:
  journalctl -u jellyhere-managed.service --since '10 minutes ago' -o cat |
      python3 tools/jellyhere_trace_report.py --client-ref 72aa14346e05
"""

from __future__ import annotations

import argparse
from collections import defaultdict
from datetime import datetime
import json
from pathlib import Path
import re
import sys


EVENT = re.compile(r"\b(mcp_request|device_call)\s+(.+)$")
TIMESTAMP = re.compile(r"(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2})")


def number(value: str | None) -> int | None:
    try:
        return int(value) if value is not None else None
    except ValueError:
        return None


def parse_line(line: str) -> dict | None:
    match = EVENT.search(line)
    if not match:
        return None
    fields = dict(part.split("=", 1) for part in match.group(2).split() if "=" in part)
    if not fields.get("trace"):
        return None
    duration = number(fields.get("total_ms"))
    started = number(fields.get("started_unix_ms"))
    if started is None:
        stamp = TIMESTAMP.search(line)
        if stamp:
            ended = int(datetime.strptime(stamp.group(1), "%Y/%m/%d %H:%M:%S").timestamp() * 1000)
            started = ended - (duration or 0)
    return {"kind": match.group(1), **fields, "duration_ms": duration, "started_ms": started}


def percentile(values: list[int], fraction: float) -> int | None:
    if not values:
        return None
    ordered = sorted(values)
    return ordered[int((len(ordered) - 1) * fraction)]


def summarize(lines: list[str], client_ref: str | None = None, device: str | None = None) -> dict:
    events = [event for line in lines if (event := parse_line(line)) is not None]
    requests = [event for event in events if event["kind"] == "mcp_request"]
    calls = [event for event in events if event["kind"] == "device_call"]
    if client_ref:
        requests = [event for event in requests if event.get("client_ref") == client_ref]
        traces = {event["trace"] for event in requests}
        calls = [event for event in calls if event["trace"] in traces]
    if device:
        calls = [event for event in calls if event.get("device") == device]
        traces = {event["trace"] for event in calls}
        requests = [event for event in requests if event["trace"] in traces]

    methods: dict[str, list[dict]] = defaultdict(list)
    for event in calls:
        methods[event.get("method", "unknown")].append(event)
    by_method = {}
    for method, group in sorted(methods.items()):
        timings = [value for item in group if (value := item["duration_ms"]) is not None]
        by_method[method] = {
            "count": len(group),
            "p50_ms": percentile(timings, 0.5),
            "p90_ms": percentile(timings, 0.9),
            "result_bytes": sum(number(item.get("result_bytes")) or 0 for item in group),
            "errors": sum(item.get("status") != "ok" for item in group),
        }

    timed = [item for item in requests if item["started_ms"] is not None and item["duration_ms"] is not None]
    elapsed = None
    if timed:
        elapsed = max(item["started_ms"] + item["duration_ms"] for item in timed) - min(item["started_ms"] for item in timed)
    grouped: dict[str, list[dict]] = defaultdict(list)
    for item in timed:
        grouped[item.get("client_ref", "-")].append(item)
    gaps = []
    for group in grouped.values():
        ordered = sorted(group, key=lambda item: item["started_ms"])
        gaps.extend(max(0, next_item["started_ms"] - item["started_ms"] - item["duration_ms"])
                    for item, next_item in zip(ordered, ordered[1:]))
    mcp_timings = [item["duration_ms"] for item in requests if item["duration_ms"] is not None]
    return {
        "mcp_requests": len(requests),
        "device_calls": len(calls),
        "elapsed_ms": elapsed,
        "mcp_p50_ms": percentile(mcp_timings, 0.5),
        "mcp_p90_ms": percentile(mcp_timings, 0.9),
        "between_calls_p50_ms": percentile(gaps, 0.5),
        "between_calls_p90_ms": percentile(gaps, 0.9),
        "screen_with_image": sum(item.get("image_requested") == "true" for item in calls if item.get("method") == "get_screen"),
        "screen_without_image": sum(item.get("image_requested") == "false" for item in calls if item.get("method") == "get_screen"),
        "by_method": by_method,
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("input", nargs="?", default="-", help="journalctl output file; default: stdin")
    parser.add_argument("--client-ref", help="12-character client reference from mcp_request logs")
    parser.add_argument("--device", help="device ID to include")
    parser.add_argument("--json", action="store_true", help="machine-readable report")
    args = parser.parse_args()
    lines = sys.stdin.readlines() if args.input == "-" else Path(args.input).read_text().splitlines()
    report = summarize(lines, args.client_ref, args.device)
    if report["mcp_requests"] == 0 and report["device_calls"] == 0:
        print("No matching JellyHere metadata logs found.", file=sys.stderr)
        return 2
    if args.json:
        print(json.dumps(report, ensure_ascii=False, indent=2))
        return 0
    print(f"MCP requests: {report['mcp_requests']}  Device calls: {report['device_calls']}")
    print(f"Elapsed: {report['elapsed_ms']} ms  MCP p50/p90: {report['mcp_p50_ms']}/{report['mcp_p90_ms']} ms")
    print(f"Between-request gap p50/p90: {report['between_calls_p50_ms']}/{report['between_calls_p90_ms']} ms")
    print(f"Screens: image {report['screen_with_image']}, tree only {report['screen_without_image']}")
    for method, item in report["by_method"].items():
        print(f"  {method:26} n={item['count']:3} p50/p90={item['p50_ms']}/{item['p90_ms']} ms "
              f"bytes={item['result_bytes']} errors={item['errors']}")
    print("Between-request gaps include agent reasoning and client/network time; they are not pure model latency.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
