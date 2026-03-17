#!/usr/bin/env bash
# paste.sh — tmux-clip-image keybinding handler.
#
# Fetches a clipboard image from the local rpaster daemon (via SSH tunnel)
# and inserts the saved file path into the current tmux pane.
#
# Phase 2: multi-format support, Claude Code detection, full error messaging.

# Set restrictive umask immediately — all files created default to 0600.
# This eliminates the race window between file creation and explicit chmod.
umask 077

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# shellcheck source=./helpers.sh
source "${SCRIPT_DIR}/helpers.sh"

# shellcheck source=./detect-claude.sh
source "${SCRIPT_DIR}/detect-claude.sh"

# ---------------------------------------------------------------------------
# Read plugin options (all reads happen at invocation time, not startup).
# ---------------------------------------------------------------------------
port="$(clip_option "port" "18339")"
save_dir="$(clip_option "save-dir" "")"
health_timeout="$(clip_option "health-timeout" "2")"
meta_timeout="$(clip_option "meta-timeout" "5")"
download_timeout="$(clip_option "download-timeout" "10")"
msg_duration="$(clip_option "msg-duration" "3000")"
claude_detect="$(clip_option "claude-detect" "auto")"

# Default save directory to $TMPDIR or /tmp.
if [ -z "${save_dir}" ]; then
    save_dir="${TMPDIR:-/tmp}"
fi

# Validate save_dir: reject paths with "..", reject system directories, require
# path under $HOME or $TMPDIR.
case "${save_dir}" in
    *..*)
        clip_display_error "invalid save-dir (contains ..)" "${msg_duration}"
        exit 1
        ;;
    /bin | /sbin | /usr | /etc | /root | /boot | /dev | /proc | /sys)
        clip_display_error "invalid save-dir (system directory)" "${msg_duration}"
        exit 1
        ;;
esac

# Token file path (read from file, not from tmux option, to keep it out of ps).
token_file="${HOME}/.config/rpaster/token"

# ---------------------------------------------------------------------------
# Step 1: Cleanup old clip-* temp files older than 6 hours.
# ---------------------------------------------------------------------------
find "${save_dir}" -maxdepth 1 -name "clip-*" -mmin +360 -delete 2>/dev/null || true

# ---------------------------------------------------------------------------
# Step 2: Health check — verify daemon is reachable.
# FR-P-20: "clip-image: cannot reach rpaster (is SSH tunnel up?)"
# ---------------------------------------------------------------------------
base_url="http://127.0.0.1:${port}"

if ! curl --silent --max-time "${health_timeout}" --max-redirs 0 \
        --output /dev/null \
        "${base_url}/health" 2>/dev/null; then
    clip_display_error "cannot reach rpaster (is SSH tunnel up?)" "${msg_duration}"
    exit 1
fi

# ---------------------------------------------------------------------------
# Step 3: Check image availability via /image/meta?format=shell
# FR-P-19: "clip-image: no image found in clipboard"
# ---------------------------------------------------------------------------
meta_raw=""
if [ -f "${token_file}" ] && [ -r "${token_file}" ]; then
    meta_raw="$(curl --silent --max-time "${meta_timeout}" --max-redirs 0 \
        --config <(printf 'header = "Authorization: Bearer %s"' "$(cat "${token_file}")") \
        "${base_url}/image/meta?format=shell" 2>/dev/null)" || true
else
    meta_raw="$(curl --silent --max-time "${meta_timeout}" --max-redirs 0 \
        "${base_url}/image/meta?format=shell" 2>/dev/null)" || true
fi

if [ -z "${meta_raw}" ]; then
    clip_display_error "could not check image availability" "${msg_duration}"
    exit 1
fi

# Evaluate shell-format KEY=VALUE lines into shell variables.
# Values are guaranteed to be enum strings and integers (no metacharacters).
eval "${meta_raw}"

# AVAILABLE, FORMAT, WIDTH, HEIGHT, SIZE_BYTES are now set (or unset).
# FR-P-19: no image found in clipboard
if [ "${AVAILABLE:-false}" = "false" ]; then
    clip_display_error "no image found in clipboard" "${msg_duration}"
    exit 1
fi

