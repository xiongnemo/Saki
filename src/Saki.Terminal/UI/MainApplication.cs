using Saki.Core.Models;
using Saki.Core.Services;
using Saki.Terminal.Extensions;
using System.Collections.ObjectModel;
using System.Text.Json;
using Terminal.Gui;

namespace Saki.Terminal.UI;

public class MainApplication : Window
{
    private readonly IMusicPlayerService _musicPlayerService;
    private readonly ISubsonicService _subsonicService;

    // Keyboard handling (SonicLair pattern)
    private readonly Dictionary<Key, Action> _hotkeys = new();
    private readonly Dictionary<Key, Action> _keys = new();

    // Layout frames
    private FrameView? _mainContentView;
    private FrameView? _playerControlView;
    private FrameView? _playlistView;
    private FrameView? _controlsBarView;

    // List views
    private SakiListView<Artist>? _currentArtistsList;
    private SakiListView<Album>? _currentAlbumsList;
    private SakiListView<Song>? _currentSongsList;
    private SakiListView<Song>? _currentPlaylistView;

    // Player control widgets
    private ProgressBar? _playingTimeProgress;
    private ProgressBar? _volumeProgress;
    private Label? _timeElapsedLabel;
    private Label? _songDurationLabel;
    private Label? _volumeLabel;
    private Label? _volumePercentLabel;
    private Label? _repeatLabel;
    private Label? _shuffleLabel;

    // Navigation history
    private readonly List<Action> _navigationHistory = new();
    private readonly List<string> _breadcrumbs = new();

    // Track whether we're in the main interface
    private bool _isMainInterfaceLoaded = false;

    public MainApplication(IMusicPlayerService musicPlayerService, ISubsonicService subsonicService)
    {
        _musicPlayerService = musicPlayerService;
        _subsonicService = subsonicService;

        Title = "Saki";

        _musicPlayerService.CurrentStateChanged += OnCurrentStateChanged;
        _musicPlayerService.TimeChanged += OnTimeChanged;
        _musicPlayerService.VolumeChanged += OnVolumeChanged;

        RegisterKeyboardShortcuts();
        ShowLoginView();
    }

    public void RegisterHotKey(Key key, Action action) => _hotkeys[key] = action;
    public void RegisterKey(Key key, Action action) => _keys[key] = action;

    /// <summary>
    /// Register all keyboard shortcuts following SonicLair pattern
    /// </summary>
    private void RegisterKeyboardShortcuts()
    {
        RegisterHotKey(Key.Q.WithCtrl, () => Application.RequestStop());

        // Media controls
        RegisterHotKey(Key.N.WithCtrl, () => _musicPlayerService.Next());
        RegisterHotKey(Key.B.WithCtrl, () => _musicPlayerService.Previous());
        RegisterKey(Key.Space, () => { if (_isMainInterfaceLoaded) _musicPlayerService.PlayPause(); });

        // Navigation (Backspace only when main interface is loaded; Tab is handled by Terminal.Gui's focus system)
        RegisterKey(Key.Backspace, () => { if (_isMainInterfaceLoaded) HandleBackNavigation(); });

        // Views
        RegisterHotKey(Key.A.WithCtrl, () =>
        {
            if (!_isMainInterfaceLoaded) return;
            _ = ShowArtistsViewAsync();
        });
        RegisterHotKey(Key.L.WithCtrl, () =>
        {
            if (!_isMainInterfaceLoaded) return;
            _ = ShowAlbumsViewAsync();
        });
        RegisterHotKey(Key.P.WithCtrl, () =>
        {
            if (!_isMainInterfaceLoaded) return;
            _ = ShowPlaylistsViewAsync();
        });
        RegisterHotKey(Key.R.WithCtrl, () =>
        {
            if (!_isMainInterfaceLoaded) return;
            ShowSearchView();
        });

        // Playback controls
        RegisterHotKey(Key.T.WithCtrl, () => _musicPlayerService.ToggleRepeat());
        RegisterHotKey(Key.H.WithCtrl, () => _musicPlayerService.Shuffle());
        RegisterHotKey(Key.I.WithCtrl, () => _musicPlayerService.SetVolume(5, true));
        RegisterHotKey(Key.K.WithCtrl, () => _musicPlayerService.SetVolume(-5, true));

        // Seeking
        RegisterHotKey(Key.CursorRight.WithCtrl, () =>
        {
            var state = _musicPlayerService.GetCurrentState();
            if (state.IsPlaying && state.CurrentTrack != null)
            {
                var seekAmount = 10f / state.CurrentTrack.Duration;
                _musicPlayerService.Seek(seekAmount, true);
            }
        });
        RegisterHotKey(Key.CursorLeft.WithCtrl, () =>
        {
            var state = _musicPlayerService.GetCurrentState();
            if (state.IsPlaying && state.CurrentTrack != null)
            {
                var seekAmount = 10f / state.CurrentTrack.Duration;
                _musicPlayerService.Seek(-seekAmount, true);
            }
        });
    }

    protected override bool OnKeyDown(Key keyEvent)
    {
        if (_hotkeys.ContainsKey(keyEvent))
        {
            _hotkeys[keyEvent]();
            return true;
        }

        if (_keys.ContainsKey(keyEvent))
        {
            _keys[keyEvent]();
            return true;
        }

        return base.OnKeyDown(keyEvent);
    }

    // ─────────────────────────────────────────────────────────────────
    // Login View
    // ─────────────────────────────────────────────────────────────────

