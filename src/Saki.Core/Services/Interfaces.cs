using Saki.Core.Models;

namespace Saki.Core.Services;

public interface IAudioService : IDisposable
{
    event EventHandler<PlaybackStateChangedEventArgs>? PlaybackStateChanged;
    event EventHandler<TimeChangedEventArgs>? TimeChanged;
    event EventHandler<VolumeChangedEventArgs>? VolumeChanged;
    event EventHandler? PlaybackCompleted;

    Task LoadAsync(string uri);
    void Play();
    void Pause();
    void Stop();
    void Seek(float position);
    void SetVolume(float volume);
    float GetPosition();
    float GetDuration();
    bool IsPlaying { get; }
    bool IsPaused { get; }
    float Volume { get; }
}

public interface IMusicPlayerService
{
    event EventHandler<CurrentStateChangedEventArgs>? CurrentStateChanged;
    event EventHandler<TimeChangedEventArgs>? TimeChanged;
    event EventHandler<VolumeChangedEventArgs>? VolumeChanged;

    CurrentState GetCurrentState();
    void PlayPause();
    void Play();
    void Pause();
    void Stop();
    void Next();
    void Previous();
    void SkipTo(int index);
    void Seek(float time, bool relative = false);
    void SetVolume(int volume, bool relative = false);
    void ToggleRepeat();
    void Shuffle();
    void AddToCurrentPlaylist(Song song);
    
    Task PlayAlbum(string albumId, int track = 0);
    Task PlayPlaylist(string playlistId, int track = 0);
    Task PlayRadio(string songId);
    
    // Add Volume property to access current volume
    float Volume { get; }
}

public interface ISubsonicService
{
    void Configure(Account account);
    Account GetActiveAccount();
    
    Task<Album> GetAlbum(string id);
    Task<List<Album>> GetAlbums(string type = "frequent", int size = 10);
    Task<List<Album>> GetAllAlbums();
    Task<Artist> GetArtist(string id);
    Task<List<Artist>> GetArtists();
    Task<Playlist> GetPlaylist(string id);
    Task<List<Playlist>> GetPlaylists();
    Task<List<Song>> GetRandomSongs();
    Task<List<Song>> GetSimilarSongs(string id);
    Task<Song> GetSong(string id);
    Task<SearchResult> Search(string query, int count = 20);
    
    Uri GetSongUri(string id);
    string GetCoverArtUri(string id);
    Task<byte[]> GetCoverArt(string id);
    Task Scrobble(string id);
    
    void Logout();
}

public interface IMediaIntegration
{
    Task InitializeAsync();
    Task UpdateNowPlayingAsync(Song song);
    Task SetPlaybackStateAsync(PlaybackState state);
    void RegisterCommandHandlers(IMusicPlayerService musicPlayer);
}

public class PlaybackStateChangedEventArgs : EventArgs
{
    public PlaybackState State { get; set; }
}

public class TimeChangedEventArgs : EventArgs
{
    public long Time { get; set; }
}

public class VolumeChangedEventArgs : EventArgs
{
    public float Volume { get; set; }
}