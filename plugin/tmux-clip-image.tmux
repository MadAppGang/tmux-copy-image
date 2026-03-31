#!/usr/bin/env bash
# tmux-clip-image.tmux — TPM entry point for the tmux-clip-image plugin.
#
# This file is sourced by TPM on plugin installation and on tmux server start.
# It must be idempotent and fast (no slow operations).

PLUGIN_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPTS_DIR="${PLUGIN_DIR}/scripts"

# Read the configurable keybinding option, defaulting to "V".
bind_key="$(tmux show-option -gqv "@clip-image-key" 2>/dev/null)"
bind_key="${bind_key:-V}"

# Register the keybinding: prefix + <key> runs paste.sh.
tmux bind-key "${bind_key}" run-shell "${SCRIPTS_DIR}/paste.sh"

# Register session hooks to discover the rpaster Unix socket on each new
# session and on each client attach. This sets RPASTER_SOCKET in the tmux
# environment so paste.sh can use the socket without per-invocation probing.
tmux set-hook -g session-created "run-shell '${SCRIPTS_DIR}/find-socket.sh'"
tmux set-hook -g client-attached "run-shell '${SCRIPTS_DIR}/find-socket.sh'"