    private void ShowLoginView()
    {
        RemoveAll();

        var logo = new Label()
        {
            Text = @"                                      -
                                     +*+
                                   .+****:
                                  :*******-
                                 -*********=               .:-
                                =***********+.      .:-=+*****.
                               =*************+.-=+************.
                             .+**************  -**********+++*.
                       -=:  .****************   :**++=-:.   =*.
                      =****=*****************  -++. :++     =*.
                     =***********************  =***+***+    =*.
                   .+************************  =*******+-.: =*.
                  .**************************  =*****-    +***.
                 :***********************+++*  =****-      =**.
                -*********************=.       =****:       -*.
               =*********************=         =*****:       =
              +**********************-         =******+-::-=**+  .-
         =*-.*************************.       :****************+=**=
       .+******************************+-:::-+**********************+.
      :***************************************************************.
     :*****************************************************************:
    =*******************************************************************-
   +*********************************************************************=
 .+***********************************************************************+.",
            X = Pos.Center(),
            Y = 1,
            Width = Dim.Auto(),
            Height = Dim.Auto()
        };

        var loginLabel = new Label() { Text = "Username:", X = Pos.Center() - 25, Y = Pos.Bottom(logo) + 1 };
        var passwordLabel = new Label() { Text = "Password:", X = Pos.Left(loginLabel), Y = Pos.Bottom(loginLabel) };
        var urlLabel = new Label() { Text = "Server URL:", X = Pos.Left(passwordLabel), Y = Pos.Bottom(passwordLabel) };

        var usernameField = new TextField()
        {
            X = Pos.Right(urlLabel) + 1,
            Y = Pos.Top(loginLabel),
            Width = 40
        };
        var passwordField = new TextField()
        {
            X = Pos.Left(usernameField),
            Y = Pos.Top(passwordLabel),
            Width = 40,
            Secret = true
        };
        var urlField = new TextField()
        {
            X = Pos.Left(usernameField),
            Y = Pos.Top(urlLabel),
            Width = 40,
            Text = "https://"
        };

        var usePlaintext = new CheckBox()
        {
            X = Pos.Left(usernameField),
            Y = Pos.Bottom(urlField),
            Text = "Use plaintext password?"
        };

        var loginBtn = new Button()
        {
            Text = "Login",
            X = Pos.Left(usernameField),
            Y = Pos.Bottom(usePlaintext) + 1
        };
        var quitBtn = new Button()
        {
            Text = "Quit",
            X = Pos.Right(loginBtn) + 1,
            Y = Pos.Top(loginBtn)
        };

        var statusLabel = new Label()
        {
            Text = "",
            X = Pos.Left(usernameField),
            Y = Pos.Bottom(loginBtn) + 1,
            Width = 50,
            Height = 2
        };

        loginBtn.Accepting += (sender, e) =>
        {
            var account = new Account
            {
                Username = usernameField.Text?.ToString() ?? "",
                Password = passwordField.Text?.ToString() ?? "",
                Url = urlField.Text?.ToString() ?? "",
                UsePlaintext = usePlaintext.CheckedState == CheckState.Checked
            };
            PerformLogin(account, statusLabel);
        };

        quitBtn.Accepting += (sender, e) =>
        {
            Application.RequestStop();
        };

        Add(logo, loginLabel, passwordLabel, urlLabel,
            usernameField, passwordField, urlField,
            usePlaintext, loginBtn, quitBtn, statusLabel);

        // Try auto-login from saved config
        try
        {
            var configPath = Path.Combine(
                Environment.GetFolderPath(Environment.SpecialFolder.UserProfile),
                ".saki", "config.json");

            if (File.Exists(configPath))
            {
                var json = File.ReadAllText(configPath);
                var account = JsonSerializer.Deserialize<Account>(json);
                if (account != null && !string.IsNullOrEmpty(account.Username) &&
                    !string.IsNullOrEmpty(account.Password) && !string.IsNullOrEmpty(account.Url))
                {
                    usernameField.Text = account.Username;
                    passwordField.Text = account.Password;
                    urlField.Text = account.Url;
                    usePlaintext.CheckedState = account.UsePlaintext ? CheckState.Checked : CheckState.UnChecked;
                    statusLabel.Text = $"Welcome back, {account.Username}! Connecting...";
                    PerformLogin(account, statusLabel);
                }
            }
        }
        catch
        {
            // Ignore auto-login errors
        }

        usernameField.SetFocus();
    }

    private void PerformLogin(Account account, Label statusLabel)
    {
        _ = Task.Run(async () =>
        {
            try
            {
                _subsonicService.Configure(account);
                await _subsonicService.GetArtists();

                // Save config
                var configDir = Path.Combine(
                    Environment.GetFolderPath(Environment.SpecialFolder.UserProfile), ".saki");
                Directory.CreateDirectory(configDir);
                var configPath = Path.Combine(configDir, "config.json");
                var json = JsonSerializer.Serialize(account, new JsonSerializerOptions { WriteIndented = true });
                await File.WriteAllTextAsync(configPath, json);

                InvokeUi(() =>
                {
                    TransitionToMainInterface();
                });
            }
            catch (Exception ex)
            {
                InvokeUi(() =>
                {
                    statusLabel.Text = $"Login failed: {ex.Message}";
                });
            }
        });
    }

    private void TransitionToMainInterface()
    {
        RemoveAll();
        _isMainInterfaceLoaded = true;
        SetupLayout();
        UpdateBreadcrumbs("Artists");
        _navigationHistory.Clear();
        _navigationHistory.Add(() => { _ = ShowArtistsViewAsync(false); });
        RequestUiRefresh(this, true);
        _ = ShowArtistsViewAsync(false);
    }

    /// <summary>
    /// Queue an action on the UI thread AND wake the main loop so it runs immediately.
    /// Application.Invoke alone does not wake the loop in v2.0.0.
    /// </summary>
    private static void InvokeUi(Action action)
    {
        Application.Invoke(action);
        Application.Wakeup();
    }

    private void RequestUiRefresh(View? view = null, bool layout = false)
    {
        if (layout)
        {
            view?.SetNeedsLayout();
            SetNeedsLayout();
        }

        view?.SetNeedsDraw();
        SetNeedsDraw();
        Application.LayoutAndDraw();
        Application.Wakeup();
    }

    // ─────────────────────────────────────────────────────────────────
    // Layout
    // ─────────────────────────────────────────────────────────────────

