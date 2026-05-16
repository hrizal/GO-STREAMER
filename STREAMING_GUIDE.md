# 🚀 MusiKita Live Streaming Guide

This guide helps you connect your MusiKita radio station directly to **TikTok Live**, **YouTube Live**, or **Facebook Live**.

## 1. Get Your Stream Credentials
Before starting, you need the RTMP credentials from your target platform:

### 📱 TikTok Live (via PC)
1. Open TikTok on your PC, go to the **Go Live** menu.
2. Select **PC/Console** as the broadcast source.
3. You will receive two pieces of data:
   - **Server URL**: e.g., `rtmp://id.tiktok.com/stage/`
   - **Stream Key**: e.g., `vbc-1234-abcd`

### 📺 YouTube Live
1. Open **YouTube Studio** -> Click the **Live** button.
2. Look at the **Stream Settings** section.
3. Get the following:
   - **Stream URL**: e.g., `rtmp://a.rtmp.youtube.com/live2`
   - **Stream Key**: e.g., `abcd-1234-wxyz`

---

## 2. Configuration in MusiKita
Open your station configuration file (usually `station.cfg`) and add the following parameters:

### Basic Formula:
`rtmp=[SERVER_URL]/[STREAM_KEY]`

### Example in `station.cfg`:
```ini
# Broadcasting to TikTok with aesthetic visuals
radio1 output=/var/www/hls/radio1 playlist=/path/to/music \
       rtmp=rtmp://id.tiktok.com/stage/vbc-1234-abcd \
       display_text=MUSIKITA_RADIO_GLOBAL \
       logo=/var/www/streamer/assets/my_logo.png \
       aac128=true
```

---

## 3. Visual Features
When you stream to RTMP, MusiKita sends both audio and high-quality visuals:

1. **Misty Background (Automatic)**: If no video is provided, MusiKita generates a beautiful "Misty Fog" effect (purple/blue moving gradients).
2. **display_text**: The text specified here will appear in the center of the screen. Perfect for your station's name.
3. **logo**: If you provide a `.png` logo file, it will be automatically overlaid in the top-right corner.
4. **video_loop**: Use your own video file as the background instead of the default misty fog.

---

## 4. How to Run
After saving `station.cfg`:

1. Run the streamer as usual:
   ```bash
   ./streamer -config=station.cfg
   ```
2. Check your platform's dashboard. Once the signal is received, click **Go Live**!

---

## 5. Pro Tips
- **Internet Connection**: RTMP streaming requires a stable upload speed. Use a wired LAN connection if possible.
- **Resolution**: MusiKita automatically sends at **360p**. This is optimized for mobile viewing on TikTok, ensuring high stability and good quality.
- **Copyright**: Ensure your playlist contains safe-to-stream content if broadcasting on platforms like YouTube for long periods.

---
**Happy Broadcasting! 🎙️✨**
