# Saki

<!-- Add to your README -->
[![AI Slop Inside](https://sladge.net/badge.svg)](https://sladge.net)

> [!NOTE]  
> This project actively uses GPT 5.5 (Thinking) on Codex to refactor legacy code and ships new functionality. Due to the nature of Terminal GUI application some GUI may not be tested.

> [!IMPORTANT]  
> This project is in active development. It's still in an unstable state and is not ready for production use.

> [!WARNING]  
> Proceed with your own risk. This project is not fully tested and has known bugs. While the author is working on it, you are also welcome to submit issues and feature requests.

## What Is Saki?

Saki is a cross-platform Subsonic Audio Klient for Individuals. It's a terminal-based music player for Subsonic-compatible music servers, a successor of [SonicLair.Cli](https://github.com/xiongnemo/SonicLair.Cli) and derived from the CLI version of [SonicLair.Net](https://github.com/thelinkin3000/SonicLair.NET), but rewritten in Go with a TUI built on `tview` and `tcell`.

## Features

- Browse Subsonic-compatible libraries by artists, albums, and playlists.
- Global search across artists, albums, and songs.
- In-view filtering with `/` for supported lists.
- Queue navigation with keyboard focus, mouse selection, and double-click playback.
- Queue-side album cover preview with terminal image protocol support and color-block fallback.
- Now Playing panel with progress, stream/cache state, audio format details, repeat, shuffle, volume controls, and a non-wrapping Controls help bar.
- Help overlay via `?` / `？` and searchable Command Palette via `:` / `：` for global navigation, playback, system, and quit actions.
- Fullscreen Now Playing overlay via `5` with auto-selected cover layouts, scrolling metadata, progress, status, and two-row clickable playback controls.
- Multiple playback backends: default `auto` tries miniaudio first for MP3/WAV/FLAC and Range-capable or completed-cache ALAC/M4A, then falls back to optional `mpv` for unsupported sources.
- Local stream proxy with playback-time buffering and completed-file audio cache.
- Seeking support when the current backend/source can provide a seekable stream or cached file.
- System page with About/runtime properties, popup-based Settings, endpoint validation, and live endpoint ping/status.
- Offline library backed by a local SQLite catalog. Saki can switch to the
  catalog automatically after an online failure, or you can use `O` for
  Offline and `o` to Retry Online. Press `u` to refresh the catalog while
  online.
- Selective downloads for tracks, albums, and playlists: `d` downloads the
  selected/current track and `D` downloads every track in an album or playlist.
  `x` removes the current track's local variants. Stream and raw variants,
  resumable partial files, configurable concurrency, and an independent
  offline storage limit are available in System settings. The offline limit
  defaults to unlimited (`0`), while downloads use two workers by default.
- Song lists show `[Cached]` and `[Downloaded]` in English, or `[已缓存]` and
  `[已下载]` in Chinese, for local playback state; the labels remain visible
  when terminal colors are unavailable.
- UI language can be selected as Auto, English, or 中文 in System settings.
- A persistent status line reports download, cache, sync, and connection events;
  song list markers refresh when local files finish.
- Windows SMTC integration through a small C++/WinRT DLL shim.
- Versioned builds and `--version` / `-v` output with branch, commit, and dirty state.

## Screenshots

### Overview

![1777182897892](image/README/1777182897892.png)

### Global Search

![1777182987570](image/README/1777182987570.png)

### In-view Search

![1777183020379](image/README/1777183020379.png)

### Windows SMTC Integration

![1777182847750](image/README/1777182847750.png)

### About

![1777344343416](image/README/1777344343416.png)

### Now Playing

#### Landscape Mode

![1777444805470](image/README/1777444805470.png)

#### Vertical Mode

![1777444711641](image/README/1777444711641.png)

#### Prompting when terminal shape is unusable 

![1777444840213](image/README/1777444840213.png)

## Run

```pwsh
$env:GOPROXY = 'https://goproxy.cn,direct'
go run ./cmd/saki
```

Print the build version without starting the TUI:

```pwsh
go run ./cmd/saki --version
```

Install a built Saki binary under the per-user command directory:

```text
saki install
saki install --dir <directory> --yes
```

The default directory is `~/.local/bin` on every platform. The installed
executable is always named `saki` on Unix-like systems and `saki.exe` on
Windows. The command previews the source, destination, and PATH status, asks
before creating a missing directory or replacing different contents, and is
content-idempotent. It only prints PATH or shell hints; it never edits PATH,
shell profiles, or registry settings. `go run ./cmd/saki install` installs the
temporary executable produced by `go run`, so use a release or locally built
binary for normal installation.

For local playback, install `mpv` so `mpv.exe` is on `PATH`, or set the full
`mpv.exe` path in the System page if you want to use the optional mpv
backend. System is available from the main screen with `6` or the Command
Palette.

## Build On Windows

```pwsh
$env:GOPROXY = 'https://goproxy.cn,direct'
.\scripts\build-windows.ps1
.\dist\saki-windows-amd64\saki.exe
```

The build script builds or reuses the Windows SMTC shim with MSVC/Windows SDK,
runs Go tests, embeds `saki_smtc.dll` into `saki.exe`, and then builds the Go
executable. The default miniaudio backend uses cgo, so Windows builds also need
a working MinGW-w64/GCC toolchain. If you already have a built shim and only
want to skip rebuilding it, pass `-SkipSMTC`.

`mpv` is optional. Install it or set its path in System only when using the
`mpv` backend or the `auto` fallback for non-seekable or unsupported sources.

## Runtime Requirements

The default miniaudio backend has no external runtime player dependency for
MP3/WAV/FLAC and Range-capable or completed-cache ALAC/M4A files. The optional
mpv backend starts mpv with JSON IPC and feeds it local proxy URLs, and remains
available as a fallback for unsupported or non-Range-capable sources.

## Release Builds

Pushes to `dev` build prerelease artifacts for Linux, Windows, and macOS, and
pushing an exact `vMAJOR.MINOR.PATCH` tag publishes that version as a release. The
workflow targets `amd64`, `arm64`, and `i686` for Linux and Windows, and
`amd64` and `arm64` for macOS. Artifacts are published to a GitHub prerelease
named like `v0.0.1-dev-06b7a4654205`.

The Linux builds also ship as packages that pull in ALSA for audio: `.deb` and
`.rpm` for the glibc builds (`amd64`, `arm64`, `i686`), and `.apk` for the
`linux-musl` builds (`amd64`, `arm64`). Alpine and other musl systems need the
musl builds; the regular Linux builds link glibc and do not start there.

```sh
sudo apt install ./saki-v0.0.63-dev-<commit>-linux-amd64.deb
sudo dnf install ./saki-v0.0.63-dev-<commit>-linux-amd64.rpm
apk add --allow-untrusted ./saki-v0.0.63-dev-<commit>-linux-musl-amd64.apk
```

That build installs as version `0.0.63~pre-1` (deb, rpm) or `0.0.63_pre-r1`
(apk): a package version has no room for a branch and commit, so dev builds are
marked as a prerelease of their base version, ordered before a later `v0.0.63`
tag, and the full build version is in the package description.

Each build embeds version metadata in this format:

```text
v{major}.{minor}.{patch}-{branch}-{commit12}[-dirty]
```

For CI and build-script releases, `patch` is derived from Git history: the
nearest exact `vMAJOR.MINOR.PATCH` tag plus the number of commits since that
tag. If no exact release tag exists, builds use `v0.0.1` as the fallback base
and add the current commit count.

To bump `major` or `minor`, create an exact semver tag on the desired base
commit, for example `v0.1.0`. The tag's own build is the release and reports
plain `v0.1.0` (packages `0.1.0-1` / `0.1.0-r1`); CI refuses a tag that is not
the version its commit computes. Dev builds of that commit are `v0.1.0-dev-...`,
and the next commit builds as `v0.1.1-...`. Dev prerelease tags like
`v0.0.1-dev-06b7a4654205` are ignored for the patch calculation.

## Windows SMTC Shim

Windows builds embed `saki_smtc.dll` into the Go executable. At runtime, Saki
extracts the embedded shim to `%LOCALAPPDATA%\Saki\smtc\saki_smtc.dll` and
loads it from that stable cache path.

During development, `go run ./cmd/saki` can start without a compiled shim, but
Windows SMTC stays disabled unless `saki_smtc.dll` is available beside the
executable, in the working directory, under
`internal/mediaintegration/smtc_shim/windows`, or on `PATH`. Release builds use
`scripts/build-windows.ps1`, which builds or reuses the shim and compiles with
the `saki_embed_smtc` tag so the DLL is embedded into `saki.exe`.

## Libraries and Frameworks

- [Go](https://go.dev/) for the main application.
- [C++/WinRT](https://learn.microsoft.com/windows/uwp/cpp-and-winrt-apis/) for the Windows SMTC shim.
- [PowerShell](https://learn.microsoft.com/powershell/) for Windows build scripting.
- [GitHub Actions](https://github.com/features/actions) for prerelease builds.
- [tview](https://github.com/rivo/tview) and [tcell](https://github.com/gdamore/tcell) for the terminal UI.
- [malgo](https://github.com/gen2brain/malgo) with [miniaudio](https://github.com/mackron/miniaudio) for the default native audio backend.
- [mpv](https://mpv.io/) as an optional external playback backend and fallback.
- [go-mp3](https://github.com/hajimehoshi/go-mp3) for MP3 decoding.
- [mewkiz/flac](https://github.com/mewkiz/flac) for FLAC decoding.
- [saprobe-alac](https://github.com/mycophonic/saprobe-alac) for ALAC/M4A decoding.
- [modernc.org/sqlite](https://modernc.org/sqlite) for the pure-Go offline catalog.
- [rasterm](https://github.com/BourgeoisBear/rasterm) for terminal image protocol encoding.
- [go-winio](https://github.com/microsoft/go-winio) for Windows named pipe support.
