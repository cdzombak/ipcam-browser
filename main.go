package main

import (
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
)

var version = "<dev>"

//go:embed static
var staticFiles embed.FS

type Config struct {
	CameraURL               string
	CameraName              string
	Username                string
	Password                string
	CacheDir                string
	MaxConcurrentConversions int
}

// MediaCache handles thread-safe caching of media files
type MediaCache struct {
	dir       string
	locks     sync.Map      // per-file mutexes for cache operations
	cameraSem chan struct{} // semaphore to limit concurrent camera requests
}

// NewMediaCache creates a new cache instance
func NewMediaCache(dir string) (*MediaCache, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}
	return &MediaCache{
		dir:       dir,
		cameraSem: make(chan struct{}, 3), // Limit to 3 concurrent camera requests
	}, nil
}

// getCacheKey generates a unique cache key for a URL
func (c *MediaCache) getCacheKey(url string, suffix string) string {
	hash := sha256.Sum256([]byte(url))
	return hex.EncodeToString(hash[:]) + suffix
}

// getCachePath returns the full path for a cache file
func (c *MediaCache) getCachePath(url string, suffix string) string {
	return filepath.Join(c.dir, c.getCacheKey(url, suffix))
}

// getFileLock gets or creates a mutex for a specific cache file
func (c *MediaCache) getFileLock(cacheKey string) *sync.Mutex {
	lock, _ := c.locks.LoadOrStore(cacheKey, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

// Get retrieves a file from cache, or executes fetchFunc if not cached
// This ensures only one goroutine fetches a given file at a time
func (c *MediaCache) Get(url string, suffix string, fetchFunc func() ([]byte, error)) (string, error) {
	cachePath := c.getCachePath(url, suffix)
	cacheKey := c.getCacheKey(url, suffix)

	// Fast path: check if file exists in cache (no lock needed)
	if _, err := os.Stat(cachePath); err == nil {
		return cachePath, nil
	}

	// Get the lock for this specific cache key to serialize processing
	fileLock := c.getFileLock(cacheKey)
	fileLock.Lock()
	defer fileLock.Unlock()

	// Double-check: file might have been created while we waited for lock
	// This is the key optimization - if another goroutine already processed it,
	// we just return the path without doing any work
	if _, err := os.Stat(cachePath); err == nil {
		return cachePath, nil
	}

	// At this point, we hold the lock and the file doesn't exist
	// We are the only goroutine that will process this file
	// Any other goroutines will wait on the lock above, then hit the
	// double-check and return immediately

	// Fetch the file
	data, err := fetchFunc()
	if err != nil {
		return "", fmt.Errorf("fetch failed: %w", err)
	}

	// Write to temporary file first (atomic operation)
	tempFile, err := os.CreateTemp(c.dir, "temp-*"+suffix)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath) // Clean up temp file if rename fails

	if _, err := tempFile.Write(data); err != nil {
		tempFile.Close()
		return "", fmt.Errorf("failed to write temp file: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return "", fmt.Errorf("failed to close temp file: %w", err)
	}

	// Atomic rename to final location
	if err := os.Rename(tempPath, cachePath); err != nil {
		return "", fmt.Errorf("failed to rename cache file: %w", err)
	}

	return cachePath, nil
}

// GetWithFile is like Get but uses a file-based fetch function
// This is more efficient for large files that are already on disk
func (c *MediaCache) GetWithFile(url string, suffix string, fetchFunc func(destPath string) error) (string, error) {
	cachePath := c.getCachePath(url, suffix)
	cacheKey := c.getCacheKey(url, suffix)

	// Fast path: check if file exists in cache (no lock needed)
	if _, err := os.Stat(cachePath); err == nil {
		return cachePath, nil
	}

	// Get the lock for this specific cache key to serialize processing
	fileLock := c.getFileLock(cacheKey)
	fileLock.Lock()
	defer fileLock.Unlock()

	// Double-check: file might have been created while we waited for lock
	// This is the key optimization - if another goroutine already processed it,
	// we just return the path without doing any work
	if _, err := os.Stat(cachePath); err == nil {
		return cachePath, nil
	}

	// At this point, we hold the lock and the file doesn't exist
	// We are the only goroutine that will process this file
	// Any other goroutines will wait on the lock above, then hit the
	// double-check and return immediately

	// Create temporary file
	tempFile, err := os.CreateTemp(c.dir, "temp-*"+suffix)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	tempPath := tempFile.Name()
	tempFile.Close()
	defer os.Remove(tempPath)

	// Fetch directly to temp file
	if err := fetchFunc(tempPath); err != nil {
		return "", fmt.Errorf("fetch failed: %w", err)
	}

	// Atomic rename to final location
	if err := os.Rename(tempPath, cachePath); err != nil {
		return "", fmt.Errorf("failed to rename cache file: %w", err)
	}

	return cachePath, nil
}

type MediaItem struct {
	Name             string `json:"name"`
	Path             string `json:"path"`
	URL              string `json:"url"`
	ProxyURL         string `json:"proxyUrl"`
	ThumbnailURL     string `json:"thumbnailUrl,omitempty"`
	DownloadFilename string `json:"downloadFilename"`
	Date             string `json:"date"`
	Type             string `json:"type"`
	Trigger          string `json:"trigger"`
	Timestamp        string `json:"timestamp"`
	Size             string `json:"size"`
	Modified         string `json:"modified"`
}

type DirectoryEntry struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Modified    string `json:"modified"`
	Size        string `json:"size"`
	IsDirectory bool   `json:"isDirectory"`
}

var config Config
var mediaCache *MediaCache

func main() {
	// Parse flags
	showVersion := flag.Bool("version", false, "Show version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		os.Exit(0)
	}

	// Load config from environment
	config = Config{
		CameraURL:               getEnv("CAMERA_URL", "http://192.168.205.196/web/sd"),
		CameraName:              getEnv("CAMERA_NAME", "camera"),
		Username:                getEnv("CAMERA_USERNAME", "admin"),
		Password:                getEnv("CAMERA_PASSWORD", "birdbath2"),
		CacheDir:                getEnv("CACHE_DIR", filepath.Join(os.TempDir(), "ipcam-browser-cache")),
		MaxConcurrentConversions: getEnvInt("MAX_CONCURRENT_CONVERSIONS", 3),
	}

	// Initialize cache
	var err error
	mediaCache, err = NewMediaCache(config.CacheDir)
	if err != nil {
		log.Fatalf("Failed to initialize cache: %v", err)
	}
	log.Printf("Cache directory: %s", config.CacheDir)

	http.HandleFunc("/api/config", handleGetConfig)
	http.HandleFunc("/api/media", handleGetMedia)
	http.HandleFunc("/api/proxy", handleProxy)
	http.HandleFunc("/api/video/", handleVideoProxy)

	// Serve embedded static files
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatal(err)
	}
	http.Handle("/", http.FileServer(http.FS(staticFS)))

	port := getEnv("PORT", "8080")
	log.Printf("Starting server on http://localhost:%s", port)
	log.Printf("Camera URL: %s", config.CameraURL)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func handleGetConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"cameraName": config.CameraName,
	})
}

