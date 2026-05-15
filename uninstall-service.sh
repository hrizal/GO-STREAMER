#!/bin/bash
# Uninstall Go Audio Broadcaster service
# Run as root: sudo bash uninstall-service.sh

set -e

SERVICE_NAME="streamer"

echo "=== Uninstalling $SERVICE_NAME Service ==="

if systemctl is-active --quiet "$SERVICE_NAME" 2>/dev/null; then
    echo "[1/3] Stopping service..."
    systemctl stop "$SERVICE_NAME"
fi

if systemctl is-enabled --quiet "$SERVICE_NAME" 2>/dev/null; then
    echo "[2/3] Disabling service..."
    systemctl disable "$SERVICE_NAME"
fi

echo "[3/3] Removing service file..."
rm -f "/etc/systemd/system/${SERVICE_NAME}.service"
systemctl daemon-reload

echo ""
echo "=== Service removed ==="
echo "To reinstall: sudo bash /opt/streamer/install-service.sh"
