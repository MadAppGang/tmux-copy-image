#!/usr/bin/env bash
# detect-claude.sh — Claude Code pane detection for tmux-clip-image.
#
# Source this file from other scripts; do not execute it directly.
#
# Provides: detect_claude_code PANE_ID MODE
#
# Returns 0 (detected) or 1 (not detected / not applicable).
# bash 3.2+ compatible: no associative arrays, no $'\n' constructs.

# detect_claude_code PANE_ID MODE
#
# PANE_ID: tmux pane identifier (e.g. "%0", "$TMUX_PANE")
# MODE:    one of: auto | always | never
#
# Exit codes:
#   0 — Claude Code detected (or mode is "always")
#   1 — not detected (or mode is "never")
#
# In "auto" mode the function captures the pane content and searches for
# Claude Code UI patterns using grep -qE (POSIX ERE). The heuristics
# are intentionally permissive: any one matching signal is sufficient.
detect_claude_code() {
    local pane_id="$1"
    local mode="${2:-auto}"

    case "${mode}" in
        never)
            return 1
            ;;
        always)
            return 0
            ;;
        auto)
            ;;
        *)
            # Unknown mode: treat as auto.
            ;;
    esac

    # Capture up to 50 joined lines from the pane.
    # -J joins wrapped lines so multi-line prompts are seen as one line.
    local pane_content
    pane_content="$(tmux capture-pane -p -t "${pane_id}" -J 2>/dev/null)" || return 1

    if [ -z "${pane_content}" ]; then
        return 1
    fi

    # Pattern 1: "Claude" brand name in the pane (header bar, prompt, tool output).
    if printf '%s' "${pane_content}" | grep -qE 'Claude'; then
        return 0
    fi

    # Pattern 2: Claude Code input prompt indicator ("> " prompt after the
    # assistant output separator line).
    if printf '%s' "${pane_content}" | grep -qE '^\s*>\s'; then
        return 0
    fi

    # Pattern 3: Tool use indicators emitted by Claude Code.
    if printf '%s' "${pane_content}" | grep -qE '(Reading file|Writing file|Bash tool|Searching|Tool use|tool_use)'; then
        return 0
    fi

    # Pattern 4: Claude Code session header lines.
    if printf '%s' "${pane_content}" | grep -qE '(claude-code|claude_code|anthropic)'; then
        return 0
    fi

    return 1
}
