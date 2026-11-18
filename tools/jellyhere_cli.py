"""Small local CLI for a self-hosted JellyHere relay."""

from __future__ import annotations

import argparse
import json
import os
import re
import shutil
import subprocess
import sys

from orb_adb_connect import OrbClient, OrbError, load_token, load_url, main as adb_main


VERSION = "0.1.0"


def doctor() -> int:
    try:
        url = load_url()
        token = load_token()
        client = OrbClient(url, token, "")
        items = client.call("device_status")
        status = next((item.get("text", "[]") for item in items if item.get("type") == "text"), "[]")
        devices = json.loads(status)
        if not isinstance(devices, list):
            raise OrbError("device_status returned an unexpected response")
    except (OrbError, ValueError) as error:
        print(f"jellyhere doctor: {error}", file=sys.stderr)
        return 1
    print(f"MCP reachable: {url}")
    print(f"Online devices: {len(devices)}")
    for device in devices:
        print(f"  {device.get('id', '?')}: {'paused' if device.get('paused') else 'online'}"
              f" ({device.get('manufacturer', '')} {device.get('model', '')})")
    if shutil.which("adb"):
        try:
            result = subprocess.run(["adb", "version"], text=True, capture_output=True, check=False, timeout=10)
            print("Local adb: ready" if result.returncode == 0 else "Local adb: unavailable")
        except (OSError, subprocess.TimeoutExpired):
            print("Local adb: unavailable")
    else:
        print("Local adb: unavailable (only needed for adb connect)")
    return 0


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="jellyhere", description="JellyHere device companion CLI")
    parser.add_argument("--version", action="version", version=f"jellyhere {VERSION}")
    parser.add_argument("--profile", help="use ~/.config/jellyhere/profiles/NAME for this command")
    commands = parser.add_subparsers(dest="command", required=True)
    commands.add_parser("doctor", help="check MCP configuration and device availability")
    device = commands.add_parser("device", help="inspect connected devices")
    device_actions = device.add_subparsers(dest="device_command", required=True)
    device_actions.add_parser("list", help="list online devices without controlling them")
    adb = commands.add_parser("adb", help="wireless debugging helper")
    adb_actions = adb.add_subparsers(dest="adb_command", required=True)
    connect = adb_actions.add_parser("connect", help="open wireless debugging and connect local adb")
    connect.add_argument("--device", help="device ID; required if multiple phones are online")
    connect.add_argument("--no-adb", action="store_true", help="only enable and print the endpoint")
    args = parser.parse_args(argv)
    if args.profile is not None and not re.fullmatch(r"[A-Za-z0-9_-]{1,64}", args.profile):
        parser.error("profile name must contain only letters, numbers, _ or -")
    old_profile = os.environ.get("JELLYHERE_PROFILE")
    if args.profile is not None:
        os.environ["JELLYHERE_PROFILE"] = args.profile
    try:
        if args.command == "doctor":
            return doctor()
        if args.command == "device":
            return adb_main(["--list"], command_name="jellyhere device list")
        connect_args = []
        if args.device:
            connect_args.extend(["--device", args.device])
        if args.no_adb:
            connect_args.append("--no-adb")
        return adb_main(connect_args, command_name="jellyhere adb connect")
    finally:
        if args.profile is not None:
            if old_profile is None:
                os.environ.pop("JELLYHERE_PROFILE", None)
            else:
                os.environ["JELLYHERE_PROFILE"] = old_profile


if __name__ == "__main__":
    raise SystemExit(main())
