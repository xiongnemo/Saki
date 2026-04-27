package subsonic

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xiongnemo/saki/internal/models"
)

const (
	clientName = "Saki-Go"
	apiVersion = "1.16.1"
)

type Client struct {
	httpClient *http.Client

	mu        sync.RWMutex
	account   models.Account
	settings  models.Settings
	endpoints []endpointRuntime
	active    int
}

type endpointRuntime struct {
	models.Endpoint
	Latency   time.Duration
	EWMA      time.Duration
	LastOK    time.Time
	Failures  int
	Successes int
}

func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{httpClient: httpClient}
}

func (c *Client) Configure(cfg models.Config) models.Config {
	c.mu.Lock()
	defer c.mu.Unlock()

	account := cfg.Account
	if !account.UsePlaintext {
		if account.Salt == "" {
			account.Salt = generateSalt()
		}
		if account.Token == "" {
			account.Token = generateToken(account.Password, account.Salt)
		}
	}
	if len(account.Endpoints) == 0 && strings.TrimSpace(account.URL) != "" {
		account.Endpoints = []models.Endpoint{{Name: "Default", URL: account.URL, Enabled: true}}
	}
	for i := range account.Endpoints {
		account.Endpoints[i].URL = strings.TrimRight(strings.TrimSpace(account.Endpoints[i].URL), "/")
		if account.Endpoints[i].Name == "" {
			account.Endpoints[i].Name = "Endpoint " + strconv.Itoa(i+1)
		}
	}
	account.URL = ""

	c.account = account
	c.settings = cfg.Settings
	if c.settings.HealthCheckIntervalSeconds <= 0 {
		c.settings.HealthCheckIntervalSeconds = 5
	}
	if c.settings.EndpointTimeoutSeconds <= 0 {
		c.settings.EndpointTimeoutSeconds = 2
	}
	if c.settings.EndpointSwitchThreshold <= 0 {
		c.settings.EndpointSwitchThreshold = 0.30
	}
	c.endpoints = make([]endpointRuntime, 0, len(account.Endpoints))
	for _, endpoint := range account.Endpoints {
		c.endpoints = append(c.endpoints, endpointRuntime{Endpoint: endpoint})
	}
	c.active = c.firstEnabledLocked()

	cfg.Account = account
	cfg.Settings = c.settings
	return cfg
}

func (c *Client) ActiveAccount() models.Account {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.account
}

func (c *Client) ActiveEndpoint() models.Endpoint {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.active >= 0 && c.active < len(c.endpoints) {
		return c.endpoints[c.active].Endpoint
	}
	return models.Endpoint{}
}

func (c *Client) EndpointStatuses() []EndpointStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()

	statuses := make([]EndpointStatus, 0, len(c.endpoints))
	for i, endpoint := range c.endpoints {
		statuses = append(statuses, EndpointStatus{
			Endpoint:  endpoint.Endpoint,
			Active:    i == c.active,
			Latency:   endpoint.Latency,
			EWMA:      endpoint.EWMA,
			LastOK:    endpoint.LastOK,
			Failures:  endpoint.Failures,
			Successes: endpoint.Successes,
		})
	}
	return statuses
}

type EndpointStatus struct {
	Endpoint  models.Endpoint
	Active    bool
	Latency   time.Duration
	EWMA      time.Duration
	LastOK    time.Time
	Failures  int
	Successes int
}

type EndpointProbe struct {
	Endpoint      models.Endpoint
	Latency       time.Duration
	ServerVersion string
	ServerType    string
	Err           error
}

type EndpointIdentity struct {
	Endpoint      models.Endpoint
	Fingerprint   string
	ArtistCount   int
	ServerVersion string
	ServerType    string
	Err           error
}

func (c *Client) ProbeEndpoints(ctx context.Context, endpoints []models.Endpoint) []EndpointProbe {
	results := make([]EndpointProbe, len(endpoints))
	if len(endpoints) == 0 {
		return results
	}

	var wg sync.WaitGroup
	for i, endpoint := range endpoints {
		i, endpoint := i, normalizeProbeEndpoint(endpoint)
		results[i].Endpoint = endpoint
		if strings.TrimSpace(endpoint.URL) == "" {
			results[i].Err = fmt.Errorf("empty endpoint URL")
			continue
		}
		if !endpoint.Enabled {
			results[i].Err = fmt.Errorf("endpoint disabled")
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			start := time.Now()
			info, err := c.pingEndpointInfo(ctx, endpoint.URL)
			results[i].Latency = time.Since(start)
			results[i].ServerVersion = info.Version
			results[i].ServerType = info.Type
			results[i].Err = err
		}()
	}
	wg.Wait()
	return results
}

