package models

import "fmt"

type Song struct {
	ID       string `json:"id"`
	Parent   string `json:"parent"`
	Track    int    `json:"track"`
	Title    string `json:"title"`
	Artist   string `json:"artist"`
	Album    string `json:"album"`
	AlbumID  string `json:"albumId"`
	Duration int    `json:"duration"`
	CoverArt string `json:"coverArt"`
	Image    string `json:"-"`
}

func (s Song) String() string {
	return s.Title
}

type Album struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Artist    string `json:"artist"`
	ArtistID  string `json:"artistId"`
	SongCount int    `json:"songCount"`
	Duration  int    `json:"duration"`
	Year      int    `json:"year"`
	CoverArt  string `json:"coverArt"`
	Songs     []Song `json:"song"`
}

func (a Album) String() string {
	return a.Name
}

type Artist struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	AlbumCount int     `json:"albumCount"`
	Albums     []Album `json:"album"`
}

func (a Artist) String() string {
	return a.Name
}

type Playlist struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Comment   string `json:"comment"`
	Owner     string `json:"owner"`
	Public    bool   `json:"public"`
	SongCount int    `json:"songCount"`
	Duration  int    `json:"duration"`
	CoverArt  string `json:"coverArt"`
	Image     string `json:"image"`
	Created   string `json:"created"`
	Entries   []Song `json:"entry"`
}

func (p Playlist) String() string {
	return p.Name
}

type Account struct {
	Username     string     `json:"username"`
	Password     string     `json:"password"`
	URL          string     `json:"url,omitempty"`
	Endpoints    []Endpoint `json:"endpoints"`
	Salt         string     `json:"salt"`
	Token        string     `json:"token"`
	UsePlaintext bool       `json:"usePlaintext"`
}

type Endpoint struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Enabled bool   `json:"enabled"`
}

type Settings struct {
	AudioBackend               string  `json:"audioBackend"`
	MPVPath                    string  `json:"mpvPath"`
	CacheDir                   string  `json:"cacheDir"`
	AudioCacheMaxBytes         int64   `json:"audioCacheMaxBytes"`
	HealthCheckIntervalSeconds int     `json:"healthCheckIntervalSeconds"`
	EndpointTimeoutSeconds     int     `json:"endpointTimeoutSeconds"`
	EndpointSwitchThreshold    float64 `json:"endpointSwitchThreshold"`
	EnablePrefetch             bool    `json:"enablePrefetch"`
	UseBundledMPV              bool    `json:"useBundledMpv"`
}

type Config struct {
	Account  Account  `json:"account"`
	Settings Settings `json:"settings"`
}

type SearchResult struct {
	Artists []Artist `json:"artists"`
	Albums  []Album  `json:"albums"`
	Songs   []Song   `json:"songs"`
}

type RepeatStatus int

const (
	RepeatNone RepeatStatus = iota
	RepeatOne
	RepeatAll
)

func (r RepeatStatus) Next() RepeatStatus {
	switch r {
	case RepeatNone:
		return RepeatOne
	case RepeatOne:
		return RepeatAll
	default:
		return RepeatNone
	}
}

func (r RepeatStatus) String() string {
	switch r {
	case RepeatOne:
		return "One"
	case RepeatAll:
		return "All"
	default:
		return "Off"
	}
}

type PlaybackState int

const (
	PlaybackStopped PlaybackState = iota
	PlaybackPlaying
	PlaybackPaused
)

func (s PlaybackState) String() string {
	switch s {
	case PlaybackPlaying:
		return "Playing"
	case PlaybackPaused:
		return "Paused"
	default:
		return "Stopped"
	}
}

type CurrentState struct {
	CurrentTrack       *Song
	Position           float64
	Playing            bool
	Stopped            bool
	Buffering          bool
	BufferedSeconds    float64
	BufferedBytes      int64
	TotalBytes         int64
	BufferedPercent    float64
	BufferPercentKnown bool
	LastError          string
	CurrentPlaylist    Playlist
	CurrentTrackIndex  int
	RepeatStatus       RepeatStatus
	Shuffled           bool
	Volume             float64
}

func SecondsAsMMSS(seconds int) string {
	if seconds < 0 {
		seconds = 0
	}
	return fmt.Sprintf("%02d:%02d", seconds/60, seconds%60)
}
