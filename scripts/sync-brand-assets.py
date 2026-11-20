#!/usr/bin/env python3
"""Export the approved JellyHere icon to web, connector, and Android assets."""

import argparse
import shutil
import subprocess
import xml.etree.ElementTree as ET
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SOURCE = ROOT / "assets/brand/jellyhere-icon.svg"
SVG_NS = "http://www.w3.org/2000/svg"
ET.register_namespace("", SVG_NS)


def foreground_svg(source: bytes) -> bytes:
    icon = ET.fromstring(source)
    defs = icon.find(f"{{{SVG_NS}}}defs")
    mascot = icon.find(f"{{{SVG_NS}}}g[@id='mascot']")
    if defs is None or mascot is None:
        raise ValueError("brand SVG needs defs and mascot groups")
    canvas = ET.Element(f"{{{SVG_NS}}}svg", {"viewBox": "0 0 108 108"})
    canvas.append(defs)
    positioned = ET.SubElement(canvas, f"{{{SVG_NS}}}g", {"transform": "translate(6 6)"})
    positioned.append(mascot)
    return ET.tostring(canvas, encoding="utf-8", xml_declaration=True)


def android_icon_svg(source: bytes) -> bytes:
    """Keep the shared mascot, with a white launcher tile for blue wallpapers."""
    icon = ET.fromstring(source)
    background = icon.find(f"{{{SVG_NS}}}g[@id='background']")
    if background is None:
        raise ValueError("brand SVG needs a background group")
    tile = background.find(f"{{{SVG_NS}}}rect")
    if tile is None:
        raise ValueError("brand SVG needs a background tile")
    tile.set("fill", "#FFFFFF")
    for circle in background.findall(f"{{{SVG_NS}}}circle"):
        circle.set("fill", "#BDD3E9")
    return ET.tostring(icon, encoding="utf-8", xml_declaration=True)


def render(renderer: str, source: bytes, size: int) -> bytes:
    return subprocess.run(
        [renderer, "--width", str(size), "--height", str(size)],
        input=source, stdout=subprocess.PIPE, check=True,
    ).stdout


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--check", action="store_true", help="fail if any generated asset differs")
    args = parser.parse_args()
    renderer = shutil.which("rsvg-convert")
    if renderer is None:
        parser.error("rsvg-convert is required (librsvg2-bin on Linux)")
    source = SOURCE.read_bytes()
    foreground = foreground_svg(source)
    android_icon = android_icon_svg(source)
    outputs = {
        ROOT / "integrations/workbuddy/jellyhere/icon.svg": source,
        ROOT / "server/internal/api/brand-icon.svg": source,
        ROOT / "assets/brand/jellyhere-icon-android.svg": android_icon,
        ROOT / "android/app/src/main/res/drawable-nodpi/ic_launcher_foreground.png": render(renderer, foreground, 432),
    }
    for density, size in (("mdpi", 48), ("hdpi", 72), ("xhdpi", 96), ("xxhdpi", 144), ("xxxhdpi", 192)):
        outputs[ROOT / f"android/app/src/main/res/mipmap-{density}/ic_launcher.png"] = render(renderer, android_icon, size)
    for path, data in outputs.items():
        if args.check:
            if not path.is_file() or path.read_bytes() != data:
                raise SystemExit(f"outdated brand asset: {path.relative_to(ROOT)}")
        else:
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(data)
    print(f"{'Verified' if args.check else 'Updated'} {len(outputs)} brand assets")


if __name__ == "__main__":
    main()
