using Saki.Core.Models;
using Saki.Core.Services;
using System.Runtime.InteropServices;

namespace Saki.Core.Services.MediaIntegration;

/// <summary>
/// Windows SMTC integration using MediaPlayer + SystemMediaTransportControls.
/// Ported from original SonicLair.Cli Control.cs.
/// </summary>
public class WindowsMediaIntegration : IMediaIntegration
{
#if WINDOWS
    private Windows.Media.Playback.MediaPlayer? _mediaPlayer;
    private Windows.Media.SystemMediaTransportControls? _smtc;
    private IMusicPlayerService? _musicPlayerService;
#endif

    public Task InitializeAsync()
    {
#if WINDOWS
        if (!RuntimeInformation.IsOSPlatform(OSPlatform.Windows))
            return Task.CompletedTask;

        try
        {
            _mediaPlayer = new Windows.Media.Playback.MediaPlayer();
            _smtc = _mediaPlayer.SystemMediaTransportControls;
            _smtc.IsEnabled = false;
            _mediaPlayer.CommandManager.IsEnabled = false;

            _smtc.IsPlayEnabled = true;
            _smtc.IsPauseEnabled = true;
            _smtc.IsStopEnabled = true;
            _smtc.IsNextEnabled = true;
            _smtc.IsPreviousEnabled = true;

            _smtc.ButtonPressed += SmtcButtonPressed;
            _smtc.PlaybackStatus = Windows.Media.MediaPlaybackStatus.Closed;
        }
        catch (Exception)
        {
            // SMTC unavailable (e.g. running in a container or older Windows)
        }
#endif
        return Task.CompletedTask;
    }

    public async Task UpdateNowPlayingAsync(Song song)
    {
#if WINDOWS
        if (!RuntimeInformation.IsOSPlatform(OSPlatform.Windows) || _smtc == null)
            return;

        try
        {
            _smtc.IsEnabled = true;
            var updater = _smtc.DisplayUpdater;
            updater.ClearAll();

            updater.Type = Windows.Media.MediaPlaybackType.Music;
            updater.MusicProperties.Artist = song.Artist;
            updater.MusicProperties.AlbumTitle = song.Album;
            updater.MusicProperties.Title = song.Title;

            // Download thumbnail and set it
            if (!string.IsNullOrEmpty(song.Image) && song.Image.StartsWith("http"))
            {
                try
                {
                    string fileName = "album.png";
                    using var httpClient = new HttpClient();
                    byte[] imageBytes = await httpClient.GetByteArrayAsync(song.Image);
                    await File.WriteAllBytesAsync(fileName, imageBytes);

                    string imagePath = Path.Combine(Environment.CurrentDirectory, fileName);
                    var storageFile = await Windows.Storage.StorageFile.GetFileFromPathAsync(imagePath);
                    updater.Thumbnail = Windows.Storage.Streams.RandomAccessStreamReference.CreateFromFile(storageFile);
                }
                catch
                {
                    // Non-critical: thumbnail unavailable
                }
            }

            updater.Update();
        }
        catch (Exception)
        {
            // SMTC update failed, non-critical
        }
#else
        await Task.CompletedTask;
#endif
    }

    public Task SetPlaybackStateAsync(PlaybackState state)
    {
#if WINDOWS
        if (!RuntimeInformation.IsOSPlatform(OSPlatform.Windows) || _smtc == null)
            return Task.CompletedTask;

        try
        {
            _smtc.PlaybackStatus = state switch
            {
                PlaybackState.Playing => Windows.Media.MediaPlaybackStatus.Playing,
                PlaybackState.Paused => Windows.Media.MediaPlaybackStatus.Paused,
                PlaybackState.Stopped => Windows.Media.MediaPlaybackStatus.Stopped,
                _ => Windows.Media.MediaPlaybackStatus.Closed
            };
        }
        catch
        {
            // SMTC state update failed, non-critical
        }
#endif
        return Task.CompletedTask;
    }

    public void RegisterCommandHandlers(IMusicPlayerService musicPlayer)
    {
#if WINDOWS
        _musicPlayerService = musicPlayer;
#endif
    }

#if WINDOWS
    private void SmtcButtonPressed(
        Windows.Media.SystemMediaTransportControls sender,
        Windows.Media.SystemMediaTransportControlsButtonPressedEventArgs args)
    {
        if (_musicPlayerService == null) return;

        switch (args.Button)
        {
            case Windows.Media.SystemMediaTransportControlsButton.Play:
                _musicPlayerService.Play();
                break;
            case Windows.Media.SystemMediaTransportControlsButton.Pause:
                _musicPlayerService.Pause();
                break;
            case Windows.Media.SystemMediaTransportControlsButton.Stop:
                _musicPlayerService.Pause();
                break;
            case Windows.Media.SystemMediaTransportControlsButton.Next:
                _musicPlayerService.Next();
                break;
            case Windows.Media.SystemMediaTransportControlsButton.Previous:
                _musicPlayerService.Previous();
                break;
        }
    }
#endif
}
