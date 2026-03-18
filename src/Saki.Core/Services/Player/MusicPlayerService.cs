using Saki.Core.Extensions;
using Saki.Core.Models;
using Saki.Core.Services;
using CorePlaybackState = Saki.Core.Models.PlaybackState;

namespace Saki.Core.Services.Player;

public class MusicPlayerService : IMusicPlayerService, IDisposable
{
    private readonly IAudioService _audioService;
    private readonly ISubsonicService _subsonicService;
    private readonly IMediaIntegration? _mediaIntegration;
    
    private Playlist _currentPlaylist;
    private List<Song> _originalPlaylist;
    private Song? _currentTrack;
    private RepeatStatus _repeatStatus;
    private bool _isShuffled;

    public event EventHandler<CurrentStateChangedEventArgs>? CurrentStateChanged;
    public event EventHandler<TimeChangedEventArgs>? TimeChanged;
    public event EventHandler<VolumeChangedEventArgs>? VolumeChanged;

    public float Volume => _audioService.Volume;

    public MusicPlayerService(
        IAudioService audioService, 
        ISubsonicService subsonicService,
        IMediaIntegration? mediaIntegration = null)
    {
        _audioService = audioService;
        _subsonicService = subsonicService;
        _mediaIntegration = mediaIntegration;
        
        _currentPlaylist = new Playlist();
        _originalPlaylist = new List<Song>();
        _repeatStatus = RepeatStatus.None;
        _isShuffled = false;

        // Subscribe to audio service events
        _audioService.PlaybackStateChanged += OnAudioPlaybackStateChanged;
        _audioService.TimeChanged += OnAudioTimeChanged;
        _audioService.VolumeChanged += OnAudioVolumeChanged;
        _audioService.PlaybackCompleted += OnAudioPlaybackCompleted;

        // Initialize media integration if available
        _mediaIntegration?.InitializeAsync();
        _mediaIntegration?.RegisterCommandHandlers(this);
    }

    public CurrentState GetCurrentState()
    {
        var currentTrackIndex = -1;
        if (_currentTrack != null && _currentPlaylist.Entry.Count > 0)
        {
            currentTrackIndex = _currentPlaylist.Entry.IndexOf(_currentTrack);
        }
        
        return new CurrentState
        {
            CurrentTrack = _currentTrack,
            Position = (decimal)_audioService.GetPosition(),
            IsPlaying = _audioService.IsPlaying,
            Stopped = !_audioService.IsPlaying && !_audioService.IsPaused,
            CurrentPlaylist = _currentPlaylist,
            CurrentTrackIndex = currentTrackIndex,
            RepeatStatus = _repeatStatus,
            IsShuffled = _isShuffled
        };
    }

    public void PlayPause()
    {
        if (_audioService.IsPlaying)
        {
            Pause();
        }
        else
        {
            Play();
        }
    }

    public void Play()
    {
        if (_currentPlaylist.Entry.Count > 0 && _currentTrack == null)
        {
            _currentTrack = _currentPlaylist.Entry[0];
            _ = LoadAndStartCurrentTrackAsync();
            return;
        }

        _audioService.Play();
        _mediaIntegration?.SetPlaybackStateAsync(CorePlaybackState.Playing);
    }

    public void Pause()
    {
        _audioService.Pause();
        _mediaIntegration?.SetPlaybackStateAsync(CorePlaybackState.Paused);
    }

    public void Stop()
    {
        _audioService.Stop();
        _mediaIntegration?.SetPlaybackStateAsync(CorePlaybackState.Stopped);
    }

    public void Next()
    {
        if (_currentTrack == null || _currentPlaylist.Entry.Count == 0)
            return;

        var currentIndex = _currentPlaylist.Entry.IndexOf(_currentTrack);
        
        if (currentIndex == _currentPlaylist.Entry.Count - 1)
        {
            // At the end of playlist
            switch (_repeatStatus)
            {
                case RepeatStatus.None:
                case RepeatStatus.RepeatOne:
                    return;
                case RepeatStatus.RepeatAll:
                    _currentTrack = _currentPlaylist.Entry[0];
                    break;
            }
        }
        else
        {
            _currentTrack = _currentPlaylist.Entry[currentIndex + 1];
        }

        _ = LoadAndStartCurrentTrackAsync();
    }

