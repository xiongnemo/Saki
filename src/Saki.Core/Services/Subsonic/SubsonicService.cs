using Saki.Core.Models;
using Saki.Core.Services;
using System.Security.Cryptography;
using System.Text;
using System.Text.Json;

namespace Saki.Core.Services.Subsonic;

public class SubsonicService : ISubsonicService
{
    private readonly HttpClient _httpClient;
    private Account _account = new();
    private const string ClientName = "Saki";
    private const string ApiVersion = "1.16.1";

    public SubsonicService(HttpClient? httpClient = null)
    {
        _httpClient = httpClient ?? new HttpClient();
    }

    public void Configure(Account account)
    {
        _account = account;
        
        // Generate salt and token for authentication
        if (string.IsNullOrEmpty(_account.Salt))
        {
            _account.Salt = GenerateSalt();
        }
        
        if (string.IsNullOrEmpty(_account.Token))
        {
            _account.Token = GenerateToken(_account.Password, _account.Salt);
        }
    }

    public Account GetActiveAccount() => _account;

    public async Task<Album> GetAlbum(string id)
    {
        var parameters = GetBasicParams();
        parameters["id"] = id;
        
        var response = await MakeRequestAsync("getAlbum", parameters);
        
        try
        {
            var albumResponse = JsonSerializer.Deserialize<SubsonicResponse<AlbumResponseData>>(response);
            
            if (albumResponse?.SubsonicResponseData?.Status != "ok")
            {
                var error = albumResponse?.SubsonicResponseData?.Error?.Message ?? "Unknown error";
                throw new InvalidOperationException($"Subsonic API error: {error}");
            }
            
            var albumJson = albumResponse.SubsonicResponseData.Album;
            if (albumJson == null)
            {
                throw new InvalidOperationException($"Album with ID {id} not found");
            }
            
            var songs = albumJson.Song.Select(s => new Song(
                s.Id, s.Parent, s.Track, s.Title, s.Artist, s.Album, s.AlbumId, s.Duration, s.CoverArt
            )).ToList();
            
            return new Album
            {
                Id = albumJson.Id,
                Name = albumJson.Name,
                Artist = albumJson.Artist,
                ArtistId = albumJson.ArtistId,
                Year = albumJson.Year,
                CoverArt = albumJson.CoverArt,
                SongCount = albumJson.SongCount,
                Duration = albumJson.Duration,
                Song = songs
            };
        }
        catch (JsonException ex)
        {
            throw new InvalidOperationException($"Failed to parse album response: {ex.Message}", ex);
        }
    }

    public async Task<List<Album>> GetAlbums(string type = "frequent", int size = 10)
    {
        var parameters = GetBasicParams();
        parameters["type"] = type;
        parameters["size"] = size.ToString();
        
        var response = await MakeRequestAsync("getAlbumList2", parameters);
        
        try
        {
            var albumListResponse = JsonSerializer.Deserialize<SubsonicResponse<AlbumListResponseData>>(response);
            
            if (albumListResponse?.SubsonicResponseData?.Status != "ok")
            {
                var error = albumListResponse?.SubsonicResponseData?.Error?.Message ?? "Unknown error";
                throw new InvalidOperationException($"Subsonic API error: {error}");
            }
            
            if (albumListResponse.SubsonicResponseData.AlbumList?.Album == null)
                return new List<Album>();
            
            return albumListResponse.SubsonicResponseData.AlbumList.Album.Select(a => new Album
            {
                Id = a.Id,
                Name = a.Name,
                Artist = a.Artist,
                ArtistId = a.ArtistId,
                Year = a.Year,
                CoverArt = a.CoverArt,
                SongCount = a.SongCount,
                Duration = a.Duration
            }).ToList();
        }
        catch (JsonException ex)
        {
            throw new InvalidOperationException($"Failed to parse album list response: {ex.Message}", ex);
        }
    }