    private void SetupLayout()
    {
        // Main content area (top-left, ~70% width)
        _mainContentView = new FrameView()
        {
            X = 0,
            Y = 0,
            Width = Dim.Percent(70),
            Height = Dim.Fill(9),
            Title = "Main",
            CanFocus = true,
            TabStop = TabBehavior.NoStop
        };

        // Current playlist sidebar (top-right, ~30% width)
        _playlistView = new FrameView()
        {
            X = Pos.Right(_mainContentView),
            Y = 0,
            Width = Dim.Fill(),
            Height = Dim.Fill(9),
            Title = "Current Playlist",
            CanFocus = true,
            TabStop = TabBehavior.NoStop
        };

        // Player controls (bottom, full width, 5 lines)
        _playerControlView = new FrameView()
        {
            X = 0,
            Y = Pos.Bottom(_mainContentView),
            Height = 5,
            Width = Dim.Fill(),
            Title = "Idle",
            CanFocus = true,
            TabStop = TabBehavior.NoStop
        };

        SetupIdlePlayerControls();

        // Controls bar at the very bottom (4 lines)
        _controlsBarView = new FrameView()
        {
            X = 0,
            Y = Pos.Bottom(_playerControlView),
            Height = 4,
            Width = Dim.Fill(),
            Title = "Controls",
            CanFocus = true,
            TabStop = TabBehavior.NoStop
        };

        _repeatLabel = new Label()
        {
            Text = "Repeat: Off",
            X = Pos.AnchorEnd(13),
            Y = 0,
            Width = 12,
            Height = 1,
            CanFocus = false
        };

        _shuffleLabel = new Label()
        {
            Text = "Shuffle: Off",
            X = Pos.AnchorEnd(14),
            Y = 1,
            Width = 13,
            Height = 1,
            CanFocus = false
        };

        var helpText = new Label()
        {
            Text = "C-a Artists | C-l Album | C-p Playlists | C-r Search | C-Right Fw(10s) | C-Left Bw(10s)\n"
                 + "C-q Quit | Space Play/Pause | C-b Prev | C-n Next | C-t Repeat | C-h Shuffle | C-m Add | BackSpace Back",
            X = 0,
            Y = 0,
            Width = Dim.Fill(15),
            Height = 2,
            CanFocus = false
        };

        _controlsBarView.Add(helpText, _repeatLabel, _shuffleLabel);

        Add(_mainContentView, _playlistView, _playerControlView, _controlsBarView);
    }

    private void SetupIdlePlayerControls()
    {
        if (_playerControlView == null) return;
        _playerControlView.RemoveAll();
        _playerControlView.Title = "Idle";

        _volumeLabel = new Label()
        {
            Text = "Vol[C-k/i]",
            X = 0,
            Y = 0,
            Width = 10,
            Height = 1,
            CanFocus = false
        };

        _volumeProgress = new ProgressBar()
        {
            X = Pos.Right(_volumeLabel) + 1,
            Y = 0,
            Width = Dim.Fill(5),
            Height = 1,
            ProgressBarFormat = ProgressBarFormat.SimplePlusPercentage,
            ProgressBarStyle = ProgressBarStyle.Blocks,
            CanFocus = false,
            Fraction = _musicPlayerService.Volume
        };

        _volumePercentLabel = new Label()
        {
            Text = $"{(int)(_musicPlayerService.Volume * 100)}%",
            X = Pos.AnchorEnd(5),
            Y = 0,
            Width = 4,
            Height = 1,
            CanFocus = false
        };

        var idleLabel = new Label()
        {
            Text = "Ready to play music",
            X = 0,
            Y = 1,
            CanFocus = false
        };

        _playerControlView.Add(_volumeLabel, _volumeProgress, _volumePercentLabel, idleLabel);
    }

    // ─────────────────────────────────────────────────────────────────
    // Navigation helpers
    // ─────────────────────────────────────────────────────────────────

    /// <summary>
    /// Register global shortcuts on a SakiListView that must be handled at the list-view
    /// level because Terminal.Gui v2's built-in key bindings consume them before they
    /// reach the Window-level OnKeyDown handler.
    /// </summary>
    private void RegisterGlobalHotkeys(SakiListView<Artist> lv, bool isPlaylist = false) => RegisterGlobalHotkeysCore(lv, isPlaylist);
    private void RegisterGlobalHotkeys(SakiListView<Album> lv, bool isPlaylist = false) => RegisterGlobalHotkeysCore(lv, isPlaylist);
    private void RegisterGlobalHotkeys(SakiListView<Song> lv, bool isPlaylist = false) => RegisterGlobalHotkeysCore(lv, isPlaylist);
    private void RegisterGlobalHotkeys(SakiListView<Playlist> lv, bool isPlaylist = false) => RegisterGlobalHotkeysCore(lv, isPlaylist);

    private void RegisterGlobalHotkeysCore<T>(SakiListView<T> lv, bool isPlaylist)
    {
        // Playback controls that the base ListView would otherwise swallow
        lv.RegisterHotKey(Key.Space, () => _musicPlayerService.PlayPause());
        lv.RegisterHotKey(Key.N.WithCtrl, () => _musicPlayerService.Next());
        lv.RegisterHotKey(Key.B.WithCtrl, () => _musicPlayerService.Previous());

        // Tab: switch between main content and playlist sidebar
        lv.RegisterHotKey(Key.Tab, () =>
        {
            if (isPlaylist)
            {
                if (_currentSongsList is { Visible: true }) { _currentSongsList.FocusFirst(); return; }
                if (_currentAlbumsList is { Visible: true }) { _currentAlbumsList.FocusFirst(); return; }
                if (_currentArtistsList is { Visible: true }) { _currentArtistsList.FocusFirst(); return; }
            }
            else
            {
                if (_currentPlaylistView is { Visible: true })
                {
                    var state = _musicPlayerService.GetCurrentState();
                    if (state?.CurrentTrackIndex >= 0)
                        _currentPlaylistView.ScrollToItem(state.CurrentTrackIndex);
                    _currentPlaylistView.FocusFirst();
                }
            }
        });
    }

