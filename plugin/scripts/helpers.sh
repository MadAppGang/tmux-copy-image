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