    public async Task<List<Album>> GetAllAlbums()
    {
        var allAlbums = new List<Album>();
        int offset = 0;
        const int batchSize = 500;
        
        while (true)
        {
            var parameters = GetBasicParams();
            parameters["type"] = "alphabeticalByName";
            parameters["size"] = batchSize.ToString();
            parameters["offset"] = offset.ToString();
            
            var response = await MakeRequestAsync("getAlbumList2", parameters);
            
            try
            {
                var albumListResponse = JsonSerializer.Deserialize<SubsonicResponse<AlbumListResponseData>>(response);
                
                if (albumListResponse?.SubsonicResponseData?.Status != "ok")
                    break;
                
                var albums = albumListResponse.SubsonicResponseData.AlbumList?.Album;
                if (albums == null || albums.Count == 0)
                    break;
                
                allAlbums.AddRange(albums.Select(a => new Album
                {
                    Id = a.Id,
                    Name = a.Name,
                    Artist = a.Artist,
                    ArtistId = a.ArtistId,
                    Year = a.Year,
                    CoverArt = a.CoverArt,
                    SongCount = a.SongCount,
                    Duration = a.Duration
                }));
                
                if (albums.Count < batchSize)
                    break;
                
                offset += batchSize;
            }
            catch (JsonException)
            {
                break;
            }
        }
        
        return allAlbums;
    }

    public async Task<Artist> GetArtist(string id)
    {
        var parameters = GetBasicParams();
        parameters["id"] = id;
        
        var response = await MakeRequestAsync("getArtist", parameters);
        
        try
        {
            var artistResponse = JsonSerializer.Deserialize<SubsonicResponse<ArtistResponseData>>(response);
            
            if (artistResponse?.SubsonicResponseData?.Status != "ok")
            {
                var error = artistResponse?.SubsonicResponseData?.Error?.Message ?? "Unknown error";
                throw new InvalidOperationException($"Subsonic API error: {error}");
            }
            
            var artistJson = artistResponse.SubsonicResponseData.Artist;
            if (artistJson == null)
            {
                throw new InvalidOperationException($"Artist with ID {id} not found");
            }
            
            var albums = artistJson.Album.Select(a => new Album
            {
                Id = a.Id,
                Name = a.Name,
                Artist = a.Artist,
                ArtistId = a.ArtistId,
                Year = a.Year,
                CoverArt = a.CoverArt,
                SongCount = a.SongCount,
                Duration = a.Duration
            }).ToList();
            
            return new Artist
            {
                Id = artistJson.Id,
                Name = artistJson.Name,
                AlbumCount = artistJson.AlbumCount,
                Album = albums
            };
        }
        catch (JsonException ex)
        {
            throw new InvalidOperationException($"Failed to parse artist response: {ex.Message}", ex);
        }
    }

    public async Task<List<Artist>> GetArtists()
    {
        var parameters = GetBasicParams();
        
        var response = await MakeRequestAsync("getArtists", parameters);
        
        try
        {
            var artistsResponse = JsonSerializer.Deserialize<SubsonicResponse<ArtistsResponseData>>(response);
            
            if (artistsResponse?.SubsonicResponseData?.Status != "ok")
            {
                var error = artistsResponse?.SubsonicResponseData?.Error?.Message ?? "Unknown error";
                throw new InvalidOperationException($"Subsonic API error: {error}");
            }
            
            var artists = new List<Artist>();
            
            if (artistsResponse.SubsonicResponseData.Artists?.Index != null)
            {
                foreach (var index in artistsResponse.SubsonicResponseData.Artists.Index)
                {
                    foreach (var artistJson in index.Artist)
                    {
                        artists.Add(new Artist
                        {
                            Id = artistJson.Id,
                            Name = artistJson.Name,
                            AlbumCount = artistJson.AlbumCount
                        });
                    }
                }
            }
            
            return artists;
        }
        catch (JsonException ex)
        {
            throw new InvalidOperationException($"Failed to parse artists response: {ex.Message}", ex);
        }
    }

    public async Task<Playlist> GetPlaylist(string id)
    {
        var parameters = GetBasicParams();
        parameters["id"] = id;
        
        var response = await MakeRequestAsync("getPlaylist", parameters);
        
        try
        {
            var playlistResponse = JsonSerializer.Deserialize<SubsonicResponse<PlaylistResponseData>>(response);
            
            if (playlistResponse?.SubsonicResponseData?.Status != "ok")
            {
                var error = playlistResponse?.SubsonicResponseData?.Error?.Message ?? "Unknown error";
                throw new InvalidOperationException($"Subsonic API error: {error}");
            }
            
            var pl = playlistResponse.SubsonicResponseData.Playlist;
            if (pl == null)
                throw new InvalidOperationException($"Playlist with ID {id} not found");
            
            var songs = pl.Entry.Select(s => new Song(
                s.Id, s.Parent, s.Track, s.Title, s.Artist, s.Album, s.AlbumId, s.Duration, s.CoverArt
            )).ToList();
            
            return new Playlist(pl.Id, pl.Name, pl.Comment, pl.Owner, pl.Public,
                pl.SongCount, pl.Duration, pl.CoverArt, "", songs)
            {
                Created = pl.Created
            };
        }
        catch (JsonException ex)
        {
            throw new InvalidOperationException($"Failed to parse playlist response: {ex.Message}", ex);
        }
    }

