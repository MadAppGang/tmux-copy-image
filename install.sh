#!/usr/bin/env bash
# install.sh — one-shot installer for rpaster
#
# Usage:
#   curl -sSL https://raw.githubusercontent.com/MadAppGang/tmux-copy-image/main/install.sh | bash
#
# What it does:
#   1. Detects OS (darwin/linux) and architecture (amd64/arm64)
#   2. Downloads the latest release binary archive from GitHub Releases
#   3. Verifies the SHA256 checksum
#   4. Installs the binary to ~/.local/bin/rpaster
#   5. Runs `rpaster install` to set up the launchd/systemd service
#   6. Prints next steps

set -euo pipefail

REPO="MadAppGang/tmux-copy-image"
INSTALL_DIR="${HOME}/.local/bin"
BINARY_NAME="rpaster"

# ---------- helpers -----------------------------------------------------------

info()    { printf '\033[0;34m[info]\033[0m  %s\n' "$*"; }
success() { printf '\033[0;32m[ok]\033[0m    %s\n' "$*"; }
warn()    { printf '\033[0;33m[warn]\033[0m  %s\n' "$*" >&2; }
die()     { printf '\033[0;31m[error]\033[0m %s\n' "$*" >&2; exit 1; }

need() {
    command -v "$1" >/dev/null 2>&1 || die "Required command not found: $1"
}

# ---------- detect platform ---------------------------------------------------

detect_os() {
    case "$(uname -s)" in
        Darwin) echo "darwin" ;;
        Linux)  echo "linux"  ;;
        *) die "Unsupported OS: $(uname -s)" ;;
    esac
}

detect_arch() {
    case "$(uname -m)" in
        x86_64 | amd64) echo "amd64" ;;
        aarch64 | arm64) echo "arm64" ;;
        *) die "Unsupported architecture: $(uname -m)" ;;
    esac
}

# ---------- fetch latest release tag -----------------------------------------

latest_tag() {
    need curl
    curl -sSfL "https://api.github.com/repos/${REPO}/releases/latest" \
        | grep '"tag_name"' \
        | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/'
}

# ---------- verify checksum ---------------------------------------------------

verify_checksum() {
    local archive="$1"
    local checksums_file="$2"
    local filename
    filename="$(basename "${archive}")"

    info "Verifying SHA256 checksum..."

    if command -v sha256sum >/dev/null 2>&1; then
        grep "${filename}" "${checksums_file}" | sha256sum --check --status \
            || die "Checksum verification failed for ${filename}"
    elif command -v shasum >/dev/null 2>&1; then
        grep "${filename}" "${checksums_file}" | shasum -a 256 --check --status \
            || die "Checksum verification failed for ${filename}"
    else
        warn "Neither sha256sum nor shasum found — skipping checksum verification"
    fi

    success "Checksum OK"
}

# ---------- main --------------------------------------------------------------

main() {
    need curl

    OS="$(detect_os)"
    ARCH="$(detect_arch)"

    info "Detected platform: ${OS}/${ARCH}"

    TAG="$(latest_tag)"
    if [[ -z "${TAG}" ]]; then
        die "Could not determine latest release tag from GitHub"
    fi
    info "Latest release: ${TAG}"

    ARCHIVE_NAME="${BINARY_NAME}_${OS}_${ARCH}.tar.gz"
    BASE_URL="https://github.com/${REPO}/releases/download/${TAG}"
    ARCHIVE_URL="${BASE_URL}/${ARCHIVE_NAME}"
    CHECKSUMS_URL="${BASE_URL}/${BINARY_NAME}_checksums.txt"

    TMPDIR="$(mktemp -d)"
    # shellcheck disable=SC2064
    trap "rm -rf '${TMPDIR}'" EXIT

    ARCHIVE_PATH="${TMPDIR}/${ARCHIVE_NAME}"
    CHECKSUMS_PATH="${TMPDIR}/checksums.txt"

    info "Downloading ${ARCHIVE_URL} ..."
    curl -sSfL --progress-bar -o "${ARCHIVE_PATH}" "${ARCHIVE_URL}" \
        || die "Download failed: ${ARCHIVE_URL}"

    info "Downloading checksums from ${CHECKSUMS_URL} ..."
    curl -sSfL -o "${CHECKSUMS_PATH}" "${CHECKSUMS_URL}" \
        || die "Download failed: ${CHECKSUMS_URL}"

    verify_checksum "${ARCHIVE_PATH}" "${CHECKSUMS_PATH}"

    info "Extracting archive..."
    tar -xzf "${ARCHIVE_PATH}" -C "${TMPDIR}" \
        || die "Failed to extract ${ARCHIVE_PATH}"

    EXTRACTED_BINARY="${TMPDIR}/${BINARY_NAME}"
    if [[ ! -f "${EXTRACTED_BINARY}" ]]; then
        die "Binary not found after extraction: ${EXTRACTED_BINARY}"
    fi

    mkdir -p "${INSTALL_DIR}" \
        || die "Could not create install directory: ${INSTALL_DIR}"

    DEST="${INSTALL_DIR}/${BINARY_NAME}"
    install -m 755 "${EXTRACTED_BINARY}" "${DEST}" \
        || die "Failed to install binary to ${DEST}"

    success "Installed ${BINARY_NAME} ${TAG} to ${DEST}"

    # Ensure INSTALL_DIR is on PATH for the current session so rpaster install works.
    export PATH="${INSTALL_DIR}:${PATH}"

    info "Running 'rpaster install' to configure the system service..."
    "${DEST}" install \
        || die "'rpaster install' failed — you may need to run it manually"

    success "Service configured."

    cat <<EOF

Next steps:

  Start the service now (if not already running):
    macOS:  launchctl load -w ~/Library/LaunchAgents/com.rpaster.plist
    Linux:  systemctl --user enable --now rpaster

  Then install the tmux plugin on a remote host:
    rpaster install --remote <user@host>

  Check everything is working:
    rpaster doctor

  For usage help:
    rpaster --help

EOF
}

main "$@"
