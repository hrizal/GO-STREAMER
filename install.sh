#!/bin/bash

# Go Audio Broadcaster - Master Installer
# Version: 1.0.0

set -e

APP_NAME="broadcaster"
BINARY_NAME="streamer"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/broadcaster"
SERVICE_FILE="/etc/systemd/system/broadcaster.service"

echo "----------------------------------------------------"
echo "🚀 Installing Go Audio Broadcaster..."
echo "----------------------------------------------------"

# 1. Check for FFmpeg
if ! command -v ffmpeg &> /dev/null; then
    echo "🔍 FFmpeg not found. Downloading static build..."
    
    # Identify Architecture
    ARCH=$(uname -m)
    FFMPEG_URL=""
    
    if [ "$ARCH" == "x86_64" ]; then
        FFMPEG_URL="https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-amd64-static.tar.xz"
    elif [ "$ARCH" == "aarch64" ] || [ "$ARCH" == "arm64" ]; then
        FFMPEG_URL="https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-arm64-static.tar.xz"
    else
        echo "❌ Unsupported architecture: $ARCH. Please install FFmpeg manually."
        exit 1
    fi

    echo "📥 Downloading from: $FFMPEG_URL"
    curl -L "$FFMPEG_URL" -o ffmpeg-static.tar.xz
    
    echo "📦 Extracting FFmpeg..."
    mkdir -p ffmpeg-temp
    tar -xf ffmpeg-static.tar.xz -C ffmpeg-temp --strip-components=1
    sudo cp ffmpeg-temp/ffmpeg /usr/local/bin/
    sudo cp ffmpeg-temp/ffprobe /usr/local/bin/
    
    # Cleanup
    rm -rf ffmpeg-temp ffmpeg-static.tar.xz
    echo "✅ FFmpeg installed successfully."
else
    echo "✅ FFmpeg is already installed: $(ffmpeg -version | head -n 1)"
fi

# 2. Build/Install the Broadcaster
if [ -f "./$BINARY_NAME" ]; then
    echo "📦 Using existing binary..."
else
    echo "🛠️ Building from source..."
    go build -o $BINARY_NAME main.go
fi

echo "🚚 Copying binary to $INSTALL_DIR..."
sudo cp $BINARY_NAME $INSTALL_DIR/$APP_NAME
sudo chmod +x $INSTALL_DIR/$APP_NAME

# 3. Setup Configuration Directory
echo "📁 Setting up configuration at $CONFIG_DIR..."
sudo mkdir -p $CONFIG_DIR
if [ ! -f "$CONFIG_DIR/station.cfg" ]; then
    if [ -f "station.cfg" ]; then
        sudo cp station.cfg $CONFIG_DIR/
    else
        echo "Empty station.cfg created."
        sudo touch $CONFIG_DIR/station.cfg
    fi
fi

# 4. Create Systemd Service
echo "⚙️ Creating Systemd service..."
cat <<EOF | sudo tee $SERVICE_FILE > /dev/null
[Unit]
Description=Go Audio Broadcaster Service
After=network.target

[Service]
Type=simple
ExecStart=$INSTALL_DIR/$APP_NAME -port 8080 -config $CONFIG_DIR/station.cfg
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF

# 5. Finalize
echo "🔄 Reloading systemd and starting service..."
sudo systemctl daemon-reload
sudo systemctl enable broadcaster
sudo systemctl restart broadcaster

echo "----------------------------------------------------"
echo "🎉 SUCCESS! Go Audio Broadcaster is now running."
echo "📻 API Port: 8080"
echo "📂 Config: $CONFIG_DIR/station.cfg"
echo "📜 View Logs: journalctl -u broadcaster -f"
echo "----------------------------------------------------"
