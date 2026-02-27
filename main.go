package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Session struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

const sessionsFile = "sessions.json"
const configFile = "config.json"

type Config struct {
	ArtMode string `json:"art_mode"` // "vinyl" or "picture"
}

var (
	appConfig   = Config{ArtMode: "vinyl"} // default
	appConfigMu sync.RWMutex
)

var (
	clientID     string
	clientSecret string
	redirectURI  = "http://127.0.0.1:8080/auth/callback"
	scopes       = "user-read-currently-playing user-read-playback-state"

	sessions   = map[string]*Session{}
	sessionsMu sync.RWMutex
)

func main() {
	loadEnv(".env")

	clientID = getEnv("SPOTIFY_CLIENT_ID", "")
	clientSecret = getEnv("SPOTIFY_CLIENT_SECRET", "")
	if clientID == "" || clientSecret == "" {
		log.Fatal("SPOTIFY_CLIENT_ID and SPOTIFY_CLIENT_SECRET must be set in .env or environment")
	}

	loadSessions()
	loadConfig()

	mux := http.NewServeMux()
	mux.HandleFunc("/auth/login", handleLogin)
	mux.HandleFunc("/auth/callback", handleCallback)
	mux.HandleFunc("/api/now-playing", handleNowPlaying)
	mux.HandleFunc("/api/config", handleConfig)
	mux.HandleFunc("/api/config/stream", handleConfigStream)
	mux.HandleFunc("/api/lyrics", handleLyrics)
	mux.HandleFunc("/lyrics", handleLyricsPage)
	mux.HandleFunc("/typed_lyrics", handleTypedLyricsPage)
	mux.HandleFunc("/api/session/export", handleSessionExport)
	mux.HandleFunc("/api/session/import", handleSessionImport)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	mux.HandleFunc("/", handleIndex)

	addr := ":8080"
	log.Printf("discify running at http://127.0.0.1%s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func loadEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		v = strings.Trim(v, `"'`)
		os.Setenv(k, v)
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func loadConfig() {
	data, err := os.ReadFile(configFile)
	if err != nil {
		log.Printf("No config.json found, using defaults")
		return
	}
	appConfigMu.Lock()
	defer appConfigMu.Unlock()
	if err := json.Unmarshal(data, &appConfig); err != nil {
		log.Printf("Failed to parse config.json: %v", err)
		return
	}
	log.Printf("Config loaded: art_mode=%s", appConfig.ArtMode)
}

func getConfig() Config {
	appConfigMu.RLock()
	defer appConfigMu.RUnlock()
	return appConfig
}

func handleConfig(w http.ResponseWriter, r *http.Request) {
	loadConfig()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(getConfig())
}

func handleConfigStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	loadConfig()
	data, _ := json.Marshal(getConfig())
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()

	var lastMod time.Time
	if info, err := os.Stat(configFile); err == nil {
		lastMod = info.ModTime()
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			info, err := os.Stat(configFile)
			if err != nil {
				continue
			}
			if info.ModTime().After(lastMod) {
				lastMod = info.ModTime()
				loadConfig()
				data, _ := json.Marshal(getConfig())
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			}
		}
	}
}

func loadSessions() {
	data, err := os.ReadFile(sessionsFile)
	if err != nil {
		return
	}
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	json.Unmarshal(data, &sessions)
	log.Printf("Loaded %d saved session(s)", len(sessions))
}

func saveSessions() {
	sessionsMu.RLock()
	data, err := json.Marshal(sessions)
	sessionsMu.RUnlock()
	if err != nil {
		log.Printf("Failed to marshal sessions: %v", err)
		return
	}
	os.WriteFile(sessionsFile, data, 0600)
}

func newSessionID() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func getSession(r *http.Request) *Session {
	cookie, err := r.Cookie("discify_session")
	if err != nil {
		return nil
	}
	sessionsMu.RLock()
	defer sessionsMu.RUnlock()
	return sessions[cookie.Value]
}

func setSession(w http.ResponseWriter, sess *Session) string {
	id := newSessionID()
	sessionsMu.Lock()
	sessions[id] = sess
	sessionsMu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name:     "discify_session",
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400 * 365,
	})
	saveSessions()
	return id
}

func ensureValidToken(sess *Session) error {
	if time.Now().Before(sess.ExpiresAt.Add(-30 * time.Second)) {
		return nil
	}
	data := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {sess.RefreshToken},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
	}
	resp, err := http.PostForm("https://accounts.spotify.com/api/token", data)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return err
	}
	sess.AccessToken = tok.AccessToken
	if tok.RefreshToken != "" {
		sess.RefreshToken = tok.RefreshToken
	}
	sess.ExpiresAt = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	saveSessions()
	return nil
}

func spotifyGet(sess *Session, endpoint string) (*http.Response, error) {
	if err := ensureValidToken(sess); err != nil {
		return nil, err
	}
	req, err := http.NewRequest("GET", "https://api.spotify.com/v1"+endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+sess.AccessToken)
	return http.DefaultClient.Do(req)
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, "static/index.html")
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	state := newSessionID()[:16]
	params := url.Values{
		"client_id":     {clientID},
		"response_type": {"code"},
		"redirect_uri":  {redirectURI},
		"scope":         {scopes},
		"state":         {state},
	}
	http.Redirect(w, r, "https://accounts.spotify.com/authorize?"+params.Encode(), http.StatusFound)
}

func handleCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "Missing authorization code", http.StatusBadRequest)
		return
	}

	data := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
	}
	resp, err := http.PostForm("https://accounts.spotify.com/api/token", data)
	if err != nil {
		http.Error(w, "Token exchange failed", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		http.Error(w, fmt.Sprintf("Spotify token error: %s", body), http.StatusBadGateway)
		return
	}

	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		http.Error(w, "Failed to parse token response", http.StatusInternalServerError)
		return
	}

	sess := &Session{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second),
	}
	setSession(w, sess)
	http.Redirect(w, r, "/", http.StatusFound)
}

func handleSessionExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sess := getSession(r)
	if sess == nil {
		http.Error(w, `{"error":"not_authenticated"}`, http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sess)
}

func handleSessionImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var sess Session
	if err := json.NewDecoder(r.Body).Decode(&sess); err != nil {
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}
	if sess.RefreshToken == "" {
		http.Error(w, `{"error":"refresh_token required"}`, http.StatusBadRequest)
		return
	}
	s := &Session{
		AccessToken:  sess.AccessToken,
		RefreshToken: sess.RefreshToken,
		ExpiresAt:    sess.ExpiresAt,
	}
	setSession(w, s)
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`))
}


type LyricLine struct {
	TimeMs int    `json:"time_ms"`
	Text   string `json:"text"`
}

type LyricsResponse struct {
	Synced bool        `json:"synced"`
	Lines  []LyricLine `json:"lines"`
}

var lrcLineRe = regexp.MustCompile(`^\[(\d{2}):(\d{2})\.(\d{2,3})\]\s?(.*)$`)

func parseLRC(lrc string) []LyricLine {
	var lines []LyricLine
	for _, raw := range strings.Split(lrc, "\n") {
		raw = strings.TrimSpace(raw)
		m := lrcLineRe.FindStringSubmatch(raw)
		if m == nil {
			continue
		}
		min, _ := strconv.Atoi(m[1])
		sec, _ := strconv.Atoi(m[2])
		frac, _ := strconv.Atoi(m[3])
		// norm to ms: "93" -> 930, "930" -> 930
		if len(m[3]) == 2 {
			frac *= 10
		}
		ms := min*60000 + sec*1000 + frac
		lines = append(lines, LyricLine{TimeMs: ms, Text: m[4]})
	}
	return lines
}

func handleLyrics(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	track := q.Get("track")
	artist := q.Get("artist")
	if track == "" || artist == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(LyricsResponse{})
		return
	}

	params := url.Values{
		"track_name":  {track},
		"artist_name": {artist},
	}
	if album := q.Get("album"); album != "" {
		params.Set("album_name", album)
	}
	if dur := q.Get("duration"); dur != "" {
		if d, err := strconv.Atoi(dur); err == nil {
			params.Set("duration", strconv.Itoa(int(math.Round(float64(d)/1000.0))))
		}
	}

	req, err := http.NewRequest("GET", "https://lrclib.net/api/get?"+params.Encode(), nil)
	if err != nil {
		http.Error(w, `{"error":"request_build_failed"}`, http.StatusInternalServerError)
		return
	}
	req.Header.Set("User-Agent", "discify/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, `{"error":"lrclib_request_failed"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(LyricsResponse{})
		return
	}

	var lrcResp struct {
		SyncedLyrics string `json:"syncedLyrics"`
		PlainLyrics  string `json:"plainLyrics"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&lrcResp); err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(LyricsResponse{})
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if lrcResp.SyncedLyrics != "" {
		lines := parseLRC(lrcResp.SyncedLyrics)
		json.NewEncoder(w).Encode(LyricsResponse{Synced: true, Lines: lines})
		return
	}

	if lrcResp.PlainLyrics != "" {
		var lines []LyricLine
		for _, l := range strings.Split(lrcResp.PlainLyrics, "\n") {
			l = strings.TrimSpace(l)
			lines = append(lines, LyricLine{TimeMs: 0, Text: l})
		}
		json.NewEncoder(w).Encode(LyricsResponse{Synced: false, Lines: lines})
		return
	}

	json.NewEncoder(w).Encode(LyricsResponse{})
}

func handleLyricsPage(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "static/lyrics.html")
}

func handleTypedLyricsPage(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "static/typed_lyrics.html")
}

func handleNowPlaying(w http.ResponseWriter, r *http.Request) {
	sess := getSession(r)
	if sess == nil {
		http.Error(w, `{"error":"not_authenticated"}`, http.StatusUnauthorized)
		return
	}

	resp, err := spotifyGet(sess, "/me/player/currently-playing")
	if err != nil {
		http.Error(w, `{"error":"spotify_request_failed"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	if resp.StatusCode == 204 {
		w.Write([]byte(`{"is_playing":false}`))
		return
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}
