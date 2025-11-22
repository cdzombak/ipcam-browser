package main

import (
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

//go:embed static
var staticFiles embed.FS

type Config struct {
	CameraURL string
	Username  string
	Password  string
}

type MediaItem struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	URL       string `json:"url"`
	ProxyURL  string `json:"proxyUrl"`
	Date      string `json:"date"`
	Type      string `json:"type"`
	Trigger   string `json:"trigger"`
	Timestamp string `json:"timestamp"`
	Size      string `json:"size"`
	Modified  string `json:"modified"`
}

type DirectoryEntry struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Modified    string `json:"modified"`
	Size        string `json:"size"`
	IsDirectory bool   `json:"isDirectory"`
}

var config Config

func main() {
	// Load config from environment
	config = Config{
		CameraURL: getEnv("CAMERA_URL", "http://192.168.205.196/web/sd"),
		Username:  getEnv("CAMERA_USERNAME", "admin"),
		Password:  getEnv("CAMERA_PASSWORD", "birdbath2"),
	}

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

	req, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		http.Error(w, "Failed to create request", http.StatusInternalServerError)
		return
	}

	req.Header.Set("Authorization", "Basic "+basicAuth(config.Username, config.Password))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "Failed to fetch from camera", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy headers
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
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

	// Fetch the raw video from camera
	req, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		http.Error(w, "Failed to create request", http.StatusInternalServerError)
		return
	}

	req.Header.Set("Authorization", "Basic "+basicAuth(config.Username, config.Password))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Failed to fetch video from camera: %v", err)
		http.Error(w, "Failed to fetch from camera", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		http.Error(w, fmt.Sprintf("Camera returned status %d", resp.StatusCode), resp.StatusCode)
		return
	}

	// Determine input format based on file extension
	inputFormat := "h264"
	if strings.HasSuffix(decodedPath, ".265") {
		inputFormat = "hevc"
	}

	// Use ffmpeg to remux raw H.264/H.265 to MP4
	// Add error concealment and browser-compatible settings
	cmd := exec.Command("ffmpeg",
		"-loglevel", "error", // Only show errors
		"-err_detect", "ignore_err", // Ignore decoding errors
		"-fflags", "+genpts", // Generate presentation timestamps
		"-f", inputFormat, // Input format
		"-i", "pipe:0", // Read from stdin
		"-c:v", "copy", // Don't re-encode video
		"-movflags", "frag_keyframe+empty_moov+default_base_moof", // Fragmented MP4 for streaming
		"-max_muxing_queue_size", "1024", // Increase buffer
		"-f", "mp4", // Output format
		"pipe:1", // Write to stdout
	)

	cmd.Stdin = resp.Body

	// Set content type
	w.Header().Set("Content-Type", "video/mp4")

	// Stream the output directly to the response
	cmd.Stdout = w

	// Capture errors
	stderr, err := cmd.StderrPipe()
	if err != nil {
		log.Printf("Failed to create stderr pipe: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if err := cmd.Start(); err != nil {
		log.Printf("Failed to start ffmpeg: %v", err)
		http.Error(w, "Failed to process video", http.StatusInternalServerError)
		return
	}

	// Log ffmpeg errors
	go func() {
		errOutput, _ := io.ReadAll(stderr)
		if len(errOutput) > 0 {
			log.Printf("ffmpeg stderr: %s", errOutput)
		}
	}()

	if err := cmd.Wait(); err != nil {
		log.Printf("ffmpeg error: %v", err)
	}
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

	return allMedia, nil
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

func parseMedia(entry DirectoryEntry, datePath string, mediaType string) MediaItem {
	name := entry.Name
	trigger := "periodic"
	if strings.HasPrefix(name, "A") {
		trigger = "alarm"
	}

	timestamp := parseTimestamp(name, mediaType)

	// Build proxy URL for videos (remux to MP4)
	proxyURL := ""
	if mediaType == "video" {
		// URL-encode the path and add .mp4 extension
		encodedPath := url.QueryEscape(entry.Path)
		proxyURL = "/api/video/" + encodedPath + ".mp4"
	}

	return MediaItem{
		Name:      name,
		Path:      entry.Path,
		URL:       config.CameraURL + "/" + entry.Path,
		ProxyURL:  proxyURL,
		Date:      strings.TrimSuffix(datePath, "/"),
		Type:      mediaType,
		Trigger:   trigger,
		Timestamp: timestamp,
		Size:      entry.Size,
		Modified:  entry.Modified,
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