func (c *Client) ProbeEndpointIdentities(ctx context.Context, endpoints []models.Endpoint) []EndpointIdentity {
	results := make([]EndpointIdentity, len(endpoints))
	if len(endpoints) == 0 {
		return results
	}

	var wg sync.WaitGroup
	for i, endpoint := range endpoints {
		i, endpoint := i, normalizeProbeEndpoint(endpoint)
		results[i].Endpoint = endpoint
		if strings.TrimSpace(endpoint.URL) == "" {
			results[i].Err = fmt.Errorf("empty endpoint URL")
			continue
		}
		if !endpoint.Enabled {
			results[i].Err = fmt.Errorf("endpoint disabled")
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			identity, err := c.endpointIdentity(ctx, endpoint)
			identity.Endpoint = endpoint
			identity.Err = err
			results[i] = identity
		}()
	}
	wg.Wait()
	return results
}

func normalizeProbeEndpoint(endpoint models.Endpoint) models.Endpoint {
	endpoint.URL = strings.TrimRight(strings.TrimSpace(endpoint.URL), "/")
	return endpoint
}

func (c *Client) StartHealthChecks(ctx context.Context) {
	interval := time.Duration(c.healthIntervalSeconds()) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}

	go func() {
		c.checkAllEndpoints(ctx)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				c.checkAllEndpoints(ctx)
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (c *Client) GetArtists(ctx context.Context) ([]models.Artist, error) {
	var wrapped response[artistsResponse]
	if err := c.get(ctx, "getArtists", nil, &wrapped); err != nil {
		return nil, err
	}
	if err := checkStatus(wrapped.Data.baseResponse); err != nil {
		return nil, err
	}

	var artists []models.Artist
	if wrapped.Data.Artists == nil {
		return artists, nil
	}
	for _, index := range wrapped.Data.Artists.Index {
		for _, artist := range index.Artist {
			artists = append(artists, mapArtist(artist))
		}
	}
	return artists, nil
}

func (c *Client) GetArtist(ctx context.Context, id string) (models.Artist, error) {
	var wrapped response[artistResponse]
	if err := c.get(ctx, "getArtist", map[string]string{"id": id}, &wrapped); err != nil {
		return models.Artist{}, err
	}
	if err := checkStatus(wrapped.Data.baseResponse); err != nil {
		return models.Artist{}, err
	}
	if wrapped.Data.Artist == nil {
		return models.Artist{}, fmt.Errorf("artist %q not found", id)
	}

	artist := models.Artist{
		ID:         wrapped.Data.Artist.ID,
		Name:       wrapped.Data.Artist.Name,
		AlbumCount: wrapped.Data.Artist.AlbumCount,
	}
	for _, album := range wrapped.Data.Artist.Album {
		artist.Albums = append(artist.Albums, mapAlbum(album))
	}
	return artist, nil
}

func (c *Client) GetAlbum(ctx context.Context, id string) (models.Album, error) {
	var wrapped response[albumResponse]
	if err := c.get(ctx, "getAlbum", map[string]string{"id": id}, &wrapped); err != nil {
		return models.Album{}, err
	}
	if err := checkStatus(wrapped.Data.baseResponse); err != nil {
		return models.Album{}, err
	}
	if wrapped.Data.Album == nil {
		return models.Album{}, fmt.Errorf("album %q not found", id)
	}

	album := mapAlbum(wrapped.Data.Album.albumJSON)
	for _, song := range wrapped.Data.Album.Song {
		album.Songs = append(album.Songs, mapSong(song))
	}
	return album, nil
}

func (c *Client) GetAlbums(ctx context.Context, listType string, size int) ([]models.Album, error) {
	params := map[string]string{
		"type": listType,
		"size": strconv.Itoa(size),
	}
	return c.getAlbumList(ctx, params)
}

func (c *Client) GetAllAlbums(ctx context.Context) ([]models.Album, error) {
	var all []models.Album
	const batchSize = 500

	for offset := 0; ; offset += batchSize {
		albums, err := c.getAlbumList(ctx, map[string]string{
			"type":   "alphabeticalByName",
			"size":   strconv.Itoa(batchSize),
			"offset": strconv.Itoa(offset),
		})
		if err != nil {
			return nil, err
		}
		all = append(all, albums...)
		if len(albums) < batchSize {
			return all, nil
		}
	}
}

func (c *Client) GetPlaylist(ctx context.Context, id string) (models.Playlist, error) {
	var wrapped response[playlistResponse]
	if err := c.get(ctx, "getPlaylist", map[string]string{"id": id}, &wrapped); err != nil {
		return models.Playlist{}, err
	}
	if err := checkStatus(wrapped.Data.baseResponse); err != nil {
		return models.Playlist{}, err
	}
	if wrapped.Data.Playlist == nil {
		return models.Playlist{}, fmt.Errorf("playlist %q not found", id)
	}
	return mapPlaylist(*wrapped.Data.Playlist, true), nil
}

func (c *Client) GetPlaylists(ctx context.Context) ([]models.Playlist, error) {
	var wrapped response[playlistsResponse]
	if err := c.get(ctx, "getPlaylists", nil, &wrapped); err != nil {
		return nil, err
	}
	if err := checkStatus(wrapped.Data.baseResponse); err != nil {
		return nil, err
	}

	var playlists []models.Playlist
	if wrapped.Data.Playlists == nil {
		return playlists, nil
	}
	for _, playlist := range wrapped.Data.Playlists.Playlist {
		playlists = append(playlists, mapPlaylist(playlist, false))
	}
	return playlists, nil
}

func (c *Client) GetRandomSongs(ctx context.Context) ([]models.Song, error) {
	var wrapped response[randomSongsResponse]
	if err := c.get(ctx, "getRandomSongs", map[string]string{"size": "50"}, &wrapped); err != nil {
		return nil, err
	}
	if err := checkStatus(wrapped.Data.baseResponse); err != nil {
		return nil, err
	}
	if wrapped.Data.RandomSongs == nil {
		return nil, nil
	}
	return mapSongs(wrapped.Data.RandomSongs.Song), nil
}

func (c *Client) GetSimilarSongs(ctx context.Context, id string) ([]models.Song, error) {
	var wrapped response[similarSongsResponse]
	if err := c.get(ctx, "getSimilarSongs2", map[string]string{"id": id, "count": "50"}, &wrapped); err != nil {
		return nil, err
	}
	if err := checkStatus(wrapped.Data.baseResponse); err != nil {
		return nil, err
	}
	if wrapped.Data.SimilarSongs == nil {
		return nil, nil
	}
	return mapSongs(wrapped.Data.SimilarSongs.Song), nil
}

func (c *Client) GetSong(ctx context.Context, id string) (models.Song, error) {
	var wrapped response[songResponse]
	if err := c.get(ctx, "getSong", map[string]string{"id": id}, &wrapped); err != nil {
		return models.Song{}, err
	}
	if err := checkStatus(wrapped.Data.baseResponse); err != nil {
		return models.Song{}, err
	}
	if wrapped.Data.Song == nil {
		return models.Song{}, fmt.Errorf("song %q not found", id)
	}
	return mapSong(*wrapped.Data.Song), nil
}

func (c *Client) Search(ctx context.Context, query string, count int) (models.SearchResult, error) {
	params := map[string]string{
		"query":       query,
		"artistCount": strconv.Itoa(count),
		"albumCount":  strconv.Itoa(count),
		"songCount":   strconv.Itoa(count),
	}

	var wrapped response[searchResponse]
	if err := c.get(ctx, "search3", params, &wrapped); err != nil {
		return models.SearchResult{}, err
	}
	if err := checkStatus(wrapped.Data.baseResponse); err != nil {
		return models.SearchResult{}, err
	}
	if wrapped.Data.SearchResult == nil {
		return models.SearchResult{}, nil
	}

	result := models.SearchResult{}
	for _, artist := range wrapped.Data.SearchResult.Artist {
		result.Artists = append(result.Artists, mapArtist(artist))
	}
	for _, album := range wrapped.Data.SearchResult.Album {
		result.Albums = append(result.Albums, mapAlbum(album))
	}
	for _, song := range wrapped.Data.SearchResult.Song {
		result.Songs = append(result.Songs, mapSong(song))
	}
	return result, nil
}

func (c *Client) SongURI(id string) string {
	return c.buildURL("stream", map[string]string{"id": id})
}

func (c *Client) SongURIAt(baseURL, id string) string {
	return c.buildURLAt(baseURL, "stream", map[string]string{"id": id})
}

func (c *Client) CoverArtURI(id string) string {
	return c.buildURL("getCoverArt", map[string]string{"id": id, "size": "300"})
}

func (c *Client) GetCoverArt(ctx context.Context, id string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.CoverArtURI(id), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("cover art returned HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func (c *Client) OpenStream(ctx context.Context, id string, rangeHeader string) (*http.Response, error) {
	active := c.activeBaseURL()
	resp, err := c.openStreamFromBase(ctx, active, id, rangeHeader)
	if err == nil {
		c.markEndpointResult(active, 0, nil)
		return resp, nil
	}
	c.markEndpointResult(active, 0, err)

	fallback := c.fallbackBaseURL(active)
	if fallback == "" || fallback == active {
		return nil, err
	}
	resp, retryErr := c.openStreamFromBase(ctx, fallback, id, rangeHeader)
	if retryErr != nil {
		c.markEndpointResult(fallback, 0, retryErr)
		return nil, err
	}
	c.markEndpointResult(fallback, 0, nil)
	return resp, nil
}

func (c *Client) Scrobble(ctx context.Context, id string) error {
	var wrapped response[baseResponse]
	err := c.get(ctx, "scrobble", map[string]string{"id": id, "submission": "true"}, &wrapped)
	if err != nil {
		return err
	}
	return checkStatus(wrapped.Data)
}

func (c *Client) getAlbumList(ctx context.Context, params map[string]string) ([]models.Album, error) {
	var wrapped response[albumListResponse]
	if err := c.get(ctx, "getAlbumList2", params, &wrapped); err != nil {
		return nil, err
	}
	if err := checkStatus(wrapped.Data.baseResponse); err != nil {
		return nil, err
	}
	if wrapped.Data.AlbumList == nil {
		return nil, nil
	}

	albums := make([]models.Album, 0, len(wrapped.Data.AlbumList.Album))
	for _, album := range wrapped.Data.AlbumList.Album {
		albums = append(albums, mapAlbum(album))
	}
	return albums, nil
}

func (c *Client) get(ctx context.Context, endpoint string, params map[string]string, out any) error {
	active := c.activeBaseURL()
	err := c.getFromBase(ctx, active, endpoint, params, out)
	if err == nil {
		c.markEndpointResult(active, 0, nil)
		return nil
	}
	c.markEndpointResult(active, 0, err)

	fallback := c.fallbackBaseURL(active)
	if fallback == "" || fallback == active {
		return err
	}
	if retryErr := c.getFromBase(ctx, fallback, endpoint, params, out); retryErr != nil {
		c.markEndpointResult(fallback, 0, retryErr)
		return err
	}
	c.markEndpointResult(fallback, 0, nil)
	return nil
}

func (c *Client) buildURL(endpoint string, params map[string]string) string {
	return c.buildURLAt(c.activeBaseURL(), endpoint, params)
}

func (c *Client) buildURLAt(baseURL, endpoint string, params map[string]string) string {
	c.mu.RLock()
	account := c.account
	c.mu.RUnlock()

	values := url.Values{}
	values.Set("u", account.Username)
	values.Set("c", clientName)
	values.Set("v", apiVersion)
	values.Set("f", "json")
	if account.UsePlaintext {
		values.Set("p", account.Password)
	} else {
		values.Set("t", account.Token)
		values.Set("s", account.Salt)
	}
	for key, value := range params {
		values.Set(key, value)
	}
	return strings.TrimRight(baseURL, "/") + "/rest/" + endpoint + "?" + values.Encode()
}

func (c *Client) getFromBase(ctx context.Context, baseURL, endpoint string, params map[string]string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.buildURLAt(baseURL, endpoint, params), nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s returned HTTP %d", endpoint, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) openStreamFromBase(ctx context.Context, baseURL, id string, rangeHeader string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.buildURLAt(baseURL, "stream", map[string]string{"id": id}), nil)
	if err != nil {
		return nil, err
	}
	if rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, fmt.Errorf("stream returned HTTP %d", resp.StatusCode)
	}
	return resp, nil
}

func (c *Client) checkAllEndpoints(ctx context.Context) {
	c.mu.RLock()
	snapshot := make([]endpointRuntime, len(c.endpoints))
	copy(snapshot, c.endpoints)
	timeout := time.Duration(c.settings.EndpointTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	c.mu.RUnlock()

	for _, endpoint := range snapshot {
		if !endpoint.Enabled {
			continue
		}
		checkCtx, cancel := context.WithTimeout(ctx, timeout)
		start := time.Now()
		err := c.pingEndpoint(checkCtx, endpoint.URL)
		latency := time.Since(start)
		cancel()
		c.markEndpointResult(endpoint.URL, latency, err)
	}
}

func (c *Client) pingEndpoint(ctx context.Context, baseURL string) error {
	_, err := c.pingEndpointInfo(ctx, baseURL)
	return err
}

func (c *Client) pingEndpointInfo(ctx context.Context, baseURL string) (baseResponse, error) {
	var wrapped response[baseResponse]
	err := c.getFromBase(ctx, baseURL, "ping.view", nil, &wrapped)
	if err != nil {
		return baseResponse{}, err
	}
	if err := checkStatus(wrapped.Data); err != nil {
		return wrapped.Data, err
	}
	return wrapped.Data, nil
}

func (c *Client) endpointIdentity(ctx context.Context, endpoint models.Endpoint) (EndpointIdentity, error) {
	ping, err := c.pingEndpointInfo(ctx, endpoint.URL)
	if err != nil {
		return EndpointIdentity{ServerVersion: ping.Version, ServerType: ping.Type}, err
	}

	var wrapped response[artistsResponse]
	if err := c.getFromBase(ctx, endpoint.URL, "getArtists", nil, &wrapped); err != nil {
		return EndpointIdentity{ServerVersion: ping.Version, ServerType: ping.Type}, err
	}
	if err := checkStatus(wrapped.Data.baseResponse); err != nil {
		return EndpointIdentity{ServerVersion: ping.Version, ServerType: ping.Type}, err
	}

	fingerprint, count := artistsFingerprint(wrapped.Data)
	return EndpointIdentity{
		Endpoint:      endpoint,
		Fingerprint:   fingerprint,
		ArtistCount:   count,
		ServerVersion: ping.Version,
		ServerType:    ping.Type,
	}, nil
}

func artistsFingerprint(data artistsResponse) (string, int) {
	var entries []string
	if data.Artists != nil {
		for _, index := range data.Artists.Index {
			for _, artist := range index.Artist {
				entries = append(entries, artist.ID+"\x00"+artist.Name+"\x00"+strconv.Itoa(artist.AlbumCount))
			}
		}
	}
	sort.Strings(entries)
	hash := sha256.New()
	for _, entry := range entries {
		hash.Write([]byte(entry))
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil)), len(entries)
}

func (c *Client) markEndpointResult(baseURL string, latency time.Duration, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	index := c.endpointIndexLocked(baseURL)
	if index < 0 {
		return
	}
	endpoint := &c.endpoints[index]
	if err != nil {
		endpoint.Failures++
		endpoint.Successes = 0
		if index == c.active && endpoint.Failures >= 1 {
			c.active = c.bestEndpointLocked(index)
		}
		return
	}

	endpoint.Failures = 0
	endpoint.Successes++
	endpoint.LastOK = time.Now()
	if latency > 0 {
		endpoint.Latency = latency
		if endpoint.EWMA == 0 {
			endpoint.EWMA = latency
		} else {
			endpoint.EWMA = time.Duration(float64(endpoint.EWMA)*0.7 + float64(latency)*0.3)
		}
	}
	c.maybeSwitchLocked(index)
}

func (c *Client) maybeSwitchLocked(candidate int) {
	if candidate == c.active || candidate < 0 || candidate >= len(c.endpoints) {
		return
	}
	if c.endpoints[candidate].Successes < 2 || c.endpoints[candidate].EWMA <= 0 {
		return
	}
	if c.active < 0 || c.active >= len(c.endpoints) || c.endpoints[c.active].Failures > 0 {
		c.active = candidate
		return
	}
	active := c.endpoints[c.active]
	if active.EWMA <= 0 {
		return
	}
	threshold := c.settings.EndpointSwitchThreshold
	if threshold <= 0 {
		threshold = 0.30
	}
	if float64(c.endpoints[candidate].EWMA) < float64(active.EWMA)*(1-threshold) {
		c.active = candidate
	}
}

func (c *Client) activeBaseURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.active >= 0 && c.active < len(c.endpoints) && c.endpoints[c.active].Enabled {
		return c.endpoints[c.active].URL
	}
	if index := c.firstEnabledLocked(); index >= 0 {
		return c.endpoints[index].URL
	}
	return ""
}

