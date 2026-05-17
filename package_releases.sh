#!/bin/bash
# Packaging script to create single-unzip ready-to-run ZIP installers for Go Audio Broadcaster

VERSION="1.2.0"
BUILD_DIR="./releases"
DIST_DIR="./dist"
TMP_DIR="./tmp_packaging"

echo "=== Starting Packaging for Go Audio Broadcaster v${VERSION} ==="
mkdir -p "$DIST_DIR"

# Clean any existing temp packaging dirs
rm -rf "$TMP_DIR"

# Function to package a release
# Usage: package_platform <platform_name> <binary_filename_in_releases> <is_windows_bool> <include_service_scripts_bool>
package_platform() {
    local platform="$1"
    local bin_src="$2"
    local is_windows="$3"
    local include_services="$4"

    echo "--------------------------------------------------------"
    echo "Packaging for ${platform}..."

    local target_dir="${TMP_DIR}/${platform}"
    mkdir -p "$target_dir"
    mkdir -p "$target_dir/silent"

    # 1. Copy Binary and rename it to 'streamer' / 'streamer.exe'
    if [ "$is_windows" = "true" ]; then
        cp "${BUILD_DIR}/${bin_src}" "$target_dir/streamer.exe"
    else
        cp "${BUILD_DIR}/${bin_src}" "$target_dir/streamer"
        chmod +x "$target_dir/streamer"
    fi

    # 2. Copy Default Assets & Configuration templates
    cp station.cfg "$target_dir/station.cfg"
    cp silent/silent_5s.mp3 "$target_dir/silent/silent_5s.mp3"
    cp README.md "$target_dir/README.md"
    cp API.md "$target_dir/API.md"
    cp LICENSE "$target_dir/LICENSE"

    # 3. Copy service scripts / platform helpers
    if [ "$is_windows" = "true" ]; then
        if [ -f setup-windows.bat ]; then
            cp setup-windows.bat "$target_dir/setup-windows.bat"
        fi
    else
        if [ "$include_services" = "true" ]; then
            cp install-service.sh "$target_dir/install-service.sh"
            cp uninstall-service.sh "$target_dir/uninstall-service.sh"
            cp streamer.service "$target_dir/streamer.service"
            chmod +x "$target_dir/install-service.sh"
            chmod +x "$target_dir/uninstall-service.sh"
        fi
    fi

    # 4. Create ZIP archive
    local zip_file="${DIST_DIR}/streamer-v${VERSION}-${platform}.zip"
    rm -f "$zip_file"
    
    # Enter target directory to avoid including the absolute path structure
    (cd "$target_dir" && zip -r "../../${zip_file}" .) > /dev/null

    echo "[Success] Created: ${zip_file}"
}

# 1. Linux AMD64
package_platform "linux-amd64" "streamer-linux-amd64" "false" "true"

# 2. Linux ARM64
package_platform "linux-arm64" "streamer-linux-arm64" "false" "true"

# 3. Windows AMD64
package_platform "windows-amd64" "streamer-windows-amd64.exe" "true" "false"

# 4. macOS Intel (amd64)
package_platform "macos-amd64" "streamer-darwin-amd64" "false" "false"

# 5. macOS Apple Silicon (arm64)
package_platform "macos-arm64" "streamer-darwin-arm64" "false" "false"

# Clean up temporary directories
rm -rf "$TMP_DIR"

echo "--------------------------------------------------------"
echo "=== Packaging Complete! ==="
ls -lh "$DIST_DIR"
