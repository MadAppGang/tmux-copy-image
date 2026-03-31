#!/usr/bin/env bash
# helpers.sh — Shared utilities for tmux-clip-image plugin scripts.
#
# Source this file from other scripts; do not execute it directly.

# clip_option NAME DEFAULT
# Reads a @clip-image-NAME tmux option; returns DEFAULT if not set or empty.
clip_option() {
    local opt_name="$1"
    local default_val="$2"
    local val
    val="$(tmux show-option -gqv "@clip-image-${opt_name}" 2>/dev/null)"
    printf '%s' "${val:-$default_val}"
}

# rpaster_resolve_endpoint
# Resolves the rpaster endpoint, preferring a Unix socket when available.
#
# Sets (and exports) two variables after calling this function:
#   RPASTER_BASE             — base URL (e.g. "http://localhost" or "http://127.0.0.1:18339")
#   RPASTER_CURL_SOCK_ARGS   — array of extra curl flags (e.g. (--unix-socket /tmp/rpaster.sock))
#
# Usage:
#   rpaster_resolve_endpoint
#   curl --silent "${RPASTER_CURL_SOCK_ARGS[@]}" "${RPASTER_BASE}/health"
rpaster_resolve_endpoint() {
    local sock
    sock="$(tmux show-environment RPASTER_SOCKET 2>/dev/null | cut -d= -f2-)"
    if [ -n "${sock}" ] && [ -S "${sock}" ]; then
        RPASTER_BASE="http://localhost"
        RPASTER_CURL_SOCK_ARGS=(--unix-socket "${sock}")
    else
        local p
        p="$(tmux show-option -gqv "@clip-image-port" 2>/dev/null)"
        p="${p:-18339}"
        RPASTER_BASE="http://127.0.0.1:${p}"
        RPASTER_CURL_SOCK_ARGS=()
    fi
    export RPASTER_BASE RPASTER_CURL_SOCK_ARGS
}

# clip_display_error MESSAGE [DURATION_MS]
# Displays an error message in the tmux status bar.
clip_display_error() {
    local msg="clip-image: $1"
    local duration="${2:-3000}"
    tmux display-message -d "${duration}" "${msg}"
}

# clip_display_info MESSAGE [DURATION_MS]
# Displays an informational message in the tmux status bar.
clip_display_info() {
    local msg="clip-image: $1"
    local duration="${2:-2000}"
    tmux display-message -d "${duration}" "${msg}"
}

# clip_check_command COMMAND
# Returns 0 if COMMAND is in PATH, 1 otherwise.
clip_check_command() {
    command -v "$1" >/dev/null 2>&1
}

# clip_log_debug MESSAGE
# Logs a debug message to stderr if CLIP_IMAGE_DEBUG is set to 1.
clip_log_debug() {
    if [ "${CLIP_IMAGE_DEBUG:-0}" = "1" ]; then
        printf 'clip-image [debug]: %s\n' "$1" >&2
    fi
}
