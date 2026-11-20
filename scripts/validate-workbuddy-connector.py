#!/usr/bin/env python3
"""Validate and optionally package the JellyHere WorkBuddy connector."""
import argparse
import json
import pathlib
import re
import sys
import zipfile

ROOT = pathlib.Path(__file__).resolve().parents[1]
SOURCE = ROOT / "integrations" / "workbuddy" / "jellyhere"
REQUIRED = {"connector-meta.json", "mcp.json", "icon.svg", "skills/jellyhere/SKILL.md"}
MCP_URL = "https://jellyhub.yuanxuyinli.com/mcp"


def fail(message):
    print(f"ERROR: {message}", file=sys.stderr)
    raise SystemExit(1)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--package", action="store_true", help="also build a submission ZIP in dist/")
    args = parser.parse_args()
    missing = [name for name in sorted(REQUIRED) if not (SOURCE / name).is_file()]
    if missing:
        fail("missing files: " + ", ".join(missing))
    if (SOURCE / "icon.svg").read_bytes() != (ROOT / "assets/brand/jellyhere-icon.svg").read_bytes():
        fail("WorkBuddy icon must match the approved brand source")
    meta = json.loads((SOURCE / "connector-meta.json").read_text(encoding="utf-8"))
    if meta.get("source") != "jellyhere" or meta.get("type") != "mcp":
        fail("connector source/type mismatch")
    if not meta.get("examples_zh") or not meta.get("examples_en"):
        fail("Chinese and English examples are required")
    if not re.fullmatch(r"\d+\.\d+\.\d+", str(meta.get("version", ""))):
        fail("version must use semantic versioning")
    if meta.get("auth_mode") != "server-side":
        fail("auth_mode must be server-side")
    auth = meta.get("server_auth", {})
    oauth = auth.get("data", {}).get("mcp_oauth", {})
    if auth.get("auth_type") != "mcp_oauth" or oauth.get("server_url") != MCP_URL:
        fail("OAuth server configuration mismatch")
    if oauth.get("scopes") != "device.read device.control":
        fail("connector must request read and control scopes")
    servers = json.loads((SOURCE / "mcp.json").read_text(encoding="utf-8")).get("mcpServers", {})
    if list(servers) != ["jellyhere"] or servers["jellyhere"].get("type") != "streamableHttp" or servers["jellyhere"].get("url") != MCP_URL:
        fail("mcp.json must contain one matching HTTPS MCP server")
    skill = (SOURCE / "skills/jellyhere/SKILL.md").read_text(encoding="utf-8")
    match = re.search(r"^version:\s*(\S+)\s*$", skill, re.MULTILINE)
    if not match or match.group(1) != meta["version"]:
        fail("Skill version must match connector version")
    for path in SOURCE.rglob("*"):
        if path.is_file() and ("client_secret" in path.read_text(encoding="utf-8") or "Bearer ey" in path.read_text(encoding="utf-8")):
            fail("possible credential in " + str(path.relative_to(SOURCE)))
    if args.package:
        output = ROOT / "dist" / "workbuddy" / f"jellyhere-connector-{meta['version']}.zip"
        output.parent.mkdir(parents=True, exist_ok=True)
        with zipfile.ZipFile(output, "w", compression=zipfile.ZIP_DEFLATED) as archive:
            for name in sorted(REQUIRED):
                archive.write(SOURCE / name, name)
        print(output)
    print("WorkBuddy connector package is valid")


if __name__ == "__main__":
    main()