    public void Previous()
    {
        if (_currentTrack == null || _currentPlaylist.Entry.Count == 0)
            return;

        var currentIndex = _currentPlaylist.Entry.IndexOf(_currentTrack);
        
        if (currentIndex == 0)
        {
            // At the beginning of playlist
            switch (_repeatStatus)
            {
                case RepeatStatus.None:
                case RepeatStatus.RepeatOne:
                    return;
                case RepeatStatus.RepeatAll:
                    _currentTrack = _currentPlaylist.Entry[^1];
                    break;
            }
        }
        else
        {
            _currentTrack = _currentPlaylist.Entry[currentIndex - 1];
        }

        _ = LoadAndStartCurrentTrackAsync();
    }

    public void SkipTo(int index)
    {
        if (index < 0 || index >= _currentPlaylist.Entry.Count)
            return;

        _currentTrack = _currentPlaylist.Entry[index];
        _ = LoadAndStartCurrentTrackAsync();
    }

    public void Seek(float time, bool relative = false)
    {
        if (relative)
        {
            var currentPosition = _audioService.GetPosition();
            var duration = _audioService.GetDuration();
            var newPosition = (currentPosition + time).Clamp(0f, duration);
            _audioService.Seek(newPosition / duration);
        }
        else
        {
            _audioService.Seek(time);
        }
    }

    public void SetVolume(int volume, bool relative = false)
    {
        var currentVolume = (int)(_audioService.Volume * 100);
        var newVolume = relative ? (currentVolume + volume).Clamp(0, 100) : volume.Clamp(0, 100);
        _audioService.SetVolume(newVolume / 100f);
    }

    public void ToggleRepeat()
    {
        _repeatStatus = _repeatStatus.Next();
        OnCurrentStateChanged();
    }

    public void Shuffle()
    {
        if (_isShuffled)
        {
            // Restore original order
            _currentPlaylist.Entry = new List<Song>(_originalPlaylist);
        }
        else
        {
            // Shuffle, but keep current track at the beginning
            if (_currentPlaylist.Entry.Count > 0 && _currentTrack != null)
            {
                _currentPlaylist.Entry.Shuffle();
                var currentIndex = _currentPlaylist.Entry.IndexOf(_currentTrack);
                if (currentIndex > 0)
                {
                    // Move current track to the beginning
                    var temp = _currentPlaylist.Entry[0];
                    _currentPlaylist.Entry[0] = _currentTrack;
                    _currentPlaylist.Entry[currentIndex] = temp;
                }
            }
        }
        
        _isShuffled = !_isShuffled;
        OnCurrentStateChanged();
    }

    public void AddToCurrentPlaylist(Song song)
    {
        _currentPlaylist.Entry.Add(song);
        _originalPlaylist.Add(song);
        OnCurrentStateChanged();
    }

    public async Task PlayAlbum(string albumId, int track = 0)
    {
        try
        {
            var album = await _subsonicService.GetAlbum(albumId);
            
            _currentPlaylist = new Playlist(
                "",
                album.Name,
                $"by {album.Artist}",
                _subsonicService.GetActiveAccount().Username,
                false,
                album.Song.Count,
                album.Song.Sum(s => s.Duration),
                "",
                "",
                album.Song
            );
            
            _originalPlaylist = album.Song.ToList();
            await LoadImagesAndPlay(track);
        }
        catch (Exception)
        {
            // TODO: Handle exception properly (logging, notifications, etc.)
            throw;
        }
    }

