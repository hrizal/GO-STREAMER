#!/bin/bash
# Install Go Audio Broadcaster as systemd service
# Run this script as root: sudo bash install-service.sh

set -e

SERVICE_NAME="streamer"
SERVICE_SRC="/opt/streamer/streamer.service"
SERVICE_DST="/etc/systemd/system/${SERVICE_NAME}.service"
BINARY="/opt/streamer/streamer"

echo "=== Go Audio Broadcaster Service Installation ==="
echo ""

# Check station.cfg exists
if [ ! -f "/opt/streamer/station.cfg" ]; then
    echo "[WARN] station.cfg not found, creating default..."
    echo -e "# Station Configuration\nradio1" > /var/www/streamer/station.cfg
fi

# Check service file
if [ ! -f "$SERVICE_SRC" ]; then
    echo "[ERROR] Service file not found at $SERVICE_SRC"
    exit 1
fi

# Build binary
BUILD_DIR="/opt/streamer"
echo "[BUILD] Rebuilding binary..."
cd "$BUILD_DIR"
if go build -o streamer .; then
    echo "  -> Binary built: $BINARY"
else
    echo "[ERROR] Build failed!"
    exit 1
fi

echo "[1/4] Copying service file..."
cp "$SERVICE_SRC" "$SERVICE_DST"
chmod 644 "$SERVICE_DST"
echo "  -> $SERVICE_DST"

echo "[2/4] Reloading systemd daemon..."
systemctl daemon-reload

echo "[3/4] Enabling service (auto-start on boot)..."
systemctl enable "$SERVICE_NAME"

echo "[4/4] Starting service..."
systemctl start "$SERVICE_NAME"

echo ""
echo "=== Verification ==="
sleep 2
systemctl status "$SERVICE_NAME" --no-pager
echo ""
echo "=== Checking logs ==="
journalctl -u "$SERVICE_NAME" -n 10 --no-pager
echo ""
echo "=== API Status ==="
sleep 3
curl -s http://localhost:8080/status | python3 -m json.tool 2>/dev/null || curl -s http://localhost:8080/status
echo ""
echo "=== Done! ==="
echo ""
echo "Useful commands:"
echo "  sudo systemctl status streamer         # Check status"
echo "  sudo systemctl restart streamer        # Restart"
echo "  sudo systemctl stop streamer           # Stop"
echo "  sudo journalctl -u streamer -f         # Follow logs"
echo "  sudo systemctl disable streamer        # Disable auto-start"
