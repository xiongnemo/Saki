using System.Text.Json.Serialization;

namespace Saki.Core.Services.Subsonic;

// Subsonic API response wrapper
public class SubsonicResponse<T>
{
    [JsonPropertyName("subsonic-response")]
    public T SubsonicResponseData { get; set; } = default!;
}

// Base Subsonic response
public class BaseSubsonicResponse
{
    [JsonPropertyName("status")]
    public string Status { get; set; } = "";
    
    [JsonPropertyName("version")]
    public string Version { get; set; } = "";
    
    [JsonPropertyName("type")]
    public string Type { get; set; } = "";
    
    [JsonPropertyName("error")]
    public SubsonicError? Error { get; set; }
}

// Error response
public class SubsonicError
{
    [JsonPropertyName("code")]
    public int Code { get; set; }
    
    [JsonPropertyName("message")]
    public string Message { get; set; } = "";
}

// Artists response
public class ArtistsResponseData : BaseSubsonicResponse
{
    [JsonPropertyName("artists")]
    public InnerArtistsResponse? Artists { get; set; }
}

public class InnerArtistsResponse
{
    [JsonPropertyName("ignoredArticles")]
    public string IgnoredArticles { get; set; } = "";
    
    [JsonPropertyName("index")]
    public List<ArtistIndex> Index { get; set; } = new();
}

public class ArtistIndex
{
    [JsonPropertyName("name")]
    public string Name { get; set; } = "";
    
    [JsonPropertyName("artist")]
    public List<ArtistJson> Artist { get; set; } = new();
}

// JSON representation of Artist from API
public class ArtistJson
{
    [JsonPropertyName("id")]
    public string Id { get; set; } = "";
    
    [JsonPropertyName("name")]
    public string Name { get; set; } = "";
    
    [JsonPropertyName("albumCount")]
    public int AlbumCount { get; set; }
    
    [JsonPropertyName("starred")]
    public DateTime? Starred { get; set; }
}

// Artist response (single artist)
public class ArtistResponseData : BaseSubsonicResponse
{
    [JsonPropertyName("artist")]
    public ArtistDetailJson? Artist { get; set; }
}

public class ArtistDetailJson
{
    [JsonPropertyName("id")]
    public string Id { get; set; } = "";
    
    [JsonPropertyName("name")]
    public string Name { get; set; } = "";
    
    [JsonPropertyName("albumCount")]
    public int AlbumCount { get; set; }
    
    [JsonPropertyName("starred")]
    public DateTime? Starred { get; set; }
    
    [JsonPropertyName("album")]
    public List<AlbumJson> Album { get; set; } = new();
}

// JSON representation of Album from API
public class AlbumJson
{
    [JsonPropertyName("id")]
    public string Id { get; set; } = "";
    
    [JsonPropertyName("name")]
    public string Name { get; set; } = "";
    
    [JsonPropertyName("artist")]
    public string Artist { get; set; } = "";
    
    [JsonPropertyName("artistId")]
    public string ArtistId { get; set; } = "";
    
    [JsonPropertyName("coverArt")]
    public string CoverArt { get; set; } = "";
    
    [JsonPropertyName("songCount")]
    public int SongCount { get; set; }
    
    [JsonPropertyName("duration")]
    public int Duration { get; set; }
    
    [JsonPropertyName("playCount")]
    public int PlayCount { get; set; }
    
    [JsonPropertyName("created")]
    public DateTime Created { get; set; }
    
    [JsonPropertyName("starred")]
    public DateTime? Starred { get; set; }
    
    [JsonPropertyName("year")]
    public int Year { get; set; }
    
    [JsonPropertyName("genre")]
    public string Genre { get; set; } = "";
}

// Album response (single album with songs)
public class AlbumResponseData : BaseSubsonicResponse
{
    [JsonPropertyName("album")]
    public AlbumDetailJson? Album { get; set; }
}

public class AlbumDetailJson : AlbumJson
{
    [JsonPropertyName("song")]
    public List<SongJson> Song { get; set; } = new();
}

// Playlist response
public class PlaylistResponseData : BaseSubsonicResponse
{
    [JsonPropertyName("playlist")]
    public PlaylistJson? Playlist { get; set; }
}

public class PlaylistJson
{
    [JsonPropertyName("id")]
    public string Id { get; set; } = "";

    [JsonPropertyName("name")]
    public string Name { get; set; } = "";

    [JsonPropertyName("comment")]
    public string Comment { get; set; } = "";

