# ipcam-browser HTTP API Documentation

This document describes the HTTP API provided by ipcam-browser. The API allows other applications to retrieve IP camera media files, configuration, and access cached/converted video content.

## Base URL

The API runs on the configured port (default: `8080`). All endpoints are relative to:

```
http://localhost:8080
```

## Authentication

The API itself does **not require authentication**. However, it is designed to be deployed behind an authenticating reverse proxy for secure external access.

The application uses HTTP Basic Authentication when communicating with the camera, configured via `CAMERA_USERNAME` and `CAMERA_PASSWORD` environment variables.

## Endpoints

### GET /api/config

Returns the camera configuration.

#### Request

```http
GET /api/config HTTP/1.1
```

#### Response

**Status:** `200 OK`

**Content-Type:** `application/json`

**Body:**

```json
{
  "cameraName": "string"
}
```

#### Response Fields

| Field | Type | Description |
|-------|------|-------------|
| `cameraName` | string | The configured name of the camera (from `CAMERA_NAME` env var, default: "camera") |

#### Example

```bash
curl http://localhost:8080/api/config
```

```json
{
  "cameraName": "Front Door Camera"
}
```

---

### GET /api/media

Retrieves all media files (images and videos) from the camera's SD card.

#### Request

```http
GET /api/media HTTP/1.1
```

#### Response

**Status:** `200 OK`

**Content-Type:** `application/json`

**Body:** Array of MediaItem objects

```json
[
  {
    "name": "string",
    "path": "string",
    "url": "string",
    "proxyUrl": "string",
    "thumbnailUrl": "string",
    "downloadFilename": "string",
    "date": "string",
    "type": "string",
    "trigger": "string",
    "timestamp": "string",
    "size": "string",
    "modified": "string"
  }
]
```

#### MediaItem Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Original filename from camera (e.g., "A251121212356.jpg") |
| `path` | string | Yes | Full path to file on camera (e.g., "2025-11-21/images000/A251121212356.jpg") |
| `url` | string | Yes | Direct URL to file on camera |
| `proxyUrl` | string | Yes | Proxied/converted URL for videos (empty for images). Format: `/api/video/{encoded-path}.mp4` |
| `thumbnailUrl` | string | No | Thumbnail image URL for videos (omitted if no matching thumbnail). Format: `/api/proxy?url={encoded-url}` |
| `downloadFilename` | string | Yes | Suggested filename for downloads in format: `{cameraName}_YYYY-MM-DD_HH-mm-ss.ext` |
| `date` | string | Yes | Date directory name (e.g., "2025-11-21") |
| `type` | string | Yes | Media type: `"image"` or `"video"` |
| `trigger` | string | Yes | Recording trigger: `"alarm"` (motion-triggered) or `"periodic"` (scheduled) |
| `timestamp` | string | Yes | Formatted timestamp. Images: "YYYY-MM-DD HH:mm:ss". Videos: "YYYY-MM-DD HH:mm:ss - HH:mm:ss" (start - end) |
| `size` | string | Yes | File size as reported by camera (e.g., "1.2M", "512K") |
| `modified` | string | Yes | Last modified date/time from camera |

#### Media Type Values

- **`"image"`**: JPEG images (`.jpg` files)
- **`"video"`**: H.264 or H.265 video files (`.264` or `.265` files, served as `.mp4`)

#### Trigger Values

- **`"alarm"`**: Motion-triggered recording (filename starts with 'A')
- **`"periodic"`**: Scheduled/periodic recording (filename starts with 'P')

#### Example

```bash
curl http://localhost:8080/api/media
```

```json
[
  {
    "name": "A251121212356.jpg",
    "path": "2025-11-21/images000/A251121212356.jpg",
    "url": "http://camera.local/2025-11-21/images000/A251121212356.jpg",
    "proxyUrl": "",
    "downloadFilename": "camera_2025-11-21_21-23-56.jpg",
    "date": "2025-11-21",
    "type": "image",
    "trigger": "alarm",
    "timestamp": "2025-11-21 21:23:56",
    "size": "245K",
    "modified": "2025-11-21 21:23:57"
  },
  {
    "name": "A251121_212356_212410.264",
    "path": "2025-11-21/record000/A251121_212356_212410.264",
    "url": "http://camera.local/2025-11-21/record000/A251121_212356_212410.264",
    "proxyUrl": "/api/video/2025-11-21%2Frecord000%2FA251121_212356_212410.264.mp4",
    "thumbnailUrl": "/api/proxy?url=http%3A%2F%2Fcamera.local%2F2025-11-21%2Fimages000%2FA251121212356.jpg",
    "downloadFilename": "camera_2025-11-21_21-23-56.mp4",
    "date": "2025-11-21",
    "type": "video",
    "trigger": "alarm",
    "timestamp": "2025-11-21 21:23:56 - 21:24:10",
    "size": "1.8M",
    "modified": "2025-11-21 21:24:11"
  }
]
```