func (c *Client) fallbackBaseURL(current string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	currentIndex := c.endpointIndexLocked(current)
	next := c.bestEndpointLocked(currentIndex)
	if next >= 0 {
		c.active = next
		return c.endpoints[next].URL
	}
	return ""
}

func (c *Client) firstEnabledLocked() int {
	for i, endpoint := range c.endpoints {
		if endpoint.Enabled && endpoint.URL != "" {
			return i
		}
	}
	return -1
}

func (c *Client) bestEndpointLocked(exclude int) int {
	best := -1
	for i, endpoint := range c.endpoints {
		if i == exclude || !endpoint.Enabled || endpoint.URL == "" {
			continue
		}
		if best == -1 {
			best = i
			continue
		}
		if endpoint.Failures < c.endpoints[best].Failures {
			best = i
			continue
		}
		if endpoint.Failures == c.endpoints[best].Failures && endpoint.EWMA > 0 && (c.endpoints[best].EWMA == 0 || endpoint.EWMA < c.endpoints[best].EWMA) {
			best = i
		}
	}
	if best >= 0 {
		return best
	}
	return c.firstEnabledLocked()
}

func (c *Client) endpointIndexLocked(baseURL string) int {
	baseURL = strings.TrimRight(baseURL, "/")
	for i, endpoint := range c.endpoints {
		if strings.EqualFold(endpoint.URL, baseURL) {
			return i
		}
	}
	return -1
}