    [JsonPropertyName("owner")]
    public string Owner { get; set; } = "";

    [JsonPropertyName("public")]
    public bool Public { get; set; }

    [JsonPropertyName("songCount")]
    public int SongCount { get; set; }

    [JsonPropertyName("duration")]
    public int Duration { get; set; }

    [JsonPropertyName("coverArt")]
    public string CoverArt { get; set; } = "";

    [JsonPropertyName("created")]
    public string Created { get; set; } = "";

    [JsonPropertyName("entry")]
    public List<SongJson> Entry { get; set; } = new();
}

// Playlists response
public class PlaylistsResponseData : BaseSubsonicResponse
{
    [JsonPropertyName("playlists")]
    public InnerPlaylistsResponse? Playlists { get; set; }
}

public class InnerPlaylistsResponse
{
    [JsonPropertyName("playlist")]
    public List<PlaylistJson> Playlist { get; set; } = new();
}

// Song response
public class SongResponseData : BaseSubsonicResponse
{
    [JsonPropertyName("song")]
    public SongJson? Song { get; set; }
}

// Similar songs response
public class SimilarSongsResponseData : BaseSubsonicResponse
{
    [JsonPropertyName("similarSongs2")]
    public InnerSimilarSongsResponse? SimilarSongs { get; set; }
}

public class InnerSimilarSongsResponse
{
    [JsonPropertyName("song")]
    public List<SongJson> Song { get; set; } = new();
}

// Random songs response
public class RandomSongsResponseData : BaseSubsonicResponse
{
    [JsonPropertyName("randomSongs")]
    public InnerRandomSongsResponse? RandomSongs { get; set; }
}

public class InnerRandomSongsResponse
{
    [JsonPropertyName("song")]
    public List<SongJson> Song { get; set; } = new();
}

// Album list response
public class AlbumListResponseData : BaseSubsonicResponse
{
    [JsonPropertyName("albumList2")]
    public InnerAlbumListResponse? AlbumList { get; set; }
}

public class InnerAlbumListResponse
{
    [JsonPropertyName("album")]
    public List<AlbumJson> Album { get; set; } = new();
}

// Search response
public class SearchResponseData : BaseSubsonicResponse
{
    [JsonPropertyName("searchResult3")]
    public InnerSearchResponse? SearchResult { get; set; }
}

public class InnerSearchResponse
{
    [JsonPropertyName("artist")]
    public List<ArtistJson> Artist { get; set; } = new();

    [JsonPropertyName("album")]
    public List<AlbumJson> Album { get; set; } = new();

    [JsonPropertyName("song")]
    public List<SongJson> Song { get; set; } = new();
}

// JSON representation of Song from API
public class SongJson
{
    [JsonPropertyName("id")]
    public string Id { get; set; } = "";
    
    [JsonPropertyName("parent")]
    public string Parent { get; set; } = "";
    
    [JsonPropertyName("title")]
    public string Title { get; set; } = "";
    
    [JsonPropertyName("album")]
    public string Album { get; set; } = "";
    
    [JsonPropertyName("artist")]
    public string Artist { get; set; } = "";
    
    [JsonPropertyName("track")]
    public int Track { get; set; }
    
    [JsonPropertyName("year")]
    public int Year { get; set; }
    
    [JsonPropertyName("genre")]
    public string Genre { get; set; } = "";
    
    [JsonPropertyName("coverArt")]
    public string CoverArt { get; set; } = "";
    
    [JsonPropertyName("size")]
    public long Size { get; set; }
    
    [JsonPropertyName("contentType")]
    public string ContentType { get; set; } = "";
    
    [JsonPropertyName("suffix")]
    public string Suffix { get; set; } = "";
    
    [JsonPropertyName("duration")]
    public int Duration { get; set; }
    
    [JsonPropertyName("bitRate")]
    public int BitRate { get; set; }
    
    [JsonPropertyName("path")]
    public string Path { get; set; } = "";
    
    [JsonPropertyName("playCount")]
    public int PlayCount { get; set; }
    
    [JsonPropertyName("discNumber")]
    public int DiscNumber { get; set; }
    
    [JsonPropertyName("created")]
    public DateTime Created { get; set; }
    
    [JsonPropertyName("albumId")]
    public string AlbumId { get; set; } = "";
    
    [JsonPropertyName("artistId")]
    public string ArtistId { get; set; } = "";
    
    [JsonPropertyName("type")]
    public string Type { get; set; } = "";
    
    [JsonPropertyName("starred")]
    public DateTime? Starred { get; set; }
}