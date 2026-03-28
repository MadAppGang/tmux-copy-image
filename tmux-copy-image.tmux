#!/usr/bin/env bash
# tmux-copy-image.tmux — TPM entry point.
#
# TPM expects a <plugin-name>.tmux file at the repository root.
# This delegates to the actual plugin entry point under plugin/.

CURRENT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${CURRENT_DIR}/plugin/tmux-clip-image.tmux"
