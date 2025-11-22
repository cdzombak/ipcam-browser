# IP Camera Browser

A web application for browsing and viewing IP camera recordings and snapshots with a Go backend and responsive JavaScript frontend.

## Features

- 📹 Browse videos and images from your IP camera's SD card
- 🔍 Filter by date, media type (images/videos), and trigger type (alarm/periodic)
- 🖼️ Gallery view with thumbnails
- 🎬 Built-in video player for H.264 (.264) and H.265 (.265) files
- 🔄 On-the-fly video remuxing (raw H.264/H.265 → MP4) with aggressive error handling
- 💾 Thread-safe caching system for images and converted videos
- 🔐 HTTP Basic Authentication support
- 📦 Single self-contained binary (frontend embedded with go:embed)
- 🚀 CORS-free proxy architecture
- 📱 Responsive design

## Prerequisites

- **Go 1.21+** - For building the application
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

### Running the Application

```bash
# Build the binary
go build -o ipcam-browser

# Run with default settings (camera at 192.168.205.196)
./ipcam-browser

# Or configure via environment variables
export CAMERA_URL="http://192.168.1.100/web/sd"
export CAMERA_USERNAME="admin"
export CAMERA_PASSWORD="your-password"
export PORT="8080"
./ipcam-browser
```

Then open your browser to http://localhost:8080

### Configuration

Configuration is done via environment variables:

- `CAMERA_URL` - Base URL to camera SD card (default: `http://192.168.205.196/web/sd`)
- `CAMERA_USERNAME` - Camera username (default: `admin`)
- `CAMERA_PASSWORD` - Camera password (default: `birdbath2`)
- `PORT` - Server port (default: `8080`)
- `CACHE_DIR` - Directory for caching media files (default: `/tmp/ipcam-browser-cache`)

## Architecture

```
┌─────────────┐         ┌──────────────┐         ┌─────────────┐
│   Browser   │ ◄─────► │  Go Backend  │ ◄─────► │  IP Camera  │
│  (Frontend) │   HTTP  │   (Proxy +   │   HTTP  │             │
│             │         │    Parser)   │  Basic  │             │
└─────────────┘         └──────────────┘  Auth   └─────────────┘
```

### Backend (Go)

- **HTTP Server**: Serves embedded static files and API endpoints
- **Proxy**: Forwards media requests to camera, bypassing CORS
- **HTML Parser**: Parses camera's directory listings into structured JSON
- **Media Categorization**: Identifies and categorizes images/videos by type and trigger
- **Video Remuxing**: On-the-fly conversion of raw H.264/H.265 streams to MP4 containers using ffmpeg for browser compatibility

### Frontend (JavaScript)

- **Single Page App**: No framework dependencies, vanilla JavaScript
- **API Consumer**: Fetches media metadata from backend
- **Media Viewer**: Gallery grid with modal lightbox for viewing
- **Filtering**: Client-side filtering by date, type, and trigger

## API Endpoints

### GET /api/media

Returns all media items from the camera.

**Response:**
```json
[
  {
    "name": "A25112207050200.jpg",
    "path": "20251122/images000/A25112207050200.jpg",
    "url": "http://192.168.205.196/web/sd/20251122/images000/A25112207050200.jpg",
    "proxyUrl": "",
    "date": "20251122",
    "type": "image",
    "trigger": "alarm",
    "timestamp": "2025-11-22 07:05:02",
    "size": "799.9k",
    "modified": "22-Nov-2025 07:05"
  },
  {
    "name": "A251122_070503_070517.264",
    "path": "20251122/record000/A251122_070503_070517.264",
    "url": "http://192.168.205.196/web/sd/20251122/record000/A251122_070503_070517.264",
    "proxyUrl": "/api/video/20251122%2Frecord000%2FA251122_070503_070517.264.mp4",
    "date": "20251122",
    "type": "video",
    "trigger": "alarm",
    "timestamp": "2025-11-22 07:05:03 - 07:05:17",
    "size": "9.2M",
    "modified": "22-Nov-2025 07:05"
  }
]
```

### GET /api/proxy?url={url}

Proxies media files from the camera. URL must start with configured `CAMERA_URL`.

**Parameters:**
- `url` - Full URL to camera media file

**Response:** Binary media file (image or video)

### GET /api/video/{path}.mp4

Fetches raw H.264/H.265 video from camera and remuxes to MP4 on-the-fly for browser playback.

**Parameters:**
- `path` - URL-encoded path to video file (e.g., `20251122%2Frecord000%2FA251122_070503_070517.264`)

