using Saki.Core.Models;
using Saki.Core.Services;
using SoundFlow.Abstracts;
using SoundFlow.Backends.MiniAudio;
using SoundFlow.Components;
using SoundFlow.Providers;
using SoundFlow.Enums;
using CorePlaybackState = Saki.Core.Models.PlaybackState;

namespace Saki.Core.Services.Audio;

public class SoundFlowAudioService : IAudioService, IDisposable
{
    private readonly MiniAudioEngine _audioEngine;
    private SoundPlayer? _soundPlayer;
    private bool _isPlaying;
    private bool _isPaused;
    private float _volume = 1.0f;
    private float _duration = 0.0f;
    private bool _disposed = false;
    private Timer? _positionUpdateTimer;

    public event EventHandler<PlaybackStateChangedEventArgs>? PlaybackStateChanged;
    public event EventHandler<TimeChangedEventArgs>? TimeChanged;
    public event EventHandler<VolumeChangedEventArgs>? VolumeChanged;
    public event EventHandler? PlaybackCompleted;

    public bool IsPlaying => _isPlaying;
    public bool IsPaused => _isPaused;
    public float Volume => _volume;

    public SoundFlowAudioService()
    {
        try
        {
            // Initialize MiniAudio engine with 48kHz sample rate and playback capability
            _audioEngine = new MiniAudioEngine(48000, Capability.Playback);
        }
        catch (Exception ex)
        {
            throw new InvalidOperationException("Failed to initialize SoundFlow audio engine", ex);
        }
    }

    public async Task LoadAsync(string uri)
    {
        try
        {
            if (_disposed)
                throw new ObjectDisposedException(nameof(SoundFlowAudioService));

            // Stop and remove existing player if any
            if (_soundPlayer != null)
            {
                _soundPlayer.Stop();
                _soundPlayer.PlaybackEnded -= OnSoundPlayerPlaybackEnded;
                Mixer.Master.RemoveComponent(_soundPlayer);
                _soundPlayer = null;
            }

            // Create new sound player with network data provider for URL loading
            var dataProvider = new NetworkDataProvider(uri);
            _soundPlayer = new SoundPlayer(dataProvider);
            _soundPlayer.Volume = _volume;
            
            // Add player to the master mixer (required for SoundFlow)
            Mixer.Master.AddComponent(_soundPlayer);
            
            // Set up event handlers
            _soundPlayer.PlaybackEnded += OnSoundPlayerPlaybackEnded;
            
            // Get duration if available (may be available after loading)
            _duration = _soundPlayer.Duration;
            
            // Set initial state
            _isPlaying = false;
            _isPaused = false;
            OnPlaybackStateChanged(CorePlaybackState.Stopped);
            
            // Simulate async loading completion
            await Task.CompletedTask;
        }
        catch (Exception ex)
        {
            throw new InvalidOperationException($"Failed to load audio from {uri}", ex);
        }
    }

    public void Play()
    {
        try
        {
            if (_disposed)
                throw new ObjectDisposedException(nameof(SoundFlowAudioService));

            if (_soundPlayer == null)
                throw new InvalidOperationException("No audio loaded. Call LoadAsync first.");

            _soundPlayer.Play();
            _isPlaying = true;
            _isPaused = false;
            OnPlaybackStateChanged(CorePlaybackState.Playing);
            
            // Start position update timer
            StartPositionUpdateTimer();
        }
        catch (Exception ex)
        {
            throw new InvalidOperationException("Failed to start playback", ex);
        }
    }

    public void Pause()
    {
        if (!_isPlaying)
            return;

        try
        {
            if (_disposed)
                throw new ObjectDisposedException(nameof(SoundFlowAudioService));

            if (_soundPlayer == null)
                return;

            _soundPlayer.Pause();
            _isPlaying = false;
            _isPaused = true;
            OnPlaybackStateChanged(CorePlaybackState.Paused);
            
            // Stop position update timer
            StopPositionUpdateTimer();
        }
        catch (Exception ex)
        {
            throw new InvalidOperationException("Failed to pause playback", ex);
        }
    }

    public void Stop()
    {
        try
        {
            if (_disposed)
                throw new ObjectDisposedException(nameof(SoundFlowAudioService));

            if (_soundPlayer == null)
                return;

            _soundPlayer.Stop();
            _isPlaying = false;
            _isPaused = false;
            OnPlaybackStateChanged(CorePlaybackState.Stopped);
            
            // Stop position update timer
            StopPositionUpdateTimer();
        }
        catch (Exception ex)
        {
            throw new InvalidOperationException("Failed to stop playback", ex);
        }
    }