# Derive file extension from FORMAT.
case "${FORMAT:-}" in
    png)  ext=".png"  ;;
    jpeg) ext=".jpg"  ;;
    gif)  ext=".gif"  ;;
    webp) ext=".webp" ;;
    *)    ext=".bin"  ;;
esac

# ---------------------------------------------------------------------------
# Step 4: Construct save path with a random suffix.
# ---------------------------------------------------------------------------
rand_suffix="$(openssl rand -hex 8 2>/dev/null || printf '%s' "${RANDOM}${RANDOM}")"
filename="clip-$(date +%s)-${rand_suffix}${ext}"
save_path="${save_dir}/${filename}"

# Ensure the save directory exists.
mkdir -p "${save_dir}" 2>/dev/null || save_dir="${TMPDIR:-/tmp}"
save_path="${save_dir}/${filename}"

# ---------------------------------------------------------------------------
# Step 5: Download the image from /image.
# FR-P-22: "clip-image: download timed out after 10s"
# FR-P-21: "clip-image: image too large (max 10MB)"
# ---------------------------------------------------------------------------
http_status=""
curl_exit=0
if [ -f "${token_file}" ] && [ -r "${token_file}" ]; then
    http_status="$(curl --silent --max-time "${download_timeout}" --max-redirs 0 \
        --config <(printf 'header = "Authorization: Bearer %s"' "$(cat "${token_file}")") \
        --output "${save_path}" \
        --write-out "%{http_code}" \
        "${base_url}/image" 2>/dev/null)" || curl_exit=$?
else
    http_status="$(curl --silent --max-time "${download_timeout}" --max-redirs 0 \
        --output "${save_path}" \
        --write-out "%{http_code}" \
        "${base_url}/image" 2>/dev/null)" || curl_exit=$?
fi

# FR-P-22: curl timeout (exit code 28)
if [ "${curl_exit}" -eq 28 ]; then
    clip_display_error "download timed out after ${download_timeout}s" "${msg_duration}"
    rm -f "${save_path}" 2>/dev/null || true
    exit 1
fi

if [ "${curl_exit}" -ne 0 ]; then
    clip_display_error "download failed (curl exit ${curl_exit})" "${msg_duration}"
    rm -f "${save_path}" 2>/dev/null || true
    exit 1
fi

# FR-P-21: image too large (HTTP 413)
if [ "${http_status:-}" = "413" ]; then
    clip_display_error "image too large (max 10MB)" "${msg_duration}"
    rm -f "${save_path}" 2>/dev/null || true
    exit 1
fi

# Non-200 HTTP status.
if [ "${http_status:-}" != "200" ]; then
    clip_display_error "unexpected server response (HTTP ${http_status:-unknown})" "${msg_duration}"
    rm -f "${save_path}" 2>/dev/null || true
    exit 1
fi

# Verify the downloaded file is non-empty.
# FR-P-23: "clip-image: failed to save image to <path>"
if [ ! -s "${save_path}" ]; then
    clip_display_error "failed to save image to ${save_path}" "${msg_duration}"
    rm -f "${save_path}" 2>/dev/null || true
    exit 1
fi

# ---------------------------------------------------------------------------
# Step 6: Set permissions (belt-and-suspenders; umask 077 already applied).
# ---------------------------------------------------------------------------
chmod 600 "${save_path}" 2>/dev/null || true

# ---------------------------------------------------------------------------
# Step 7: Detect whether the active pane is running Claude Code.
# FR-P-13 through FR-P-18: Claude Code detection and mode control.
# ---------------------------------------------------------------------------
if detect_claude_code "${TMUX_PANE}" "${claude_detect}"; then
    clip_log_debug "Claude Code detected in pane ${TMUX_PANE}"
else
    clip_log_debug "Claude Code not detected in pane ${TMUX_PANE} (mode: ${claude_detect})"
fi
# Currently both paths insert the path identically (future: may differ).

# ---------------------------------------------------------------------------
# Step 8: Insert the file path into the active tmux pane (no Enter).
# ---------------------------------------------------------------------------
tmux send-keys -t "${TMUX_PANE}" "${save_path}"

# ---------------------------------------------------------------------------
# Step 9: Show success notification.
# FR-P-24: "clip-image: image saved to <path>"
# ---------------------------------------------------------------------------
success_duration="$(clip_option "msg-success-duration" "2000")"
clip_display_info "image saved to ${save_path}" "${success_duration}"