    private void UpdateBreadcrumbs(params string[] crumbs)
    {
        _breadcrumbs.Clear();
        _breadcrumbs.Add("Saki");

        var account = _subsonicService.GetActiveAccount();
        if (!string.IsNullOrEmpty(account?.Username))
        {
            _breadcrumbs.Add(GetServerName(account.Url));
        }
        _breadcrumbs.AddRange(crumbs);
        Title = string.Join(" :: ", _breadcrumbs);
    }

    private string GetServerName(string url)
    {
        try { return new Uri(url).Host; }
        catch { return "Server"; }
    }

    private bool HandleBackNavigation()
    {
        if (_navigationHistory.Count < 2)
            return false;

        _navigationHistory.RemoveAt(_navigationHistory.Count - 1);
        _navigationHistory.Last()();
        return true;
    }

    // ─────────────────────────────────────────────────────────────────
    // Artists View
    // ─────────────────────────────────────────────────────────────────

    private Task ShowArtistsViewAsync(bool pushHistory = true)
    {
        if (_mainContentView == null) return Task.CompletedTask;

        if (pushHistory)
            _navigationHistory.Add(() => { _ = ShowArtistsViewAsync(false); });

        ShowLoading(_mainContentView, "Artists", "Loading artists...");
        UpdateBreadcrumbs("Artists");

        return Task.Run(async () =>
        {
            try
            {
                var artists = await _subsonicService.GetArtists();
                InvokeUi(() =>
                {
                    _mainContentView.RemoveAll();
                    if (artists?.Count > 0)
                    {
                        var maxNameWidth = artists.Max(a => a.Name.StandardizedStringLength());
                        var maxAlbumWidth = artists.Max(a => a.AlbumCount.ToString().Length);

                        _currentArtistsList = new SakiListView<Artist>(artist =>
                        {
                            var tag = artist.AlbumCount > 1 ? "Albums" : "Album";
                            return $"{artist.Name.RunePadRight(maxNameWidth)} {artist.AlbumCount.ToString().RunePadLeft(maxAlbumWidth)} {tag}";
                        })
                        {
                            X = 0, Y = 0,
                            Width = Dim.Fill(),
                            Height = Dim.Fill()
                        };

                        _currentArtistsList.ItemSelected += (sender, selectedArtist) =>
                        {
                            _ = ShowArtistAlbumsAsync(selectedArtist, true);
                        };

                        RegisterGlobalHotkeys(_currentArtistsList);
                        _currentArtistsList.SetDataSource(artists);
                        _mainContentView.Add(_currentArtistsList);
                        _currentArtistsList.FocusFirst();
                    }
                    else
                    {
                        _mainContentView.Add(new Label() { Text = "No artists found.", X = Pos.Center(), Y = Pos.Center() });
                    }
                    RequestUiRefresh(_mainContentView, true);
                });
            }
            catch (Exception ex)
            {
                InvokeUi(() =>
                {
                    ShowError(_mainContentView, $"Error loading artists: {ex.Message}");
                    RequestUiRefresh(_mainContentView, true);
                });
            }
        });
    }

    private Task ShowArtistAlbumsAsync(Artist artist, bool pushHistory = true)
    {
        if (_mainContentView == null) return Task.CompletedTask;

        if (pushHistory)
            _navigationHistory.Add(() => { _ = ShowArtistAlbumsAsync(artist, false); });

        ShowLoading(_mainContentView, artist.Name, $"Loading albums for {artist.Name}...");
        UpdateBreadcrumbs("Artists", artist.Name, "Albums");

        return Task.Run(async () =>
        {
            try
            {
                var artistDetails = await _subsonicService.GetArtist(artist.Id);
                InvokeUi(() =>
                {
                    _mainContentView.RemoveAll();
                    if (artistDetails?.Album?.Count > 0)
                    {
                        _currentAlbumsList = new SakiListView<Album>(album => $"({album.Year:0000}) {album.Name}")
                        {
                            X = 0, Y = 0,
                            Width = Dim.Fill(),
                            Height = Dim.Fill()
                        };

                        _currentAlbumsList.ItemSelected += (sender, selectedAlbum) =>
                        {
                            _ = ShowAlbumSongsAsync(selectedAlbum, true);
                        };

                        RegisterGlobalHotkeys(_currentAlbumsList);
                        _currentAlbumsList.SetDataSource(artistDetails.Album);
                        _mainContentView.Add(_currentAlbumsList);
                        _currentAlbumsList.FocusFirst();
                    }
                    else
                    {
                        _mainContentView.Add(new Label() { Text = $"No albums found for {artist.Name}.", X = Pos.Center(), Y = Pos.Center() });
                    }
                    RequestUiRefresh(_mainContentView, true);
                });
            }
            catch (Exception ex)
            {
                InvokeUi(() =>
                {
                    ShowError(_mainContentView, $"Error loading albums: {ex.Message}");
                    RequestUiRefresh(_mainContentView, true);
                });
            }
        });
    }