**Response:** MP4 video stream (Content-Type: video/mp4)

**Technical Details:**
- Downloads video to temporary file first
- Uses ffmpeg to remux (no re-encoding) raw H.264/H.265 into MP4 container
- Employs aggressive error handling (`-fflags +genpts+discardcorrupt+igndts`, `-err_detect ignore_err`)
- Generates proper timestamps and handles corrupted parameter sets from camera
- Output is standard MP4 with `+faststart` for maximum compatibility
- Sets Content-Length header for efficient browser playback
- Preserves original video quality (codec copy, no transcoding)
- The `proxyUrl` field in media items automatically points to this endpoint

## Directory Structure

The application expects the camera to have this directory structure:

```
/web/sd/
├── YYYYMMDD/           # Date folders
│   ├── images000/      # Alarm/motion snapshots
│   │   └── *.jpg
│   ├── record000/      # Video recordings
│   │   ├── *.264       # H.264 videos
│   │   └── *.265       # H.265 videos
│   ├── imgdata.db
│   └── recdata.db
```

## File Naming Convention

- **Images**: `AYYMMDDHHMMSSXX.jpg`
  - `A` = Alarm/motion triggered
  - `P` = Periodic/scheduled
  - Example: `A25112207050200.jpg` = Alarm on 2025-11-22 at 07:05:02

- **Videos**: `AYYMMDDHHMMSSHHMMSSS.264` or `.265`
  - `A` = Alarm/motion triggered
  - `P` = Periodic/scheduled recording
  - Example: `A251122_070503_070517.264` = Alarm video from 07:05:03 to 07:05:17

## Building for Production

```bash
# Build optimized binary
go build -ldflags="-s -w" -o ipcam-browser

# Cross-compile for different platforms
GOOS=linux GOARCH=amd64 go build -o ipcam-browser-linux-amd64
GOOS=darwin GOARCH=arm64 go build -o ipcam-browser-darwin-arm64
GOOS=windows GOARCH=amd64 go build -o ipcam-browser-windows-amd64.exe
```

## Development

### Project Structure

```
.
├── main.go              # Go backend
├── go.mod              # Go dependencies
├── static/
│   └── index.html      # Frontend (embedded)
├── build-tree.sh       # CLI utility to view directory tree
└── README.md
```

### Building

```bash
go mod tidy
go build
```

### Testing

```bash
# Start the server
./ipcam-browser

# Test API
curl http://localhost:8080/api/media

# Test proxy
curl "http://localhost:8080/api/proxy?url=http://192.168.205.196/web/sd/20251122/images000/A25112207050200.jpg" > test.jpg
```

## Browser Compatibility

- ✅ Chrome/Edge (recommended)
- ✅ Firefox
- ✅ Safari
- ⚠️ H.265 video playback may not be supported in all browsers

## Caching

The application implements a thread-safe caching system for all media files:

- **Images**: Cached on first request to reduce camera load
- **Videos**: Converted MP4 files are cached to avoid repeated ffmpeg processing
- **Cache Directory**: Configurable via `CACHE_DIR` environment variable
- **Concurrency Safety**: Uses file locks and atomic operations to prevent race conditions
- **Cache Key**: SHA-256 hash of the source URL ensures uniqueness

The cache persists across server restarts (if using a persistent directory) and dramatically improves performance for repeated access.

## Known Issues

The camera outputs raw H.264/H.265 bitstreams with corrupted or missing Picture Parameter Sets (PPS). The application handles this by:
- Using aggressive ffmpeg error handling during remuxing
- Generating timestamps from the bitstream
- Discarding corrupt packets while preserving playable content
- Creating standard MP4 containers with `+faststart` for maximum compatibility

Video playback works in modern browsers despite these issues thanks to their built-in error recovery mechanisms.

## Future Enhancements

Potential features for future development:

- 💾 Media caching to reduce camera requests
- 📊 Timeline view with date range selection
- 🔍 Search by filename or time
- 📥 Bulk download functionality
- 🎨 Configurable UI themes
- 📈 Storage analytics and graphs
- 🔄 Auto-refresh for live monitoring
- 🗂️ SQLite database integration for faster queries

## Security Note

The application uses HTTP Basic Authentication to communicate with the camera. Ensure:
- The camera is on a trusted network
- Use environment variables (not hardcoded credentials)
- Consider running behind a reverse proxy with HTTPS
- The proxy endpoint validates URLs to prevent SSRF attacks

## License

This project is provided as-is for personal use.
