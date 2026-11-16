# JellyHere agent guide

JellyHere is an Android development-testing tool. The local command is `jellyhere`; run `jellyhere --help` to discover subcommands. Install it with `./scripts/install-cli.sh` if it is absent from `PATH`.

- `jellyhere doctor`: check local MCP configuration and list online devices.
- `jellyhere device list`: read online devices without operating them.
- `jellyhere adb connect --device <id>`: on the selected unlocked phone, attempt to open or enable wireless debugging, read its current endpoint, and connect local ADB. This changes a phone setting; use only when the user asks for Android development or ADB recovery. It cannot bridge ADB across unrelated networks. Some OEM settings pages do not expose a usable accessibility tree; stop on an error and let the user finish that step locally.
- `jellyhere adb connect --device <id> --no-adb`: return the wireless-debugging endpoint without running `adb connect`.

The CLI reads `~/.config/jellyhere/mcp-url` and `mcp-token`, or `JELLYHERE_MCP_URL` and `JELLYHERE_MCP_TOKEN`. Never print or commit tokens. Existing `orb-adb` remains a compatibility command.

For direct MCP use, call `device_status` first and specify `deviceId` when more than one phone is online. Read the screen before tapping an element and read it again after UI changes. Ask the user before entering sensitive data or taking a final order, purchase, or payment action. A phone-side pause rejects further remote requests.
