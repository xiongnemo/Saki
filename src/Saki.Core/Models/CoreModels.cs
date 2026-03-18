using System.Text.Json.Serialization;
using Saki.Core.Extensions;

namespace Saki.Core.Models;

public class Song
{
    public Song(string id, string parent, int track, string title, string artist, string album, string albumId, int duration, string coverArt)
    {
        Id = id;
        Parent = parent;
        Track = track;
        Title = title;
        Artist = artist;
        Album = album;
        AlbumId = albumId;
        Duration = duration;
        CoverArt = coverArt;
    }

    public static Song GetDefault()
    {
        return new Song("", "", 0, "", "", "", "", 0, "");
    }

    public string Id { get; set; }
    public string Parent { get; set; }
    public int Track { get; set; }
    public string Title { get; set; }
    public string Artist { get; set; }
    public string Album { get; set; }
    public string AlbumId { get; set; }
    public int Duration { get; set; }
    public string CoverArt { get; set; }

    [JsonIgnore]
    public string GetSecondLine => $"by {Artist} from {Album}";

    [JsonIgnore]
    public string GetDuration => Duration.ToTimeString();

    [JsonIgnore]
    public string FirstLine => Title;

    [JsonIgnore]
    public string SecondLine => GetSecondLine;

    [JsonIgnore]
    public string Image { get; set; } = "";

    public override string ToString()
    {
        return Title;
    }
}

public class Album
{
    public string Id { get; set; } = "";
    public string Name { get; set; } = "";
    public string Artist { get; set; } = "";
    public string ArtistId { get; set; } = "";
    public int SongCount { get; set; }
    public int Duration { get; set; }
    public int Year { get; set; }
    public string CoverArt { get; set; } = "";
    public List<Song> Song { get; set; } = new();

    public override string ToString()
    {
        return Name;
    }
}

public class Artist
{
    public string Id { get; set; } = "";
    public string Name { get; set; } = "";
    public int AlbumCount { get; set; }
    public List<Album> Album { get; set; } = new();

    public override string ToString()
    {
        return Name;
    }
}

public class Playlist
{
    public Playlist()
    {
        Entry = new List<Song>();
    }

    public Playlist(string id, string name, string comment, string owner, bool isPublic, int songCount, int duration, string coverArt, string image, List<Song> entry)
    {
        Id = id;
        Name = name;
        Comment = comment;
        Owner = owner;
        Public = isPublic;
        SongCount = songCount;
        Duration = duration;
        CoverArt = coverArt;
        Image = image;
        Entry = entry;
    }

    public string Id { get; set; } = "";
    public string Name { get; set; } = "";
    public string Comment { get; set; } = "";
    public string Owner { get; set; } = "";
    public bool Public { get; set; }
    public int SongCount { get; set; }
    public int Duration { get; set; }
    public string CoverArt { get; set; } = "";
    public string Image { get; set; } = "";
    public string Created { get; set; } = "";
    public List<Song> Entry { get; set; }

    public override string ToString()
    {
        return Name;
    }
}

public class Account
{
    public string Username { get; set; } = "";
    public string Password { get; set; } = "";
    public string Url { get; set; } = "";
    public string Salt { get; set; } = "";
    public string Token { get; set; } = "";
    public bool UsePlaintext { get; set; } = false;
}

public class SearchResult
{
    public List<Artist>? Artists { get; set; }
    public List<Album>? Albums { get; set; }
    public List<Song>? Songs { get; set; }
}

public enum RepeatStatus
{
    None,
    RepeatOne,
    RepeatAll
}

public enum PlaybackState
{
    Stopped,
    Playing,
    Paused
}

public class CurrentState
{
    public Song? CurrentTrack { get; set; }
    public decimal Position { get; set; }
    public bool IsPlaying { get; set; }
    public bool Stopped { get; set; }
    public Playlist? CurrentPlaylist { get; set; }
    public int CurrentTrackIndex { get; set; } = -1;
    public RepeatStatus RepeatStatus { get; set; }
    public bool IsShuffled { get; set; }
}

public class CurrentStateChangedEventArgs : EventArgs
{
    public CurrentState CurrentState { get; set; } = new();
}