    public async Task<List<Playlist>> GetPlaylists()
    {
        var parameters = GetBasicParams();
        
        var response = await MakeRequestAsync("getPlaylists", parameters);
        
        try
        {
            var playlistsResponse = JsonSerializer.Deserialize<SubsonicResponse<PlaylistsResponseData>>(response);
            
            if (playlistsResponse?.SubsonicResponseData?.Status != "ok")
            {
                var error = playlistsResponse?.SubsonicResponseData?.Error?.Message ?? "Unknown error";
                throw new InvalidOperationException($"Subsonic API error: {error}");
            }
            
            if (playlistsResponse.SubsonicResponseData.Playlists?.Playlist == null)
                return new List<Playlist>();
            
            return playlistsResponse.SubsonicResponseData.Playlists.Playlist.Select(pl => new Playlist
            {
                Id = pl.Id,
                Name = pl.Name,
                Comment = pl.Comment,
                Owner = pl.Owner,
                Public = pl.Public,
                SongCount = pl.SongCount,
                Duration = pl.Duration,
                CoverArt = pl.CoverArt,
                Created = pl.Created
            }).ToList();
        }
        catch (JsonException ex)
        {
            throw new InvalidOperationException($"Failed to parse playlists response: {ex.Message}", ex);
        }
    }

    public async Task<List<Song>> GetRandomSongs()
    {
        var parameters = GetBasicParams();
        parameters["size"] = "50";
        
        var response = await MakeRequestAsync("getRandomSongs", parameters);
        
        try
        {
            var randomResponse = JsonSerializer.Deserialize<SubsonicResponse<RandomSongsResponseData>>(response);
            
            if (randomResponse?.SubsonicResponseData?.Status != "ok")
                return new List<Song>();
            
            if (randomResponse.SubsonicResponseData.RandomSongs?.Song == null)
                return new List<Song>();
            
            return randomResponse.SubsonicResponseData.RandomSongs.Song.Select(s => new Song(
                s.Id, s.Parent, s.Track, s.Title, s.Artist, s.Album, s.AlbumId, s.Duration, s.CoverArt
            )).ToList();
        }
        catch (JsonException)
        {
            return new List<Song>();
        }
    }

    public async Task<List<Song>> GetSimilarSongs(string id)
    {
        var parameters = GetBasicParams();
        parameters["id"] = id;
        parameters["count"] = "50";
        
        var response = await MakeRequestAsync("getSimilarSongs2", parameters);
        
        try
        {
            var similarResponse = JsonSerializer.Deserialize<SubsonicResponse<SimilarSongsResponseData>>(response);
            
            if (similarResponse?.SubsonicResponseData?.Status != "ok")
                return new List<Song>();
            
            if (similarResponse.SubsonicResponseData.SimilarSongs?.Song == null)
                return new List<Song>();
            
            return similarResponse.SubsonicResponseData.SimilarSongs.Song.Select(s => new Song(
                s.Id, s.Parent, s.Track, s.Title, s.Artist, s.Album, s.AlbumId, s.Duration, s.CoverArt
            )).ToList();
        }
        catch (JsonException)
        {
            return new List<Song>();
        }
    }

    public async Task<Song> GetSong(string id)
    {
        var parameters = GetBasicParams();
        parameters["id"] = id;
        
        var response = await MakeRequestAsync("getSong", parameters);
        
        try
        {
            var songResponse = JsonSerializer.Deserialize<SubsonicResponse<SongResponseData>>(response);
            
            if (songResponse?.SubsonicResponseData?.Status != "ok")
            {
                var error = songResponse?.SubsonicResponseData?.Error?.Message ?? "Unknown error";
                throw new InvalidOperationException($"Subsonic API error: {error}");
            }
            
            var s = songResponse.SubsonicResponseData.Song;
            if (s == null)
                throw new InvalidOperationException($"Song with ID {id} not found");
            
            return new Song(s.Id, s.Parent, s.Track, s.Title, s.Artist, s.Album, s.AlbumId, s.Duration, s.CoverArt);
        }
        catch (JsonException ex)
        {
            throw new InvalidOperationException($"Failed to parse song response: {ex.Message}", ex);
        }
    }