#### Notes

- The endpoint fetches all date directories from the camera and aggregates media from both `images000` and `record000` subdirectories
- Video thumbnails are automatically matched with images taken during or 1 second before the video
- Calling this endpoint triggers background pre-caching of videos (conversion to MP4)
- May take several seconds to complete depending on the number of media files on the camera

---

### GET /api/proxy

Proxies and caches media files from the camera. Used primarily for serving images and thumbnails.

#### Request

```http
GET /api/proxy?url={encoded-url} HTTP/1.1
```

#### Query Parameters

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `url` | string | Yes | URL-encoded camera media URL. Must start with the configured `CAMERA_URL` |

#### Response

**Status:** `200 OK`

**Content-Type:** Determined by file extension (e.g., `image/jpeg`)

**Body:** Raw media file content

#### Error Responses

| Status | Description |
|--------|-------------|
| `400 Bad Request` | Missing `url` parameter or URL does not match configured camera |
| `500 Internal Server Error` | Failed to fetch media from camera or cache error |

#### Example

```bash
curl "http://localhost:8080/api/proxy?url=http%3A%2F%2Fcamera.local%2F2025-11-21%2Fimages000%2FA251121212356.jpg" \
  --output image.jpg
```

#### Behavior

1. **Security Check**: Validates that the URL starts with the configured `CAMERA_URL` to prevent proxy abuse
2. **Cache Check**: Returns cached file immediately if available
3. **Fetch**: Downloads file from camera using HTTP Basic Auth (limited to 3 concurrent camera requests)
4. **Cache Storage**: Stores file in cache directory using SHA-256 hash of URL as filename
5. **Serve**: Returns the cached file

#### Caching

- Cache directory: Configured via `CACHE_DIR` (default: `/tmp/ipcam-browser-cache`)
- Cache key: SHA-256 hash of URL + file extension
- Thread-safe: Per-file locking ensures only one fetch per URL
- Persistent: Cache survives server restarts (unless using `/tmp`)

---

### GET /api/video/{encoded-path}.mp4

Downloads raw video from camera, converts to MP4 format, caches, and serves.

#### Request

```http
GET /api/video/{encoded-path}.mp4 HTTP/1.1
```

#### Path Parameters

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `encoded-path` | string | Yes | URL-encoded path to video file on camera (without leading slash). Original extension (`.264` or `.265`) should be included in the path before encoding |

#### Response

**Status:** `200 OK`

**Content-Type:** `video/mp4`

**Body:** MP4 video file

#### Error Responses

| Status | Description |
|--------|-------------|
| `400 Bad Request` | Invalid path or URL does not match configured camera |
| `500 Internal Server Error` | Video conversion failed or cache error |

#### Example

```bash
# Path: 2025-11-21/record000/A251121_212356_212410.264
# Encoded: 2025-11-21%2Frecord000%2FA251121_212356_212410.264

curl "http://localhost:8080/api/video/2025-11-21%2Frecord000%2FA251121_212356_212410.264.mp4" \
  --output video.mp4
```

#### Video Processing

The endpoint performs several processing steps:

1. **Download**: Fetches raw H.264/H.265 video from camera
2. **Header Stripping**: Removes proprietary HXVS/HXVF 16-byte headers that prevent playback
3. **Frame Rate Detection**: Uses `ffprobe` to detect video frame rate (defaults to 20 FPS if detection fails)
4. **MP4 Conversion**: Converts to MP4 using `ffmpeg` with:
   - Video codec copy (no re-encoding)
   - Audio codec copy (preserves audio if present)
   - Fast-start optimization (moov atom at beginning)
   - Proper PTS (presentation timestamp) generation
5. **Caching**: Stores converted MP4 in cache for future requests
6. **Serve**: Returns the MP4 file

#### Supported Input Formats

- **H.264**: Files with `.264` extension
- **H.265/HEVC**: Files with `.265` extension

#### ffmpeg Requirements

This endpoint requires `ffmpeg` and `ffprobe` to be installed and available in the system PATH.

#### Concurrency

- Maximum concurrent conversions: Configured via `MAX_CONCURRENT_CONVERSIONS` (default: 3)
- Per-file locking: Multiple requests for the same video will only trigger one conversion
- Cached videos are served immediately without re-conversion

#### Caching

- Cache directory: Same as `/api/proxy` (configured via `CACHE_DIR`)
- Cache key: SHA-256 hash of camera URL + `.mp4` extension
- Persistent: Converted videos remain cached across server restarts

---

## Configuration

The API behavior is controlled by environment variables:

### Required

| Variable | Description | Default |
|----------|-------------|---------|
| `CAMERA_URL` | Base URL of the IP camera (e.g., `http://192.168.1.100`) | (none - required) |

### Optional