func handleGetMedia(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	media, err := fetchAllMedia()
	if err != nil {
		log.Printf("Error fetching media: %v", err)
		http.Error(w, fmt.Sprintf("Failed to fetch media: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(media)
}

func handleProxy(w http.ResponseWriter, r *http.Request) {
	targetURL := r.URL.Query().Get("url")
	if targetURL == "" {
		http.Error(w, "Missing url parameter", http.StatusBadRequest)
		return
	}

	// Ensure URL is for our camera
	if !strings.HasPrefix(targetURL, config.CameraURL) {
		http.Error(w, "Invalid URL", http.StatusBadRequest)
		return
	}

	// Determine file extension for cache key
	ext := filepath.Ext(targetURL)
	if ext == "" {
		ext = ".bin" // fallback for files without extension
	}

	// Try to get from cache, or fetch if not cached
	cachedPath, err := mediaCache.Get(targetURL, ext, func() ([]byte, error) {
		return fetchFromCamera(targetURL)
	})

	if err != nil {
		log.Printf("Cache error for %s: %v", targetURL, err)
		http.Error(w, "Failed to fetch media", http.StatusInternalServerError)
		return
	}

	// Serve the cached file
	http.ServeFile(w, r, cachedPath)
}

// fetchFromCamera downloads a file from the camera
func fetchFromCamera(targetURL string) ([]byte, error) {
	// Acquire semaphore to limit concurrent camera requests
	mediaCache.cameraSem <- struct{}{}
	defer func() { <-mediaCache.cameraSem }()

	req, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Basic "+basicAuth(config.Username, config.Password))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch from camera: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("camera returned status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return data, nil
}

func handleVideoProxy(w http.ResponseWriter, r *http.Request) {
	// Extract the video path from the URL
	// URL format: /api/video/{encoded-path}.mp4
	path := strings.TrimPrefix(r.URL.Path, "/api/video/")
	path = strings.TrimSuffix(path, ".mp4")

	// Decode the path
	decodedPath, err := url.QueryUnescape(path)
	if err != nil {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	// Build the camera URL
	targetURL := config.CameraURL + "/" + decodedPath

	// Ensure URL is for our camera
	if !strings.HasPrefix(targetURL, config.CameraURL) {
		http.Error(w, "Invalid URL", http.StatusBadRequest)
		return
	}

	// Try to get converted video from cache, or convert if not cached
	cachedPath, err := mediaCache.GetWithFile(targetURL, ".mp4", func(destPath string) error {
		return convertVideoToMP4(targetURL, destPath)
	})

	if err != nil {
		log.Printf("Video conversion error for %s: %v", targetURL, err)
		http.Error(w, "Failed to convert video", http.StatusInternalServerError)
		return
	}

	// Serve the cached converted video
	http.ServeFile(w, r, cachedPath)
}

// stripHXVSHeaders removes HXVS/HXVF 16-byte headers from raw H.264/H.265 stream
// These proprietary headers prevent the video from playing in most video players
func stripHXVSHeaders(data []byte) []byte {
	out := make([]byte, 0, len(data))
	i := 0
	removed := 0
	length := len(data)

	for i < length {
		// Check for HXVS or HXVF header (4 bytes + 12 more = 16 bytes total)
		if i+16 <= length {
			header := data[i : i+4]
			if string(header) == "HXVS" || string(header) == "HXVF" {
				// Skip the 16-byte header
				i += 16
				removed += 16
				continue
			}
		}
		out = append(out, data[i])
		i++
	}

	if removed > 0 {
		log.Printf("Stripped %d bytes of HXVS/HXVF headers from video", removed)
	}

	return out
}

// detectFPS tries to detect the frame rate from a video file using ffprobe
// Returns the detected FPS or 0 if detection fails
func detectFPS(path string) int {
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=r_frame_rate,avg_frame_rate",
		"-of", "default=nk=1:nw=1",
		path,
	)

	output, err := cmd.Output()
	if err != nil {
		return 0
	}

	// Parse frame rate from output (format: "num/den" or "fps")
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "/") {
			// Format: "30000/1001" or "25/1"
			parts := strings.Split(line, "/")
			if len(parts) == 2 {
				num := parseFloat(parts[0])
				den := parseFloat(parts[1])
				if den != 0 {
					fps := num / den
					if fps > 0 {
						return int(fps + 0.5) // Round to nearest int
					}
				}
			}
		} else {
			// Format: "25.0" or "30"
			fps := parseFloat(line)
			if fps > 0 {
				return int(fps + 0.5)
			}
		}
	}

	return 0
}

// parseFloat safely parses a string to float64, returning 0 on error
func parseFloat(s string) float64 {
	f := 0.0
	fmt.Sscanf(s, "%f", &f)
	return f
}

// convertVideoToMP4 downloads a raw video from camera and converts it to MP4
func convertVideoToMP4(sourceURL string, destPath string) error {
	// Download raw video from camera
	rawData, err := fetchFromCamera(sourceURL)
	if err != nil {
		return fmt.Errorf("failed to fetch video: %w", err)
	}

	// Strip HXVS/HXVF headers that prevent playback in most video players
	cleanedData := stripHXVSHeaders(rawData)

	// Determine input format based on file extension
	inputFormat := "h264"
	if strings.HasSuffix(sourceURL, ".265") {
		inputFormat = "hevc"
	}

	// Create temporary file for cleaned video
	tempFile, err := os.CreateTemp("", "clean-video-*."+inputFormat)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	// Write cleaned video to temp file
	if _, err := tempFile.Write(cleanedData); err != nil {
		return fmt.Errorf("failed to write cleaned video: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	// Detect frame rate from the cleaned video
	fps := detectFPS(tempFile.Name())
	if fps == 0 {
		fps = 20 // Default fallback
		log.Printf("Could not detect FPS for %s, defaulting to 20", sourceURL)
	} else {
		log.Printf("Detected FPS for %s: %d", sourceURL, fps)
	}

	// Convert to MP4 using ffmpeg with proper framerate
	cmd := exec.Command("ffmpeg",
		"-y",                       // Overwrite output file without asking
		"-fflags", "+genpts",       // Generate presentation timestamps
		"-framerate", fmt.Sprintf("%d", fps), // Set input framerate
		"-i", tempFile.Name(),      // Input file
		"-c:v", "copy",             // Copy video codec (no re-encoding)
		"-c:a", "copy",             // Copy audio codec (preserve audio if present)
		"-movflags", "+faststart",  // Put moov atom at start for better compatibility
		destPath,                   // Output file
	)

	// Run ffmpeg and capture errors
	errOutput, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg failed: %v, output: %s", err, string(errOutput))
	}
	if len(errOutput) > 0 {
		log.Printf("ffmpeg output for %s: %s", sourceURL, string(errOutput))
	}

	return nil
}

func fetchAllMedia() ([]MediaItem, error) {
	var allMedia []MediaItem

	// Fetch root directory
	dates, err := fetchDirectory("")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch root directory: %w", err)
	}

	// Iterate through date directories
	for _, date := range dates {
		if !date.IsDirectory {
			continue
		}

		dateMedia, err := fetchDateMedia(date.Name)
		if err != nil {
			log.Printf("Warning: failed to fetch media for %s: %v", date.Name, err)
			continue
		}

		allMedia = append(allMedia, dateMedia...)
	}

	// Pre-cache videos in the background for instant playback
	go preCacheVideos(allMedia)

	return allMedia, nil
}

// preCacheVideos pre-converts videos to MP4 in the background
func preCacheVideos(media []MediaItem) {
	// Create a semaphore to limit concurrent video conversions
	sem := make(chan struct{}, config.MaxConcurrentConversions)

	for _, item := range media {
		if item.Type != "video" {
			continue
		}

		sem <- struct{}{} // Acquire
		go func(videoURL string) {
			defer func() { <-sem }() // Release

			// Try to get/create cached MP4 - this will trigger conversion if not cached
			_, err := mediaCache.GetWithFile(videoURL, ".mp4", func(destPath string) error {
				return convertVideoToMP4(videoURL, destPath)
			})
			if err != nil {
				log.Printf("Pre-cache failed for %s: %v", videoURL, err)
			}
		}(item.URL)
	}
}

// matchVideoThumbnails finds and assigns thumbnail images to videos
// Prefers images taken during the video, falls back to 1 second before
func matchVideoThumbnails(media []MediaItem) {
	// Build index of images by timestamp
	images := make(map[string]*MediaItem)
	for i := range media {
		if media[i].Type == "image" {
			images[media[i].Timestamp] = &media[i]
		}
	}

	// Match each video with the best thumbnail
	for i := range media {
		if media[i].Type != "video" {
			continue
		}

		// Parse video timestamp range "2025-11-21 21:23:56 - 21:24:10"
		parts := strings.Split(media[i].Timestamp, " - ")
		if len(parts) != 2 {
			continue
		}

		startTime := strings.TrimSpace(parts[0])
		endTime := strings.TrimSpace(parts[1])

		// Parse times to compare
		startParsed, err := time.Parse("2006-01-02 15:04:05", startTime)
		if err != nil {
			continue
		}

		// For end time, we may need to prepend the date
		var endParsed time.Time
		if strings.Contains(endTime, "-") {
			// Full timestamp
			endParsed, err = time.Parse("2006-01-02 15:04:05", endTime)
		} else {
			// Time only, use same date as start
			endParsed, err = time.Parse("2006-01-02 15:04:05", startTime[:11]+endTime)
		}
		if err != nil {
			continue
		}

		var bestMatch *MediaItem
		var duringVideo *MediaItem
		var beforeVideo *MediaItem

		// Look for matching images
		for _, img := range images {
			imgParsed, err := time.Parse("2006-01-02 15:04:05", img.Timestamp)
			if err != nil {
				continue
			}

			// Check if image is during video (preferred)
			if (imgParsed.Equal(startParsed) || imgParsed.After(startParsed)) && imgParsed.Before(endParsed) {
				if duringVideo == nil || imgParsed.Before(mustParseTime(duringVideo.Timestamp)) {
					duringVideo = img
				}
			}

			// Check if image is 1 second before video start (fallback)
			if imgParsed.Equal(startParsed.Add(-1 * time.Second)) {
				beforeVideo = img
			}
		}

		// Prefer during video, fallback to before video
		if duringVideo != nil {
			bestMatch = duringVideo
		} else if beforeVideo != nil {
			bestMatch = beforeVideo
		}

		// Set thumbnail URL if we found a match
		if bestMatch != nil {
			media[i].ThumbnailURL = "/api/proxy?url=" + url.QueryEscape(bestMatch.URL)
		}
	}
}

// mustParseTime parses a time or panics (for use in comparisons where we know format is valid)
func mustParseTime(s string) time.Time {
	t, err := time.Parse("2006-01-02 15:04:05", s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func fetchDateMedia(datePath string) ([]MediaItem, error) {
	var media []MediaItem

	entries, err := fetchDirectory(datePath)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDirectory {
			continue
		}

		dirName := strings.TrimSuffix(entry.Name, "/")

		if dirName == "images000" {
			images, err := fetchDirectory(entry.Path)
			if err != nil {
				log.Printf("Warning: failed to fetch images from %s: %v", entry.Path, err)
				continue
			}

			for _, img := range images {
				if strings.HasSuffix(img.Name, ".jpg") {
					media = append(media, parseMedia(img, datePath, "image"))
				}
			}
		} else if dirName == "record000" {
			videos, err := fetchDirectory(entry.Path)
			if err != nil {
				log.Printf("Warning: failed to fetch videos from %s: %v", entry.Path, err)
				continue
			}

			for _, vid := range videos {
				if strings.HasSuffix(vid.Name, ".264") || strings.HasSuffix(vid.Name, ".265") {
					media = append(media, parseMedia(vid, datePath, "video"))
				}
			}
		}
	}

	// Match videos with their thumbnail images
	matchVideoThumbnails(media)

	return media, nil
}

func fetchDirectory(path string) ([]DirectoryEntry, error) {
	url := config.CameraURL + "/" + path

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Basic "+basicAuth(config.Username, config.Password))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return parseDirectory(string(body), path), nil
}

func parseDirectory(htmlContent string, basePath string) []DirectoryEntry {
	var entries []DirectoryEntry

	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		log.Printf("Error parsing HTML: %v", err)
		return entries
	}

	var traverse func(*html.Node)
	traverse = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "tr" {
			entry := parseTableRow(n, basePath)
			if entry != nil {
				entries = append(entries, *entry)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			traverse(c)
		}
	}

	traverse(doc)
	return entries
}

func parseTableRow(tr *html.Node, basePath string) *DirectoryEntry {
	var cells []*html.Node

	for c := tr.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == "td" {
			cells = append(cells, c)
		}
	}

	if len(cells) < 3 {
		return nil
	}

	// Extract link from first cell
	var link *html.Node
	for c := cells[0].FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == "a" {
			link = c
			break
		}
	}

	if link == nil {
		return nil
	}

	name := getTextContent(link)
	if name == "Parent directory" {
		return nil
	}

	modified := strings.TrimSpace(getTextContent(cells[1]))
	size := strings.TrimSpace(getTextContent(cells[2]))

	// Build path without double slashes
	path := name
	if basePath != "" {
		// Remove trailing slash from basePath and leading/trailing slash from name
		cleanBase := strings.TrimSuffix(basePath, "/")
		cleanName := strings.Trim(name, "/")
		if strings.HasSuffix(name, "/") {
			cleanName += "/"
		}
		path = cleanBase + "/" + cleanName
	}

	return &DirectoryEntry{
		Name:        name,
		Path:        path,
		Modified:    modified,
		Size:        size,
		IsDirectory: strings.HasSuffix(name, "/"),
	}
}