    private Task ShowAlbumSongsAsync(Album album, bool pushHistory = true)
    {
        if (_mainContentView == null) return Task.CompletedTask;

        if (pushHistory)
            _navigationHistory.Add(() => { _ = ShowAlbumSongsAsync(album, false); });

        ShowLoading(_mainContentView, $"{album.Name} :: {album.Artist}", $"Loading songs for {album.Name}...");
        UpdateBreadcrumbs("Artists", album.Artist, "Albums", album.Name, "Songs");

        return Task.Run(async () =>
        {
            try
            {
                var albumDetails = await _subsonicService.GetAlbum(album.Id);
                InvokeUi(() =>
                {
                    _mainContentView.RemoveAll();
                    if (albumDetails?.Song?.Count > 0)
                    {
                        var maxTrackWidth = albumDetails.Song.Max(s => s.Track.ToString().Length);
                        var maxTitleWidth = albumDetails.Song.Max(s => s.Title.StandardizedStringLength());

                        _currentSongsList = new SakiListView<Song>(song =>
                            $"{song.Track.ToString().RunePadLeft(maxTrackWidth)} - {song.Title.RunePadRight(maxTitleWidth)} [{song.Duration.GetAsMMSS()}]")
                        {
                            X = 0, Y = 0,
                            Width = Dim.Fill(),
                            Height = Dim.Fill()
                        };

                        _currentSongsList.ItemSelected += (sender, selectedSong) =>
                        {
                            var trackIndex = albumDetails.Song.IndexOf(selectedSong);
                            _ = _musicPlayerService.PlayAlbum(album.Id, trackIndex);
                        };

                        _currentSongsList.RegisterHotKey(Key.M.WithCtrl, () =>
                        {
                            var selected = _currentSongsList.GetSelectedItem();
                            if (selected != null)
                                _musicPlayerService.AddToCurrentPlaylist(selected);
                        });

                        RegisterGlobalHotkeys(_currentSongsList);
                        _currentSongsList.SetDataSource(albumDetails.Song);
                        _mainContentView.Add(_currentSongsList);
                        _currentSongsList.FocusFirst();
                    }
                    else
                    {
                        _mainContentView.Add(new Label() { Text = $"No songs found in {album.Name}.", X = Pos.Center(), Y = Pos.Center() });
                    }
                    RequestUiRefresh(_mainContentView, true);
                });
            }
            catch (Exception ex)
            {
                InvokeUi(() =>
                {
                    ShowError(_mainContentView, $"Error loading songs: {ex.Message}");
                    RequestUiRefresh(_mainContentView, true);
                });
            }
        });
    }

    // ─────────────────────────────────────────────────────────────────
    // Albums View (Ctrl+L)
    // ─────────────────────────────────────────────────────────────────

    private Task ShowAlbumsViewAsync(bool pushHistory = true)
    {
        if (_mainContentView == null) return Task.CompletedTask;

        if (pushHistory)
            _navigationHistory.Add(() => { _ = ShowAlbumsViewAsync(false); });

        ShowLoading(_mainContentView, "Albums", "Loading all albums...");
        UpdateBreadcrumbs("Albums");

        return Task.Run(async () =>
        {
            try
            {
                var albums = await _subsonicService.GetAllAlbums();
                InvokeUi(() =>
                {
                    _mainContentView.RemoveAll();
                    if (albums?.Count > 0)
                    {
                        var maxNameWidth = albums.Max(a => a.Name.StandardizedStringLength());
                        var maxArtistWidth = albums.Max(a => a.Artist.StandardizedStringLength());

                        _currentAlbumsList = new SakiListView<Album>(album =>
                            $"{album.Name.RunePadRight(maxNameWidth)} :: {album.Artist.RunePadRight(maxArtistWidth)}")
                        {
                            X = 0, Y = 0,
                            Width = Dim.Fill(),
                            Height = Dim.Fill()
                        };

                        _currentAlbumsList.ItemSelected += (sender, selectedAlbum) =>
                        {
                            _ = ShowAlbumSongsFromAlbumsViewAsync(selectedAlbum, true);
                        };

                        RegisterGlobalHotkeys(_currentAlbumsList);
                        _currentAlbumsList.SetDataSource(albums);
                        _mainContentView.Add(_currentAlbumsList);
                        _currentAlbumsList.FocusFirst();
                    }
                    else
                    {
                        _mainContentView.Add(new Label() { Text = "No albums found.", X = Pos.Center(), Y = Pos.Center() });
                    }
                    RequestUiRefresh(_mainContentView, true);
                });
            }
            catch (Exception ex)
            {
                InvokeUi(() =>
                {
                    ShowError(_mainContentView, $"Error loading albums: {ex.Message}");
                    RequestUiRefresh(_mainContentView, true);
                });
            }
        });
    }

    /// <summary>
    /// Drill-down from the all-Albums view into a specific album's songs.
    /// Same browse-and-select behavior as the artist→album→songs flow.
    /// </summary>
    private Task ShowAlbumSongsFromAlbumsViewAsync(Album album, bool pushHistory = true)
    {
        if (_mainContentView == null) return Task.CompletedTask;

        if (pushHistory)
            _navigationHistory.Add(() => { _ = ShowAlbumSongsFromAlbumsViewAsync(album, false); });

        ShowLoading(_mainContentView, $"{album.Name} :: {album.Artist}", $"Loading songs for {album.Name}...");
        UpdateBreadcrumbs("Albums", album.Name, "Songs");

        return Task.Run(async () =>
        {
            try
            {
                var albumDetails = await _subsonicService.GetAlbum(album.Id);
                InvokeUi(() =>
                {
                    _mainContentView.RemoveAll();
                    if (albumDetails?.Song?.Count > 0)
                    {
                        var maxTrackWidth = albumDetails.Song.Max(s => s.Track.ToString().Length);
                        var maxTitleWidth = albumDetails.Song.Max(s => s.Title.StandardizedStringLength());

                        _currentSongsList = new SakiListView<Song>(song =>
                            $"{song.Track.ToString().RunePadLeft(maxTrackWidth)} - {song.Title.RunePadRight(maxTitleWidth)} [{song.Duration.GetAsMMSS()}]")
                        {
                            X = 0, Y = 0,
                            Width = Dim.Fill(),
                            Height = Dim.Fill()
                        };

                        _currentSongsList.ItemSelected += (sender, selectedSong) =>
                        {
                            var trackIndex = albumDetails.Song.IndexOf(selectedSong);
                            _ = _musicPlayerService.PlayAlbum(album.Id, trackIndex);
                        };

                        _currentSongsList.RegisterHotKey(Key.M.WithCtrl, () =>
                        {
                            var selected = _currentSongsList.GetSelectedItem();
                            if (selected != null)
                                _musicPlayerService.AddToCurrentPlaylist(selected);
                        });

                        RegisterGlobalHotkeys(_currentSongsList);
                        _currentSongsList.SetDataSource(albumDetails.Song);
                        _mainContentView.Add(_currentSongsList);
                        _currentSongsList.FocusFirst();
                    }
                    else
                    {
                        _mainContentView.Add(new Label() { Text = $"No songs found in {album.Name}.", X = Pos.Center(), Y = Pos.Center() });
                    }
                    RequestUiRefresh(_mainContentView, true);
                });
            }
            catch (Exception ex)
            {
                InvokeUi(() =>
                {
                    ShowError(_mainContentView, $"Error loading songs: {ex.Message}");
                    RequestUiRefresh(_mainContentView, true);
                });
            }
        });
    }

