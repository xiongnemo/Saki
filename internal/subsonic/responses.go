package subsonic

type response[T any] struct {
	Data T `json:"subsonic-response"`
}

type baseResponse struct {
	Status  string         `json:"status"`
	Version string         `json:"version"`
	Type    string         `json:"type"`
	Error   *subsonicError `json:"error"`
}

type subsonicError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type artistsResponse struct {
	baseResponse
	Artists *struct {
		Index []struct {
			Name   string       `json:"name"`
			Artist []artistJSON `json:"artist"`
		} `json:"index"`
	} `json:"artists"`
}

type artistResponse struct {
	baseResponse
	Artist *artistDetailJSON `json:"artist"`
}

type albumResponse struct {
	baseResponse
	Album *albumDetailJSON `json:"album"`
}

type playlistResponse struct {
	baseResponse
	Playlist *playlistJSON `json:"playlist"`
}

type playlistsResponse struct {
	baseResponse
	Playlists *struct {
		Playlist []playlistJSON `json:"playlist"`
	} `json:"playlists"`
}

type songResponse struct {
	baseResponse
	Song *songJSON `json:"song"`
}

type similarSongsResponse struct {
	baseResponse
	SimilarSongs *struct {
		Song []songJSON `json:"song"`
	} `json:"similarSongs2"`
}

type randomSongsResponse struct {
	baseResponse
	RandomSongs *struct {
		Song []songJSON `json:"song"`
	} `json:"randomSongs"`
}

type albumListResponse struct {
	baseResponse
	AlbumList *struct {
		Album []albumJSON `json:"album"`
	} `json:"albumList2"`
}

type searchResponse struct {
	baseResponse
	SearchResult *struct {
		Artist []artistJSON `json:"artist"`
		Album  []albumJSON  `json:"album"`
		Song   []songJSON   `json:"song"`
	} `json:"searchResult3"`
}

type artistJSON struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	AlbumCount int    `json:"albumCount"`
}

type artistDetailJSON struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	AlbumCount int         `json:"albumCount"`
	Album      []albumJSON `json:"album"`
}

type albumJSON struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Artist    string `json:"artist"`
	ArtistID  string `json:"artistId"`
	CoverArt  string `json:"coverArt"`
	SongCount int    `json:"songCount"`
	Duration  int    `json:"duration"`
	Year      int    `json:"year"`
}

type albumDetailJSON struct {
	albumJSON
	Song []songJSON `json:"song"`
}

type playlistJSON struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Comment   string     `json:"comment"`
	Owner     string     `json:"owner"`
	Public    bool       `json:"public"`
	SongCount int        `json:"songCount"`
	Duration  int        `json:"duration"`
	CoverArt  string     `json:"coverArt"`
	Created   string     `json:"created"`
	Entry     []songJSON `json:"entry"`
}

type songJSON struct {
	ID       string `json:"id"`
	Parent   string `json:"parent"`
	Title    string `json:"title"`
	Album    string `json:"album"`
	Artist   string `json:"artist"`
	Track    int    `json:"track"`
	CoverArt string `json:"coverArt"`
	Duration int    `json:"duration"`
	AlbumID  string `json:"albumId"`
}