// generateDownloadFilename creates a filename in format: <camera>_yyyy-MM-dd_HH-mm-ss.ext
func generateDownloadFilename(timestamp, originalName, mediaType string) string {
	// Extract the start time from timestamp
	// For images: "2025-11-21 21:23:56"
	// For videos: "2025-11-21 21:23:56 - 21:24:10"
	var timeStr string
	if strings.Contains(timestamp, " - ") {
		// Video: use start time
		timeStr = strings.Split(timestamp, " - ")[0]
	} else {
		// Image: use the whole timestamp
		timeStr = timestamp
	}

	// Parse the timestamp to reformat it
	t, err := time.Parse("2006-01-02 15:04:05", timeStr)
	if err != nil {
		// If parsing fails, use the original name
		return originalName
	}

	// Get file extension
	ext := filepath.Ext(originalName)
	if ext == "" {
		// For files without extension, use defaults
		if mediaType == "video" {
			ext = ".mp4"
		} else {
			ext = ".jpg"
		}
	} else if mediaType == "video" {
		// Videos are served as MP4, so use .mp4 extension regardless of original
		ext = ".mp4"
	}

	// Format as: camera_2025-11-21_21-23-56.ext
	formatted := t.Format("2006-01-02_15-04-05")
	return fmt.Sprintf("%s_%s%s", config.CameraName, formatted, ext)
}