| Variable | Description | Default |
|----------|-------------|---------|
| `CAMERA_NAME` | Display name for the camera | `camera` |
| `CAMERA_USERNAME` | Username for camera HTTP Basic Auth | `admin` |
| `CAMERA_PASSWORD` | Password for camera HTTP Basic Auth | (empty) |
| `CACHE_DIR` | Directory for cached media files | `/tmp/ipcam-browser-cache` |
| `PORT` | HTTP server port | `8080` |
| `MAX_CONCURRENT_CONVERSIONS` | Maximum parallel video conversions | `3` |
| `BACKGROUND_CACHE_ENABLED` | Enable periodic background caching | `false` |
| `BACKGROUND_CACHE_INTERVAL_MINUTES` | Interval between background cache runs | `5` |

---

## Background Caching

When `BACKGROUND_CACHE_ENABLED=true`, the server periodically:

1. Fetches all media from the camera
2. Pre-converts all videos to MP4
3. Pre-caches all images and thumbnails

This improves user experience by ensuring media is ready for instant playback/viewing.

**Configuration:**
- Enable: `BACKGROUND_CACHE_ENABLED=true`
- Interval: `BACKGROUND_CACHE_INTERVAL_MINUTES=5` (minimum: 1 minute)

**Behavior:**
- Runs immediately on server startup (asynchronously)
- Runs periodically based on configured interval
- Skips runs if previous run is still in progress
- Stops gracefully on server shutdown

---

## Security Considerations

### No Built-in Authentication

The API does **not** provide authentication. Deploy behind an authenticating reverse proxy (e.g., nginx with HTTP Basic Auth, OAuth2 Proxy) for secure external access.

### URL Validation

Both `/api/proxy` and `/api/video/*` endpoints validate that requested URLs belong to the configured camera (`CAMERA_URL`) to prevent proxy abuse and SSRF attacks.

### Rate Limiting

The server limits concurrent camera requests to 3 (via internal semaphore) to prevent overwhelming the camera. This applies to both API requests and background caching.

---

## Error Handling

### HTTP Status Codes

| Code | Meaning |
|------|---------|
| `200 OK` | Request succeeded |
| `400 Bad Request` | Invalid parameters or malformed request |
| `405 Method Not Allowed` | Endpoint only supports GET requests |
| `500 Internal Server Error` | Server-side error (camera unreachable, conversion failed, etc.) |

### Error Response Format

Error responses return plain text error messages:

```
Failed to fetch media: server returned status 401
```

---

## Usage Examples

### Fetch Camera Name

```bash
curl http://localhost:8080/api/config | jq '.cameraName'
```

### List All Media

```bash
curl http://localhost:8080/api/media | jq '.'
```

### Filter Alarm Videos

```bash
curl http://localhost:8080/api/media | jq '[.[] | select(.type == "video" and .trigger == "alarm")]'
```

### Download Image via Proxy

```bash
# Get media list
MEDIA=$(curl -s http://localhost:8080/api/media)

# Extract first image URL
IMAGE_URL=$(echo "$MEDIA" | jq -r '[.[] | select(.type == "image")][0].url')

# Download via proxy
curl "http://localhost:8080/api/proxy?url=$(printf %s "$IMAGE_URL" | jq -sRr @uri)" \
  --output image.jpg
```

### Download Converted Video

```bash
# Get media list
MEDIA=$(curl -s http://localhost:8080/api/media)

# Extract first video proxy URL
VIDEO_PROXY=$(echo "$MEDIA" | jq -r '[.[] | select(.type == "video")][0].proxyUrl')

# Download converted MP4
curl "http://localhost:8080${VIDEO_PROXY}" --output video.mp4
```

### Stream Video in Browser

```html
<video controls>
  <source src="/api/video/2025-11-21%2Frecord000%2FA251121_212356_212410.264.mp4"
          type="video/mp4">
</video>
```

---

## Integration Notes

### Building Custom Applications

The API is suitable for:
- Custom web interfaces
- Mobile applications
- Desktop clients
- Automation scripts
- Media management tools

### Considerations

1. **No Pagination**: `/api/media` returns all media items. For cameras with thousands of recordings, consider implementing client-side pagination or filtering
2. **First-Request Latency**: Initial video requests trigger conversion, which may take several seconds. Enable background caching for better UX
3. **Cache Management**: The cache directory can grow large. Implement cache cleanup based on your retention needs
4. **Concurrent Access**: The API is thread-safe and can handle multiple concurrent clients
5. **Camera Load**: The 3-concurrent-request limit protects the camera from overload but may slow down bulk operations

---

## Dependencies

### Runtime

- **ffmpeg**: Required for video conversion (`/api/video/*` endpoint)
- **ffprobe**: Required for frame rate detection (part of ffmpeg package)

Install on Ubuntu/Debian:
```bash
sudo apt-get install ffmpeg
```

Install on macOS:
```bash
brew install ffmpeg
```

### Go Modules

- `golang.org/x/net/html`: HTML parsing for camera directory listings

---

## Version

Check the server version:

```bash
./ipcam-browser -version
```

---

## License

See the main project README for license information.
