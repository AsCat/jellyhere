# Changelog

This file records changes from the first public preview onward.

## Unreleased

- Preview a white Android launcher tile for better contrast on blue wallpapers, while keeping the same star mascot and the blue website/WorkBuddy brand icon.

## 0.5.6 — 2026-11-20 (public preview)

- Unify the JellyHere brand icon around the warm yellow star mascot on fresh blue across Android launcher and notification, WorkBuddy connector package, and JellyHub web surfaces.
- Default fresh Android installations to the star mascot while preserving the user's existing mascot choice.

## 0.5.5 — 2026-11-20 (public preview)

- Redesign the JellyHub login and device dashboard with responsive cards, visible device counts, online status, and a separate add-device flow; add a managed-console shortcut to the Android floating menu.
- Include the AndroidX runtime classes required by the QR scanner, fixing a launch crash on Samsung.
- Let managed users name MCP clients in Chinese, keep device selections during dashboard refresh, and default-select a single paired phone.
- Add optional GitHub login to JellyHub, one-time QR device pairing, a signed Android APK download, and in-app QR scanning. Existing invite credentials continue to work.
- Add an invite-only managed web portal for device and MCP client provisioning and revocation.
- Add an optional invite-only managed pilot with tenant-isolated device hubs, per-device and per-client tokens, viewer/controller scopes, revocation, and local state persistence.
- Anchor the compact activity bubble beside the mascot, above it when space permits and below near the top edge.
- Dock the mascot immediately after a drag; move its activity bubble to a compact top-edge position with smaller text.
- Add the `jellyhere` CLI with `doctor`, `device list`, and `adb connect` commands; retain `orb-adb` compatibility.
- Refine the Android star mascot with rounded jelly highlights, deeper edge docking, action bubbles, and a direct pause control in the bubble and notification.
- Remove private deployment domain and instance-specific Nginx files from the public repository; use same-origin OpenAPI and self-hosting examples.
- Rename the project to JellyHere (果冻), with Android as the first device platform and iOS/desktop capability plans documented.
- Preserve the Android application ID, release signing identity, old service template, and `ORBBUDDY_*` configuration as migration aliases.
- Android client with mobile-network WSS relay, accessibility tree, on-demand screenshots, test gestures, and a local pause control.
- Go relay with Streamable HTTP MCP, temporary HTTPS authorization, a read-only browser viewer, and a public fixed-JSON health endpoint.
- Self-hosted deployment examples and an open-source release plan.
- Local wireless-debugging shortcut with on-device selector memory, and a desktop `orb-adb` command for Xiaomi and Samsung.
- Multiple trusted phones with explicit device selection, plus tree-only and smaller-image MCP screen reads.
- Optional compact MCP screen tree with original element paths and bounds to reduce text sent to agents.
- Explicit lock-state query and bounded screen-wake request; secure device authentication remains on the phone.
- Explicit temporary keep-screen-on testing session, with a 30-minute cap and local notification stop action.
- WorkBuddy OAuth connector with explicit device and permission selection; the connector is distributed through the WorkBuddy market.
- Optional direct App launch by package name and structured service-side task timing to help diagnose slow multi-step runs.