    public async Task PlayPlaylist(string playlistId, int track = 0)
    {
        try
        {
            var playlist = await _subsonicService.GetPlaylist(playlistId);
            _currentPlaylist = playlist;
            _originalPlaylist = playlist.Entry.ToList();
            await LoadImagesAndPlay(track);
        }
        catch (Exception)
        {
            // TODO: Handle exception properly
            throw;
        }
    }

    public async Task PlayRadio(string songId)
    {
        try
        {
            var similarSongs = await _subsonicService.GetSimilarSongs(songId);
            var baseSong = await _subsonicService.GetSong(songId);
            var songs = similarSongs.Prepend(baseSong).ToList();
            
            _currentPlaylist = new Playlist(
                "",
                $"Radio based on {baseSong.Title}",
                $"by {baseSong.Artist} from {baseSong.Album}",
                _subsonicService.GetActiveAccount().Username,
                false,
                songs.Count,
                songs.Sum(s => s.Duration),
                "",
                "",
                songs
            );
            
            _originalPlaylist = songs.ToList();
            await LoadImagesAndPlay(0);
        }
        catch (Exception)
        {
            // TODO: Handle exception properly
            throw;
        }
    }

    private async Task LoadImagesAndPlay(int track = 0)
    {
        // Load cover art URLs for all songs
        foreach (var song in _currentPlaylist.Entry)
        {
            song.Image = _subsonicService.GetCoverArtUri(song.AlbumId);
        }
        
        if (track < _currentPlaylist.Entry.Count)
        {
            _currentTrack = _currentPlaylist.Entry[track];
            await LoadAndStartCurrentTrackAsync();
        }
    }

    private async Task LoadAndStartCurrentTrackAsync()
    {
        await LoadCurrentTrackAsync();
        _audioService.Play();
        _mediaIntegration?.SetPlaybackStateAsync(CorePlaybackState.Playing);
    }

    private async Task LoadCurrentTrackAsync()
    {
        if (_currentTrack == null)
            return;

        try
        {
            var uri = _subsonicService.GetSongUri(_currentTrack.Id);
            await _audioService.LoadAsync(uri.ToString());
            
            // Update media integration
            await (_mediaIntegration?.UpdateNowPlayingAsync(_currentTrack) ?? Task.CompletedTask);
            
            // Scrobble in background
            _ = Task.Run(() => _subsonicService.Scrobble(_currentTrack.Id));
            
            OnCurrentStateChanged();
        }
        catch (Exception)
        {
            // TODO: Handle loading errors properly
            throw;
        }
    }

    private void OnAudioPlaybackStateChanged(object? sender, PlaybackStateChangedEventArgs e)
    {
        OnCurrentStateChanged();
        _mediaIntegration?.SetPlaybackStateAsync(e.State);
    }

    private void OnAudioTimeChanged(object? sender, TimeChangedEventArgs e)
    {
        TimeChanged?.Invoke(this, e);
    }

    private void OnAudioVolumeChanged(object? sender, VolumeChangedEventArgs e)
    {
        VolumeChanged?.Invoke(this, e);
    }

    private void OnAudioPlaybackCompleted(object? sender, EventArgs e)
    {
        // This callback fires on SoundFlow's audio thread.
        // Queue the auto-advance on a thread-pool thread to avoid blocking.
        ThreadPool.QueueUserWorkItem(_ =>
        {
            try
            {
                switch (_repeatStatus)
                {
                    case RepeatStatus.RepeatOne:
                        _ = LoadAndStartCurrentTrackAsync();
                        break;
                    case RepeatStatus.None:
                    case RepeatStatus.RepeatAll:
                        Next();
                        break;
                }
            }
            catch
            {
                // Swallow: auto-advance failure should not crash the app
            }
        });
    }

    private void OnCurrentStateChanged()
    {
        CurrentStateChanged?.Invoke(this, new CurrentStateChangedEventArgs
        {
            CurrentState = GetCurrentState()
        });
    }

    public void Dispose()
    {
        _audioService?.Dispose();
    }
}