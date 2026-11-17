# Changelog

This file records changes from the first public preview onward. Until a tagged release exists, the work below is unreleased.

## Unreleased

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