func (c *Client) healthIntervalSeconds() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.settings.HealthCheckIntervalSeconds
}

func checkStatus(base baseResponse) error {
	if base.Status == "" || base.Status == "ok" {
		return nil
	}
	if base.Error != nil {
		return fmt.Errorf("subsonic API error %d: %s", base.Error.Code, base.Error.Message)
	}
	return fmt.Errorf("subsonic API status: %s", base.Status)
}

func mapArtist(a artistJSON) models.Artist {
	return models.Artist{ID: a.ID, Name: a.Name, AlbumCount: a.AlbumCount}
}

func mapAlbum(a albumJSON) models.Album {
	return models.Album{
		ID:        a.ID,
		Name:      a.Name,
		Artist:    a.Artist,
		ArtistID:  a.ArtistID,
		SongCount: a.SongCount,
		Duration:  a.Duration,
		Year:      a.Year,
		CoverArt:  a.CoverArt,
	}
}

func mapPlaylist(p playlistJSON, includeEntries bool) models.Playlist {
	playlist := models.Playlist{
		ID:        p.ID,
		Name:      p.Name,
		Comment:   p.Comment,
		Owner:     p.Owner,
		Public:    p.Public,
		SongCount: p.SongCount,
		Duration:  p.Duration,
		CoverArt:  p.CoverArt,
		Created:   p.Created,
	}
	if includeEntries {
		playlist.Entries = mapSongs(p.Entry)
	}
	return playlist
}

func mapSongs(songs []songJSON) []models.Song {
	ret := make([]models.Song, 0, len(songs))
	for _, song := range songs {
		ret = append(ret, mapSong(song))
	}
	return ret
}

func mapSong(s songJSON) models.Song {
	return models.Song{
		ID:                    s.ID,
		Parent:                s.Parent,
		Track:                 s.Track,
		Title:                 s.Title,
		Artist:                s.Artist,
		Album:                 s.Album,
		AlbumID:               s.AlbumID,
		Duration:              s.Duration,
		CoverArt:              s.CoverArt,
		Suffix:                s.Suffix,
		ContentType:           s.ContentType,
		TranscodedSuffix:      s.TranscodedSuffix,
		TranscodedContentType: s.TranscodedContentType,
		BitRateKbps:           s.BitRateKbps,
		BitDepth:              s.BitDepth,
		SamplingRate:          s.SamplingRate,
		ChannelCount:          s.ChannelCount,
	}
}

func generateSalt() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "saki"
	}
	return hex.EncodeToString(bytes[:])
}

func generateToken(password, salt string) string {
	sum := md5.Sum([]byte(password + salt))
	return hex.EncodeToString(sum[:])
}
