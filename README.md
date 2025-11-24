# IP Camera Browser

An improved web application for browsing and viewing recordings and snapshots on an SD-card equipped SV3C/CamHiPro compatible IP webcam.

## Features

- 📹 Browse videos and images from your IP camera's SD card
- 🔍 Filter by date, media type (images/videos), and trigger type (alarm/periodic)
- 🖼️ Gallery view with thumbnails
- 🎬 Built-in video player for H.264 (.264) and H.265 (.265) files
- 🔄 On-the-fly video remuxing (raw H.264/H.265 → MP4) with aggressive error handling
- 💾 Caching system for images and converted videos
- 📦 Single self-contained binary
- 📱 Responsive design

## Installation

### Debian/Ubuntu (via apt repository)

```bash
# Add repository
echo "deb http://dist.cdzombak.net/deb_oss any oss" | sudo tee /etc/apt/sources.list.d/dist-cdzombak-net-deb-oss.list
sudo apt update

# Install
sudo apt install ipcam-browser
```

**Note:** You'll also need to install ffmpeg: `sudo apt install ffmpeg`

### macOS (via Homebrew)

```bash
brew install cdzombak/oss/ipcam-browser
```

**Note:** ffmpeg is automatically installed as a dependency.

### Docker

Docker images are available on both GitHub Container Registry and Docker Hub:

```bash
# Using Docker Hub
docker pull cdzombak/ipcam-browser:latest

# Using GitHub Container Registry
docker pull ghcr.io/cdzombak/ipcam-browser:latest

# Run with Docker Compose (recommended)
# Edit docker-compose.yml with your camera settings, then:
docker compose up -d

# Or run directly with docker run:
docker run -d \
  -p 8080:8080 \
  -e CAMERA_URL=http://your-camera-ip/web/sd \
  -e CAMERA_USERNAME=admin \
  -e CAMERA_PASSWORD=your-password \
  -e CAMERA_NAME=your-camera-name \
  -v ipcam-cache:/var/cache/ipcam-browser \
  cdzombak/ipcam-browser:latest
```

See [`docker-compose.yml`](docker-compose.yml) for a complete example with all configuration options.

### Manual Download

Download pre-built binaries from the [releases page](https://github.com/cdzombak/ipcam-browser/releases).

**Prerequisites:**
- **ffmpeg** - Required for video remuxing (must be in PATH)

Install ffmpeg:
```bash
# macOS
brew install ffmpeg

# Ubuntu/Debian
sudo apt install ffmpeg

# Windows
# Download from https://ffmpeg.org/download.html
```

## Quick Start

```bash
export CAMERA_URL="http://192.168.1.100/web/sd"
export CAMERA_USERNAME="admin"
export CAMERA_PASSWORD="your-password"
export PORT="8080"
ipcam-browser
```

Then open your browser to http://localhost:8080

## Configuration

Configuration is via environment variables:

- `CAMERA_URL` - **[Required]** Base URL to camera SD card (e.g., `http://192.168.1.100/web/sd`)
- `CAMERA_USERNAME` - Camera username (default: `admin`)
- `CAMERA_PASSWORD` - **[Required]** Camera password
- `CAMERA_NAME` - Display name for your camera (default: `camera`)
- `PORT` - Server port (default: `8080`)
- `CACHE_DIR` - Directory for caching media files (default: `/tmp/ipcam-browser-cache`)
- `MAX_CONCURRENT_CONVERSIONS` - Maximum parallel video conversions (default: `3`)

## Cache Maintenance

To prevent unbounded cache growth, use the provided cleanup script to remove old cached files:

```bash
# Remove files older than 30 days from cache directory
./cleanup-old-cache.sh /mnt/ipcam-cache 30

# Set up automated cleanup via cron (runs daily at 3 AM)
crontab -e
# Add this line:
0 3 * * * /path/to/cleanup-old-cache.sh /mnt/ipcam-cache 30 >> /var/log/ipcam-cache-cleanup.log 2>&1
```

The script:
- Removes files modified more than N days ago (default: 30 days)
- Works recursively through all subdirectories
- Logs the number of files deleted
- Safe to run while the application is running (deleted cached files will be regenerated on next access if still available from camera)

## Security Note

This program provides no authentication. I recommend hosting it behind an authenticating reverse proxy or via [Tailscale](https://tailscale.com/kb/1312/serve).

## License

This program is provided under the MIT license; see [LICENSE](LICENSE) in this repo.

## Author

Chris Dzombak ([dzombak.com](https://www.dzombak.com) / GitHub [@cdzombak](https://github.com/cdzombak)) assisted by Claude Code.
