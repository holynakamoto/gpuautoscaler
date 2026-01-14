#!/bin/bash
set -e

# GPU Autoscaler Installation Script
# Usage: curl -sSL https://gpuautoscaler.io/install.sh | bash
# or: curl -sSL https://raw.githubusercontent.com/holynakamoto/gpuautoscaler/main/install.sh | bash

REPO_OWNER="holynakamoto"
REPO_NAME="gpuautoscaler"
BINARY_NAME="gpu-autoscaler"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Detect platform
detect_platform() {
    local platform="$(uname -s | tr '[:upper:]' '[:lower:]')"
    case "$platform" in
        linux*)
            echo "linux"
            ;;
        darwin*)
            echo "darwin"
            ;;
        mingw* | msys* | cygwin*)
            echo "windows"
            ;;
        *)
            log_error "Unsupported platform: $platform"
            exit 1
            ;;
    esac
}

# Detect architecture
detect_arch() {
    local arch="$(uname -m)"
    case "$arch" in
        x86_64 | amd64)
            echo "x86_64"
            ;;
        aarch64 | arm64)
            echo "arm64"
            ;;
        *)
            log_error "Unsupported architecture: $arch"
            exit 1
            ;;
    esac
}

# Get latest release version from GitHub
get_latest_version() {
    local version
    version=$(curl -sSf "https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
    if [ -z "$version" ]; then
        log_error "Failed to fetch latest version from GitHub"
        exit 1
    fi
    echo "$version"
}

# Download and install binary
install_binary() {
    local version="${1}"
    local platform="${2}"
    local arch="${3}"

    log_info "Installing ${BINARY_NAME} ${version} for ${platform}/${arch}..."

    # Construct download URL based on GoReleaser naming convention
    local archive_name="${REPO_NAME}_${platform}_${arch}"
    local extension="tar.gz"

    if [ "$platform" = "windows" ]; then
        extension="zip"
    fi

    # Capitalize platform name for GoReleaser convention
    local platform_title="$(echo "$platform" | sed 's/.*/\u&/')"
    local download_url="https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/download/${version}/gpuautoscaler_${platform_title}_${arch}.${extension}"

    log_info "Downloading from: ${download_url}"

    # Create temporary directory
    local tmp_dir="$(mktemp -d)"
    trap "rm -rf ${tmp_dir}" EXIT

    local archive_file="${tmp_dir}/${archive_name}.${extension}"

    # Download archive
    if ! curl -sSfL "$download_url" -o "$archive_file"; then
        log_error "Failed to download binary from ${download_url}"
        log_info "Please check if the release exists at: https://github.com/${REPO_OWNER}/${REPO_NAME}/releases"
        exit 1
    fi

    # Extract archive
    log_info "Extracting archive..."
    if [ "$extension" = "tar.gz" ]; then
        tar -xzf "$archive_file" -C "$tmp_dir"
    else
        unzip -q "$archive_file" -d "$tmp_dir"
    fi

    # Find the binary
    local binary_path="${tmp_dir}/${BINARY_NAME}"
    if [ "$platform" = "windows" ]; then
        binary_path="${binary_path}.exe"
    fi

    if [ ! -f "$binary_path" ]; then
        log_error "Binary not found in archive: ${binary_path}"
        exit 1
    fi

    # Install binary
    log_info "Installing to ${INSTALL_DIR}/${BINARY_NAME}..."

    # Check if we need sudo
    if [ -w "$INSTALL_DIR" ]; then
        mv "$binary_path" "${INSTALL_DIR}/${BINARY_NAME}"
        chmod +x "${INSTALL_DIR}/${BINARY_NAME}"
    else
        log_warn "Requesting sudo access to install to ${INSTALL_DIR}..."
        sudo mv "$binary_path" "${INSTALL_DIR}/${BINARY_NAME}"
        sudo chmod +x "${INSTALL_DIR}/${BINARY_NAME}"
    fi

    log_info "✓ ${BINARY_NAME} ${version} installed successfully!"
}

# Verify installation
verify_installation() {
    if command -v "$BINARY_NAME" &> /dev/null; then
        local installed_version
        installed_version=$("$BINARY_NAME" version 2>/dev/null || "$BINARY_NAME" --version 2>/dev/null || echo "unknown")
        log_info "Verification: ${BINARY_NAME} is available in PATH"
        log_info "Version: ${installed_version}"
    else
        log_warn "${BINARY_NAME} is not in your PATH. You may need to add ${INSTALL_DIR} to your PATH."
        log_warn "Add this to your shell profile: export PATH=\"${INSTALL_DIR}:\$PATH\""
    fi
}

# Main installation flow
main() {
    log_info "GPU Autoscaler Installer"
    log_info "========================="

    # Detect system
    local platform=$(detect_platform)
    local arch=$(detect_arch)

    log_info "Detected platform: ${platform}"
    log_info "Detected architecture: ${arch}"

    # Get version (use provided version or fetch latest)
    local version="${VERSION:-$(get_latest_version)}"

    # Install
    install_binary "$version" "$platform" "$arch"

    # Verify
    verify_installation

    echo ""
    log_info "🎉 Installation complete!"
    log_info ""
    log_info "Get started with:"
    log_info "  ${BINARY_NAME} status       # Check cluster GPU utilization"
    log_info "  ${BINARY_NAME} optimize     # Get optimization recommendations"
    log_info "  ${BINARY_NAME} cost         # View cost breakdown"
    log_info ""
    log_info "Documentation: https://github.com/${REPO_OWNER}/${REPO_NAME}"
}

main "$@"