    // ─────────────────────────────────────────────────────────────────
    // Playlists View (Ctrl+P)
    // ─────────────────────────────────────────────────────────────────

    private Task ShowPlaylistsViewAsync(bool pushHistory = true)
    {
        if (_mainContentView == null) return Task.CompletedTask;

        if (pushHistory)
            _navigationHistory.Add(() => { _ = ShowPlaylistsViewAsync(false); });

        ShowLoading(_mainContentView, "Playlists", "Loading playlists...");
        UpdateBreadcrumbs("Playlists");

        return Task.Run(async () =>
        {
            try
            {
                var playlists = await _subsonicService.GetPlaylists();
                InvokeUi(() =>
                {
                    _mainContentView.RemoveAll();
                    if (playlists?.Count > 0)
                    {
                        var maxNameWidth = playlists.Max(p => p.Name.StandardizedStringLength());
                        var maxOwnerWidth = playlists.Max(p => p.Owner.StandardizedStringLength());

                        var listView = new SakiListView<Playlist>(pl =>
                            $"{pl.Name.RunePadRight(maxNameWidth + 1)} :: {pl.Owner.RunePadRight(maxOwnerWidth + 1)} [lasts {pl.Duration.GetAsMMSS()}]")
                        {
                            X = 0, Y = 0,
                            Width = Dim.Fill(),
                            Height = Dim.Fill()
                        };

                        listView.ItemSelected += (sender, selectedPlaylist) =>
                        {
                            _ = ShowPlaylistDetailViewAsync(selectedPlaylist, true);
                        };

                        RegisterGlobalHotkeys(listView);
                        listView.SetDataSource(playlists);
                        _mainContentView.Add(listView);
                        listView.FocusFirst();
                    }
                    else
                    {
                        _mainContentView.Add(new Label() { Text = "No playlists found.", X = Pos.Center(), Y = Pos.Center() });
                    }
                    RequestUiRefresh(_mainContentView, true);
                });
            }
            catch (Exception ex)
            {
                InvokeUi(() =>
                {
                    ShowError(_mainContentView, $"Error loading playlists: {ex.Message}");
                    RequestUiRefresh(_mainContentView, true);
                });
            }
        });
    }

    private Task ShowPlaylistDetailViewAsync(Playlist playlistInfo, bool pushHistory = true)
    {
        if (_mainContentView == null) return Task.CompletedTask;

        if (pushHistory)
            _navigationHistory.Add(() => { _ = ShowPlaylistDetailViewAsync(playlistInfo, false); });

        ShowLoading(_mainContentView, $"Playlist [{playlistInfo.Name} :: {playlistInfo.Owner}] -- Lasts {playlistInfo.Duration.GetAsMMSS()}", $"Loading playlist {playlistInfo.Name}...");
        UpdateBreadcrumbs("Playlists", playlistInfo.Name);

        return Task.Run(async () =>
        {
            try
            {
                var playlist = await _subsonicService.GetPlaylist(playlistInfo.Id);
                InvokeUi(() =>
                {
                    _mainContentView.RemoveAll();
                    if (playlist?.Entry?.Count > 0)
                    {
                        var maxTitleWidth = playlist.Entry.Max(s => s.Title.StandardizedStringLength());
                        var maxArtistWidth = playlist.Entry.Max(s => s.Artist.StandardizedStringLength());

                        _currentSongsList = new SakiListView<Song>(song =>
                            $"{song.Title.RunePadRight(maxTitleWidth)} :: {song.Artist.RunePadRight(maxArtistWidth)} [{song.Duration.GetAsMMSS()}]")
                        {
                            X = 0, Y = 0,
                            Width = Dim.Fill(),
                            Height = Dim.Fill()
                        };

                        _currentSongsList.ItemSelected += (sender, selectedSong) =>
                        {
                            var index = playlist.Entry.IndexOf(selectedSong);
                            _ = _musicPlayerService.PlayPlaylist(playlist.Id, index);
                        };

                        _currentSongsList.RegisterHotKey(Key.M.WithCtrl, () =>
                        {
                            var selected = _currentSongsList.GetSelectedItem();
                            if (selected != null)
                                _musicPlayerService.AddToCurrentPlaylist(selected);
                        });

                        RegisterGlobalHotkeys(_currentSongsList);
                        _currentSongsList.SetDataSource(playlist.Entry);
                        _mainContentView.Add(_currentSongsList);
                        _currentSongsList.FocusFirst();
                    }
                    else
                    {
                        _mainContentView.Add(new Label() { Text = $"No songs in playlist {playlistInfo.Name}.", X = Pos.Center(), Y = Pos.Center() });
                    }
                    RequestUiRefresh(_mainContentView, true);
                });
            }
            catch (Exception ex)
            {
                InvokeUi(() =>
                {
                    ShowError(_mainContentView, $"Error loading playlist: {ex.Message}");
                    RequestUiRefresh(_mainContentView, true);
                });
            }
        });
    }

    // ─────────────────────────────────────────────────────────────────
    // Search View (Ctrl+R)
    // ─────────────────────────────────────────────────────────────────

