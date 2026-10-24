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

# Every job this plugin registers is backgrounded (-b), silent and forced to
# exit 0: a run-shell whose job prints, or exits non-zero, after the client that
# issued it has gone away makes tmux 3.7b segfault in the print path and takes
# every session with it. The session id is passed as $1, single-quoted for the
# job's `sh -c` (an id is `$N`), so the scripts address THIS session's
# environment explicitly: RPASTER_SOCKET is per SSH connection, hence per
# session, and a call without -t has no current session inside a hook job.
# (The plugin directory is single-quoted too, so it may not contain a `'`.)

# Register the keybinding: prefix + <key> runs paste.sh.
tmux bind-key "${bind_key}" run-shell -b "'${SCRIPTS_DIR}/paste.sh' '#{session_id}' >/dev/null 2>&1 || true"

# Discover the rpaster Unix socket on each new session and on each client
# attach; this sets RPASTER_SOCKET in the session's environment so paste.sh can
# use the socket without per-invocation probing. Slot [105] is this plugin's
# fixed index: an unindexed `set-hook -g` clears every other entry of the hook,
# and an indexed one replaces itself on reload instead of piling up.
tmux set-hook -g 'session-created[105]' "run-shell -b \"'${SCRIPTS_DIR}/find-socket.sh' '#{session_id}' >/dev/null 2>&1 || true\""
tmux set-hook -g 'client-attached[105]' "run-shell -b \"'${SCRIPTS_DIR}/find-socket.sh' '#{session_id}' >/dev/null 2>&1 || true\""