    public void Seek(float position)
    {
        try
        {
            if (_disposed)
                throw new ObjectDisposedException(nameof(SoundFlowAudioService));

            if (_soundPlayer == null)
                return;

            // Clamp position between 0 and 1
            position = Math.Max(0, Math.Min(1, position));
            
            // Convert normalized position to absolute time in seconds
            var targetTimeSeconds = position * _duration;
            
            // Use SoundFlow's Seek method with seconds
            bool seekResult = _soundPlayer.Seek(targetTimeSeconds);
            
            if (seekResult)
            {
                OnTimeChanged((long)(targetTimeSeconds * 1000));
            }
        }
        catch (Exception ex)
        {
            throw new InvalidOperationException($"Failed to seek to position {position}", ex);
        }
    }

    public void SetVolume(float volume)
    {
        volume = Math.Max(0, Math.Min(1, volume));
        
        try
        {
            if (_disposed)
                throw new ObjectDisposedException(nameof(SoundFlowAudioService));

            _volume = volume;
            
            if (_soundPlayer != null)
            {
                _soundPlayer.Volume = volume;
            }
            
            OnVolumeChanged(volume);
        }
        catch (Exception ex)
        {
            throw new InvalidOperationException($"Failed to set volume to {volume}", ex);
        }
    }

    public float GetPosition()
    {
        try
        {
            if (_disposed || _soundPlayer == null)
                return 0;

            // Get current playback position from SoundFlow
            var currentTime = _soundPlayer.Time;
            
            // Return normalized position (0-1)
            return _soundPlayer.Duration > 0 ? currentTime / _soundPlayer.Duration : 0;
        }
        catch
        {
            return 0;
        }
    }

    public float GetDuration()
    {
        try
        {
            if (_disposed || _soundPlayer == null)
                return 0;

            // Get duration directly from SoundPlayer
            return _soundPlayer.Duration;
        }
        catch
        {
            return 0;
        }
    }

    private void OnSoundPlayerPlaybackEnded(object? sender, EventArgs e)
    {
        _isPlaying = false;
        _isPaused = false;
        OnPlaybackStateChanged(CorePlaybackState.Stopped);
        OnPlaybackCompleted();
    }

    private void OnPlaybackStateChanged(CorePlaybackState state)
    {
        PlaybackStateChanged?.Invoke(this, new PlaybackStateChangedEventArgs { State = state });
    }

    private void OnTimeChanged(long time)
    {
        TimeChanged?.Invoke(this, new TimeChangedEventArgs { Time = time });
    }

    private void OnVolumeChanged(float volume)
    {
        VolumeChanged?.Invoke(this, new VolumeChangedEventArgs { Volume = volume });
    }

    private void OnPlaybackCompleted()
    {
        PlaybackCompleted?.Invoke(this, EventArgs.Empty);
    }
    
    private void StartPositionUpdateTimer()
    {
        // Update position every 100ms for smooth progress
        _positionUpdateTimer = new Timer(UpdatePosition, null, TimeSpan.Zero, TimeSpan.FromMilliseconds(100));
    }
    
    private void StopPositionUpdateTimer()
    {
        _positionUpdateTimer?.Dispose();
        _positionUpdateTimer = null;
    }
    
    private void UpdatePosition(object? state)
    {
        if (_disposed || _soundPlayer == null || !_isPlaying)
            return;
            
        try
        {
            var currentTime = _soundPlayer.Time;
            OnTimeChanged((long)(currentTime * 1000));
        }
        catch
        {
            // Ignore errors during position update
        }
    }

    public void Dispose()
    {
        if (_disposed)
            return;

        try
        {
            Stop();
            
            // Stop and dispose timer
            StopPositionUpdateTimer();
            
            // Stop sound player and remove from mixer
            if (_soundPlayer != null)
            {
                _soundPlayer.Stop();
                _soundPlayer.PlaybackEnded -= OnSoundPlayerPlaybackEnded;
                Mixer.Master.RemoveComponent(_soundPlayer);
                _soundPlayer = null;
            }
            
            // Dispose audio engine
            _audioEngine?.Dispose();
        }
        catch
        {
            // Suppress exceptions during disposal
        }
        finally
        {
            _disposed = true;
        }
    }
}