    private void ShowSearchView(bool pushHistory = true)
    {
        if (_mainContentView == null) return;

        if (pushHistory)
        {
            _navigationHistory.Add(() => { ShowSearchView(false); });
        }

        _mainContentView.RemoveAll();
        _mainContentView.Title = "Search";
        UpdateBreadcrumbs("Search");
        RequestUiRefresh(_mainContentView, true);

        var searchLabel = new Label()
        {
            Text = "[Search: ?]",
            X = 0, Y = 0,
            Width = 11,
            Height = 1,
            CanFocus = false
        };

        var searchField = new TextField()
        {
            X = Pos.Right(searchLabel) + 1,
            Y = 0,
            Height = 1,
            Width = Dim.Fill()
        };

        var artistsContainer = new FrameView()
        {
            X = 0, Y = 1,
            Width = Dim.Percent(50),
            Height = Dim.Percent(50),
            Title = "Artists",
            CanFocus = true,
            TabStop = TabBehavior.NoStop
        };
        var artistsList = new SakiListView<Artist>(a => a.Name)
        {
            X = 0, Y = 0,
            Width = Dim.Fill(),
            Height = Dim.Fill()
        };
        artistsList.ItemSelected += async (sender, artist) =>
        {
            await ShowArtistAlbumsAsync(artist, true);
        };
        artistsContainer.Add(artistsList);

        var albumsContainer = new FrameView()
        {
            X = Pos.Right(artistsContainer),
            Y = 1,
            Width = Dim.Percent(50),
            Height = Dim.Percent(50),
            Title = "Albums",
            CanFocus = true,
            TabStop = TabBehavior.NoStop
        };
        var albumsList = new SakiListView<Album>(a => $"{a.Artist} :: {a.Name}")
        {
            X = 0, Y = 0,
            Width = Dim.Fill(),
            Height = Dim.Fill()
        };
        albumsList.ItemSelected += async (sender, album) =>
        {
            await _musicPlayerService.PlayAlbum(album.Id, 0);
        };
        albumsContainer.Add(albumsList);

        var songsContainer = new FrameView()
        {
            X = 0,
            Y = Pos.Bottom(artistsContainer),
            Width = Dim.Fill(),
            Height = Dim.Percent(50),
            Title = "Songs",
            CanFocus = true,
            TabStop = TabBehavior.NoStop
        };
        var songsList = new SakiListView<Song>(s => $"{s.Artist} :: {s.Album} :: {s.Title}")
        {
            X = 0, Y = 0,
            Width = Dim.Fill(),
            Height = Dim.Fill()
        };
        songsList.ItemSelected += async (sender, song) =>
        {
            await _musicPlayerService.PlayRadio(song.Id);
        };
        songsContainer.Add(songsList);

        CancellationTokenSource? cts = null;

        searchField.HasFocusChanged += async (sender, e) =>
        {
            // No-op, just needed for focus
        };

        // Use a simple debounce approach for search
        searchField.TextChanged += (sender, e) =>
        {
            var query = searchField.Text?.ToString()?.Trim();
            if (string.IsNullOrEmpty(query))
                return;

            cts?.Cancel();
            cts = new CancellationTokenSource();
            var token = cts.Token;

            _ = Task.Run(async () =>
            {
                try
                {
                    await Task.Delay(300, token);
                    if (token.IsCancellationRequested) return;

                    var result = await _subsonicService.Search(query, 100);
                    if (token.IsCancellationRequested) return;

                    InvokeUi(() =>
                    {
                        searchLabel.Text = "[Search: ?]";

                        if (result.Artists?.Count > 0)
                            artistsList.SetDataSource(result.Artists);

                        if (result.Albums?.Count > 0)
                            albumsList.SetDataSource(result.Albums);

                        if (result.Songs?.Count > 0)
                            songsList.SetDataSource(result.Songs);

                        RequestUiRefresh(_mainContentView, true);
                    });
                }
                catch (OperationCanceledException) { }
                catch { }
            });

            searchLabel.Text = "[Search: /]";
        };

        _mainContentView.Add(searchLabel, searchField,
            artistsContainer, albumsContainer, songsContainer);
        searchField.SetFocus();
        RequestUiRefresh(_mainContentView, true);
    }

    // ─────────────────────────────────────────────────────────────────
    // Player Controls Updates
    // ─────────────────────────────────────────────────────────────────

    private void OnCurrentStateChanged(object? sender, CurrentStateChangedEventArgs e)
    {
        InvokeUi(() =>
        {
            UpdatePlayerControls(e.CurrentState);
            UpdatePlaylistView(e.CurrentState);
            UpdateRepeatShuffleDisplay(e.CurrentState);
            RequestUiRefresh(this, false);
        });
    }

    private void OnTimeChanged(object? sender, TimeChangedEventArgs e)
    {
        InvokeUi(() =>
        {
            UpdateTimeDisplay(e.Time);
            RequestUiRefresh(_playerControlView, false);
        });
    }

    private void OnVolumeChanged(object? sender, VolumeChangedEventArgs e)
    {
        InvokeUi(() =>
        {
            UpdateVolumeDisplay(e.Volume);
            RequestUiRefresh(_playerControlView, false);
        });
    }

    private void UpdatePlayerControls(CurrentState state)
    {
        if (_playerControlView == null) return;

        _playerControlView.RemoveAll();

        var statusText = state.IsPlaying ? "Now Playing" :
                        state.Stopped ? "Stopped" : "Paused";

        if (state.CurrentTrack == null)
            statusText = "Idle";

        _playerControlView.Title = statusText;

        if (state.CurrentTrack != null)
        {
            var trackLabel = new Label()
            {
                Text = $"{state.CurrentTrack.Artist} :: {state.CurrentTrack.Album} :: {state.CurrentTrack.Title}",
                X = 0, Y = 0,
                Width = Dim.Percent(60),
                Height = 1,
                CanFocus = false
            };

            _volumeLabel = new Label()
            {
                Text = "Vol[C-k/i]",
                X = Pos.Right(trackLabel),
                Y = 0,
                Width = 10,
                Height = 1,
                CanFocus = false
            };

            _volumeProgress = new ProgressBar()
            {
                X = Pos.Right(_volumeLabel) + 1,
                Y = 0,
                Width = Dim.Fill(5),
                Height = 1,
                ProgressBarFormat = ProgressBarFormat.SimplePlusPercentage,
                ProgressBarStyle = ProgressBarStyle.Blocks,
                CanFocus = false,
                Fraction = _musicPlayerService.Volume
            };

            _volumePercentLabel = new Label()
            {
                Text = $"{(int)(_musicPlayerService.Volume * 100)}%",
                X = Pos.AnchorEnd(5),
                Y = 0,
                Width = 4,
                Height = 1,
                CanFocus = false
            };

            _timeElapsedLabel = new Label()
            {
                Text = "00:00",
                X = 0, Y = 1,
                Width = Dim.Fill(),
                Height = 1,
                CanFocus = false
            };

            _playingTimeProgress = new ProgressBar()
            {
                X = 0, Y = 2,
                Width = Dim.Fill(6),
                Height = 1,
                ProgressBarFormat = ProgressBarFormat.Simple,
                ProgressBarStyle = ProgressBarStyle.Blocks,
                CanFocus = false,
                Fraction = 0
            };

            _songDurationLabel = new Label()
            {
                Text = state.CurrentTrack.Duration.GetAsMMSS(),
                X = Pos.AnchorEnd(6),
                Y = 1,
                Width = 5,
                Height = 1,
                CanFocus = false
            };

            _playerControlView.Add(trackLabel, _volumeLabel, _volumeProgress, _volumePercentLabel,
                _timeElapsedLabel, _playingTimeProgress, _songDurationLabel);
        }
        else
        {
            SetupIdlePlayerControls();
        }
    }