    public async Task<SearchResult> Search(string query, int count = 20)
    {
        var parameters = GetBasicParams();
        parameters["query"] = query;
        parameters["artistCount"] = count.ToString();
        parameters["albumCount"] = count.ToString();
        parameters["songCount"] = count.ToString();
        
        var response = await MakeRequestAsync("search3", parameters);
        
        try
        {
            var searchResponse = JsonSerializer.Deserialize<SubsonicResponse<SearchResponseData>>(response);
            
            if (searchResponse?.SubsonicResponseData?.Status != "ok")
                return new SearchResult { Artists = new(), Albums = new(), Songs = new() };
            
            var result = searchResponse.SubsonicResponseData.SearchResult;
            if (result == null)
                return new SearchResult { Artists = new(), Albums = new(), Songs = new() };
            
            return new SearchResult
            {
                Artists = result.Artist.Select(a => new Artist
                {
                    Id = a.Id,
                    Name = a.Name,
                    AlbumCount = a.AlbumCount
                }).ToList(),
                Albums = result.Album.Select(a => new Album
                {
                    Id = a.Id,
                    Name = a.Name,
                    Artist = a.Artist,
                    ArtistId = a.ArtistId,
                    Year = a.Year,
                    CoverArt = a.CoverArt,
                    SongCount = a.SongCount,
                    Duration = a.Duration
                }).ToList(),
                Songs = result.Song.Select(s => new Song(
                    s.Id, s.Parent, s.Track, s.Title, s.Artist, s.Album, s.AlbumId, s.Duration, s.CoverArt
                )).ToList()
            };
        }
        catch (JsonException)
        {
            return new SearchResult { Artists = new(), Albums = new(), Songs = new() };
        }
    }

    public Uri GetSongUri(string id)
    {
        var parameters = GetBasicParams();
        parameters["id"] = id;
        
        return BuildUri("stream", parameters);
    }

    public string GetCoverArtUri(string id)
    {
        var parameters = GetBasicParams();
        parameters["id"] = id;
        parameters["size"] = "300";
        
        return BuildUri("getCoverArt", parameters).ToString();
    }

    public async Task<byte[]> GetCoverArt(string id)
    {
        var uri = GetCoverArtUri(id);
        return await _httpClient.GetByteArrayAsync(uri);
    }

    public async Task Scrobble(string id)
    {
        var parameters = GetBasicParams();
        parameters["id"] = id;
        parameters["submission"] = "true";
        
        try
        {
            await MakeRequestAsync("scrobble", parameters);
        }
        catch
        {
            // Scrobbling is non-critical, ignore errors
        }
    }

    public void Logout()
    {
        _account = new Account();
    }

    public Dictionary<string, string> GetBasicParams()
    {
        return new Dictionary<string, string>
        {
            ["u"] = _account.Username,
            ["t"] = _account.Token,
            ["s"] = _account.Salt,
            ["c"] = ClientName,
            ["v"] = ApiVersion,
            ["f"] = "json"
        };
    }

    private async Task<string> MakeRequestAsync(string endpoint, Dictionary<string, string> parameters)
    {
        var uri = BuildUri(endpoint, parameters);
        
        try
        {
            var response = await _httpClient.GetAsync(uri);
            response.EnsureSuccessStatusCode();
            return await response.Content.ReadAsStringAsync();
        }
        catch (HttpRequestException ex)
        {
            throw new InvalidOperationException($"Failed to make request to {endpoint}: {ex.Message}", ex);
        }
    }

    private Uri BuildUri(string endpoint, Dictionary<string, string> parameters)
    {
        var baseUrl = _account.Url.TrimEnd('/');
        var url = $"{baseUrl}/rest/{endpoint}";
        
        var queryParams = string.Join("&", parameters.Select(kvp => 
            $"{Uri.EscapeDataString(kvp.Key)}={Uri.EscapeDataString(kvp.Value)}"));
        
        return new Uri($"{url}?{queryParams}");
    }

    private static string GenerateSalt()
    {
        var bytes = new byte[16];
        using var rng = RandomNumberGenerator.Create();
        rng.GetBytes(bytes);
        return Convert.ToHexString(bytes).ToLowerInvariant();
    }

    private static string GenerateToken(string password, string salt)
    {
        var combined = password + salt;
        var bytes = Encoding.UTF8.GetBytes(combined);
        var hash = MD5.HashData(bytes);
        return Convert.ToHexString(hash).ToLowerInvariant();
    }
}