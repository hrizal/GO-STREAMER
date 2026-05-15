#!/bin/bash
# Quick Installer for Go Audio Broadcaster
# Target: Linux (Ubuntu/Debian recommended)

set -e

# Colors for output
RED='\033[0-31m'
GREEN='\033[0-32m'
NC='\033[0m' # No Color

echo -e "${GREEN}=== Go Audio Broadcaster - Quick Installer ===${NC}"
echo "This script will install FFmpeg, setup directories, and start the streamer service."
echo ""

# 1. Check for root
if [ "$EUID" -ne 0 ]; then 
  echo -e "${RED}Please run as root (use sudo)${NC}"
  exit 1
fi

# 2. Install FFmpeg
echo "[1/5] Checking for FFmpeg..."
if ! command -v ffmpeg &> /dev/null; then
    echo "Installing FFmpeg..."
    if command -v apt-get &> /dev/null; then
        apt-get update && apt-get install -y ffmpeg
    elif command -v yum &> /dev/null; then
        yum install -y epel-release && yum install -y ffmpeg
    elif command -v dnf &> /dev/null; then
        dnf install -y ffmpeg
    else
        echo -e "${RED}Package manager not found. Please install FFmpeg manually.${NC}"
        exit 1
    fi
    echo -e "${GREEN}FFmpeg installed successfully.${NC}"
else
    echo "FFmpeg is already installed."
fi

# 3. Detect Architecture and Choose Binary
ARCH=$(uname -m)
BINARY_SRC=""
INSTALL_DIR="/opt/streamer"
HLS_DIR="/var/www/hls"

echo "[2/5] Detecting architecture ($ARCH)..."
if [ "$ARCH" == "x86_64" ]; then
    BINARY_SRC="./releases/streamer-linux-amd64"
elif [ "$ARCH" == "aarch64" ] || [ "$ARCH" == "arm64" ]; then
    BINARY_SRC="./releases/streamer-linux-arm64"
else
    echo -e "${RED}Unsupported architecture: $ARCH. Please compile from source.${NC}"
    exit 1
fi

# 4. Setup Directories and Copy Files
echo "[3/5] Setting up directories..."
mkdir -p "$INSTALL_DIR"
mkdir -p "$HLS_DIR"
mkdir -p "$INSTALL_DIR/output"

echo "Copying files to $INSTALL_DIR..."
if [ -f "$BINARY_SRC" ]; then
    cp "$BINARY_SRC" "$INSTALL_DIR/streamer"
    chmod +x "$INSTALL_DIR/streamer"
else
    echo -e "${RED}Binary not found at $BINARY_SRC. Please ensure you are running this from the repository folder.${NC}"
    exit 1
fi

# Copy config and service if they exist in current dir
[ -f "./station.cfg" ] && cp "./station.cfg" "$INSTALL_DIR/"
[ -f "./streamer.service" ] && cp "./streamer.service" "$INSTALL_DIR/"
[ -f "./uninstall-service.sh" ] && cp "./uninstall-service.sh" "$INSTALL_DIR/"

# 5. Setup Systemd Service
echo "[4/5] Setting up systemd service..."
SERVICE_PATH="/etc/systemd/system/streamer.service"

# Update paths in service file if needed (assuming streamer.service uses /opt/streamer)
if [ -f "$INSTALL_DIR/streamer.service" ]; then
    cp "$INSTALL_DIR/streamer.service" "$SERVICE_PATH"
    systemctl daemon-reload
    systemctl enable streamer
    systemctl start streamer
    echo -e "${GREEN}Service 'streamer' started and enabled on boot.${NC}"
else
    echo -e "${RED}streamer.service template not found. Skipping service setup.${NC}"
fi

# 6. Final Status
echo ""
echo -e "${GREEN}=== Installation Complete! ===${NC}"
echo "Streamer is running at port 8080."
echo "Config file: $INSTALL_DIR/station.cfg"
echo "HLS Output: $HLS_DIR"
echo ""
echo "You can check status using: sudo systemctl status streamer"
echo "Enjoy your broadcast!"