    private void UpdatePlaylistView(CurrentState state)
    {
        if (_playlistView == null) return;

        _playlistView.RemoveAll();

        if (state.CurrentPlaylist?.Entry != null && state.CurrentPlaylist.Entry.Count > 0)
        {
            _currentPlaylistView = new SakiListView<Song>(song =>
            {
                var index = state.CurrentPlaylist.Entry.IndexOf(song);
                var isCurrentTrack = (state.CurrentTrackIndex == index) ||
                                   (state.CurrentTrack != null && state.CurrentTrack.Id == song.Id);
                var currentMarker = isCurrentTrack ? "*" : " ";

                // Truncate title to fit the sidebar width
                var maxTitleLen = 25;
                string title;
                if (song.Title.Length > maxTitleLen)
                    title = song.Title.Substring(0, maxTitleLen);
                else
                    title = song.Title.RunePadRight(maxTitleLen);

                return $"{currentMarker}{title}[{song.Duration.GetAsMMSS()}]";
            })
            {
                X = 0, Y = 0,
                Width = Dim.Fill(),
                Height = Dim.Fill()
            };

            _currentPlaylistView.SetDataSource(state.CurrentPlaylist.Entry);

            // Auto-scroll to current track
            if (state.CurrentTrackIndex >= 0)
            {
                _currentPlaylistView.ScrollToItem(state.CurrentTrackIndex);
            }

            RegisterGlobalHotkeys(_currentPlaylistView, isPlaylist: true);

            _currentPlaylistView.SetOnLeave(lv =>
            {
                if (lv?.Items != null && lv.Items.Count > 0 && state.CurrentTrackIndex >= 0)
                {
                    lv.ScrollToItem(state.CurrentTrackIndex);
                }
            });

            _currentPlaylistView.ItemSelected += (sender, selectedSong) =>
            {
                var selectedIndex = state.CurrentPlaylist.Entry.IndexOf(selectedSong);
                if (selectedIndex >= 0)
                {
                    _ = Task.Run(async () =>
                    {
                        try
                        {
                            await _musicPlayerService.PlayAlbum(selectedSong.AlbumId, selectedIndex);
                        }
                        catch (Exception ex)
                        {
                            InvokeUi(() =>
                            {
                                MessageBox.ErrorQuery("Playback Error", $"Failed to play track: {ex.Message}", "OK");
                            });
                        }
                    });
                }
            };

            _playlistView.Add(_currentPlaylistView);
        }
        else
        {
            _playlistView.Add(new Label()
            {
                Text = "No playlist loaded",
                X = 0, Y = 0,
                CanFocus = false
            });
        }
    }

    private void UpdateTimeDisplay(long timeMs)
    {
        if (_timeElapsedLabel != null && _playingTimeProgress != null)
        {
            var currentState = _musicPlayerService.GetCurrentState();
            if (currentState?.CurrentTrack != null)
            {
                var elapsedSeconds = (int)(timeMs / 1000);
                _timeElapsedLabel.Text = elapsedSeconds.GetAsMMSS();

                if (currentState.CurrentTrack.Duration > 0)
                {
                    var fraction = (float)Math.Round(((double)timeMs / 1000) / currentState.CurrentTrack.Duration, 3);
                    _playingTimeProgress.Fraction = Math.Max(0, Math.Min(1, fraction));
                }
            }
        }
    }

    private void UpdateVolumeDisplay(float volume)
    {
        if (_volumeProgress != null && _volumePercentLabel != null)
        {
            _volumeProgress.Fraction = volume;
            _volumePercentLabel.Text = $"{(int)(volume * 100)}%";
        }
    }

    private void UpdateRepeatShuffleDisplay(CurrentState state)
    {
        if (_repeatLabel != null)
        {
            _repeatLabel.Text = state.RepeatStatus switch
            {
                RepeatStatus.RepeatAll => "Repeat: All",
                RepeatStatus.RepeatOne => "Repeat: One",
                _ => "Repeat: Off"
            };
        }

        if (_shuffleLabel != null)
        {
            _shuffleLabel.Text = state.IsShuffled ? " Shuffle: On" : "Shuffle: Off";
        }
    }

    // ─────────────────────────────────────────────────────────────────
    // Utility
    // ─────────────────────────────────────────────────────────────────

    private void ShowLoading(FrameView container, string title, string message)
    {
        container.RemoveAll();
        container.Title = title;
        container.Add(new Label() { Text = message, X = Pos.Center(), Y = Pos.Center() });
        RequestUiRefresh(container, true);
    }

    private void ShowError(FrameView? container, string message)
    {
        if (container == null) return;
        container.RemoveAll();
        container.Add(new Label()
        {
            Text = message,
            X = Pos.Center(),
            Y = Pos.Center()
        });
        RequestUiRefresh(container, true);
    }
}
