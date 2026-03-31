#!/usr/bin/env bash
# find-socket.sh — discover the rpaster socket for this tmux session
# and store it in the tmux session environment variable RPASTER_SOCKET.
#
# Run via tmux session-created and client-attached hooks:
#   set-hook -g session-created  "run-shell ~/.tmux/plugins/tmux-clip-image/scripts/find-socket.sh"
#   set-hook -g client-attached  "run-shell ~/.tmux/plugins/tmux-clip-image/scripts/find-socket.sh"

set -uo pipefail

# find_ssh_connection PID
# Walks the process tree from PID looking for SSH_CONNECTION in the environment.
# Supports Linux (/proc/<pid>/environ) and macOS (ps eww).
find_ssh_connection() {
    local pid="$1"
    local conn=""

    # Linux: read /proc/<pid>/environ
    if [ -r "/proc/${pid}/environ" ]; then
        conn="$(tr '\0' '\n' < "/proc/${pid}/environ" 2>/dev/null \
            | grep '^SSH_CONNECTION=' | head -1 | cut -d= -f2-)"
        if [ -n "${conn}" ]; then
            printf '%s' "${conn}"
            return 0
        fi
    fi

    # Try the tmux session environment (works if pane's shell is the SSH login shell).
    conn="$(tmux show-environment SSH_CONNECTION 2>/dev/null \
        | grep '^SSH_CONNECTION=' | cut -d= -f2-)"
    if [ -n "${conn}" ]; then
        printf '%s' "${conn}"
        return 0
    fi

    # macOS / fallback: walk parent pids via ps looking for SSH_CONNECTION.
    local ppid
    ppid="$(ps -o ppid= -p "${pid}" 2>/dev/null | tr -d ' ')"
    while [ -n "${ppid}" ] && [ "${ppid}" != "1" ] && [ "${ppid}" != "0" ]; do
        conn="$(ps eww -p "${ppid}" 2>/dev/null \
            | grep -oE 'SSH_CONNECTION=[^ ]+' | head -1 | cut -d= -f2-)"
        if [ -n "${conn}" ]; then
            printf '%s' "${conn}"
            return 0
        fi
        ppid="$(ps -o ppid= -p "${ppid}" 2>/dev/null | tr -d ' ')"
    done

    return 1
}

# socket_from_conn SSH_CONNECTION
# Computes the deterministic socket path from an SSH_CONNECTION value.
# Uses SHA-256(conn)[0:8] -> 16 hex chars -> /tmp/rpaster-<hex>.sock
socket_from_conn() {
    local conn="$1"
    local hash
    # sha256sum on Linux, shasum -a 256 on macOS.
    hash="$(printf '%s' "${conn}" | sha256sum 2>/dev/null \
        || printf '%s' "${conn}" | shasum -a 256 2>/dev/null)"
    # Take first 16 hex chars (first 8 bytes of SHA-256).
    hash="${hash:0:16}"
    printf '/tmp/rpaster-%s.sock' "${hash}"
}

# probe_socket PATH
# Returns 0 if PATH is a socket file and curl can reach /health via it.
probe_socket() {
    local path="$1"
    [ -S "${path}" ] || return 1
    curl --silent --max-time 1 --unix-socket "${path}" \
        http://localhost/health >/dev/null 2>&1
}

# --- Main ---

# Get the PID of the current pane's shell.
pane_pid="$(tmux display-message -p '#{pane_pid}' 2>/dev/null)"

# Priority 1: Session-unique socket derived from SSH_CONNECTION.
ssh_conn="$(find_ssh_connection "${pane_pid:-0}")" || ssh_conn=""
if [ -n "${ssh_conn}" ]; then
    candidate="$(socket_from_conn "${ssh_conn}")"
    if probe_socket "${candidate}"; then
        tmux set-environment RPASTER_SOCKET "${candidate}"
        exit 0
    fi
fi

# Priority 2: Hostname-keyed fallback socket (static StreamLocalForward).
# The installer writes: StreamLocalForward /tmp/rpaster-<hostname>.sock /tmp/rpaster.sock
hostname_sock="/tmp/rpaster-$(hostname -s 2>/dev/null | tr -dc 'a-zA-Z0-9-').sock"
if probe_socket "${hostname_sock}"; then
    tmux set-environment RPASTER_SOCKET "${hostname_sock}"
    exit 0
fi

# Priority 3: Local daemon socket at the well-known default path.
if probe_socket "/tmp/rpaster.sock"; then
    tmux set-environment RPASTER_SOCKET "/tmp/rpaster.sock"
    exit 0
fi

# Priority 4: Local daemon via TCP port (backward compat — clear socket var).
port="$(tmux show-option -gqv "@clip-image-port" 2>/dev/null)"
port="${port:-18339}"
if curl --silent --max-time 1 "http://127.0.0.1:${port}/health" >/dev/null 2>&1; then
    # TCP is available — unset RPASTER_SOCKET so paste.sh uses TCP path.
    tmux set-environment -u RPASTER_SOCKET 2>/dev/null || true
    exit 0
fi

# Nothing found — unset so paste.sh falls through to its own error handling.
tmux set-environment -u RPASTER_SOCKET 2>/dev/null || true
exit 0