func parseMedia(entry DirectoryEntry, datePath string, mediaType string) MediaItem {
	name := entry.Name
	trigger := "periodic"
	if strings.HasPrefix(name, "A") {
		trigger = "alarm"
	}

	timestamp := parseTimestamp(name, mediaType)

	// Build proxy URL for videos
	proxyURL := ""
	if mediaType == "video" {
		// URL-encode the path and add extensions
		encodedPath := url.QueryEscape(entry.Path)
		proxyURL = "/api/video/" + encodedPath + ".mp4"
	}

	// Generate download filename
	downloadFilename := generateDownloadFilename(timestamp, name, mediaType)

	return MediaItem{
		Name:             name,
		Path:             entry.Path,
		URL:              config.CameraURL + "/" + entry.Path,
		ProxyURL:         proxyURL,
		DownloadFilename: downloadFilename,
		Date:             strings.TrimSuffix(datePath, "/"),
		Type:             mediaType,
		Trigger:          trigger,
		Timestamp:        timestamp,
		Size:             entry.Size,
		Modified:         entry.Modified,
	}
}

func parseTimestamp(name string, mediaType string) string {
	if mediaType == "image" {
		// AYYMMDDHHMMSSXX.jpg
		re := regexp.MustCompile(`[AP](\d{2})(\d{2})(\d{2})(\d{2})(\d{2})(\d{2})`)
		matches := re.FindStringSubmatch(name)
		if len(matches) == 7 {
			return fmt.Sprintf("20%s-%s-%s %s:%s:%s",
				matches[1], matches[2], matches[3], matches[4], matches[5], matches[6])
		}
	} else {
		// AYYMMDDHHMMSSHHMMSSS.264
		re := regexp.MustCompile(`[AP](\d{2})(\d{2})(\d{2})_(\d{2})(\d{2})(\d{2})_(\d{2})(\d{2})(\d{2})`)
		matches := re.FindStringSubmatch(name)
		if len(matches) == 10 {
			return fmt.Sprintf("20%s-%s-%s %s:%s:%s - %s:%s:%s",
				matches[1], matches[2], matches[3],
				matches[4], matches[5], matches[6],
				matches[7], matches[8], matches[9])
		}
	}

	return ""
}

func getTextContent(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}

	var text string
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		text += getTextContent(c)
	}
	return text
}

func basicAuth(username, password string) string {
	auth := username + ":" + password
	return base64.StdEncoding.EncodeToString([]byte(auth))
}

func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

func getEnvInt(key string, defaultValue int) int {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	intValue := defaultValue
	fmt.Sscanf(value, "%d", &intValue)
	return intValue
}
