#!/usr/bin/env bash
set -euo pipefail

repo_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
bin_dir=${JELLYHERE_BIN_DIR:-$HOME/.local/bin}
target="$bin_dir/jellyhere"
mkdir -p "$bin_dir"
if [[ -e $target && ! -L $target ]]; then
    echo "Refusing to replace non-symlink: $target" >&2
    exit 1
fi
ln -sfn "$repo_dir/tools/jellyhere" "$target"
echo "Installed $target"
echo "Run: jellyhere --help"
