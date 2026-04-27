# TODO

## Windows SMTC Release Packaging

Status: completed.

- [x] Embed the Windows SMTC shim DLL into the Windows Go executable with a Windows-only `go:embed` source.
- [x] Keep the C++/WinRT shim behind Windows-only build tags so Linux/macOS builds do not require MSVC or Windows SDK.
- [x] At runtime on Windows, extract the embedded DLL to `%LOCALAPPDATA%\Saki\smtc\saki_smtc.dll`, then load it from that stable cache path.
- [x] Keep non-Windows media integration as no-op until Linux MPRIS or macOS Now Playing support is implemented.
- [x] Add GitHub Actions release builds using native OS runners: `ubuntu-latest`, `macos-latest`, and `windows-latest`.
- [x] Publish the built archives to a GitHub Release.
- [x] On `windows-latest`, build `internal/mediaintegration/smtc_shim/windows/saki_smtc.dll` with MSVC/Windows SDK before `go build`.
- [x] Do not require MSVC/`cl.exe` for non-Windows builds.
- [x] Verify with `go test ./...` and `.\scripts\build-windows.ps1 -SkipTests -SkipSMTC`.

## Versioned Dev Prerelease Builds

Status: completed.

- [x] Add a single version string source for CLI, Settings, local builds, and CI builds.
- [x] Support `--version` and `-v`.
- [x] Inject Git branch, 12-character commit hash, and dirty state during builds.
- [x] Build Linux, Windows, and macOS artifacts for release architectures.
- [x] Publish `dev` branch builds as GitHub prereleases.
- [x] Verify with `go test ./...` and targeted build checks.

## ALAC Decoder Experiment

Status: completed.

- [x] Add `github.com/mycophonic/saprobe-alac` as the ALAC decoder dependency.
- [x] Implement an ALAC `pcmSource` for local and completed-cache M4A/MP4 files.
- [x] Keep non-seekable HTTP ALAC streams on the existing mpv fallback path.
- [x] Verify PCM conversion, source detection, and optional real ALAC decode with ffmpeg when available.

## MPV Range Playback Cache

Status: completed.

- [x] Start a background completed-file cache download when mpv playback uses HTTP Range requests.
- [x] Avoid spawning duplicate cache downloads for repeated Range requests on the same track.
- [x] Verify that Range playback can still return partial content while the full cache lands on disk.

## ALAC First-Class Support Follow-Up

Status: completed.

- [x] Surface completed audio cache state in Playing as `Cache OK` instead of `Download --%`.
- [x] Keep `Download NN%` only for known active download/buffer progress.
- [x] Stop current playback when the audio backend setting changes.
- [x] Defensively stop the old concrete backend before switching to another backend.
- [x] Verify ALAC/M4A completed-cache playback, cache status labels, and backend switching with tests.

## ALAC/M4A Range Streaming

Status: completed.

- [x] Add a Range-backed `io.ReadSeeker` for HTTP audio sources.
- [x] Use `saprobe-alac` with the Range-backed reader for uncached HTTP ALAC/M4A playback.
- [x] Keep completed cache as the preferred path and retain mpv fallback only when Range streaming is unavailable.
- [x] Update README runtime/backend wording for first-class ALAC/M4A streaming.
- [x] Verify Range reader behavior, ALAC streaming decode/seek, and existing regressions with tests.

## Playing Audio Format Badge

Status: completed.

- [x] Map Subsonic/OpenSubsonic audio metadata onto songs.
- [x] Surface decoder format metadata from miniaudio sources through player state.
- [x] Render a compact codec/bit-depth/sample-rate/bitrate badge on the Playing status row.
- [x] Keep the status row responsive so the badge does not overlap repeat/shuffle or volume controls.
- [x] Verify with focused unit tests and `go test ./...`.

## Commit-Derived Patch Versions

Status: completed.

- [x] Compute build base version from the nearest `vMAJOR.MINOR.PATCH` tag plus commit count.
- [x] Use the computed base version in GitHub Actions artifacts and prerelease tags.
- [x] Use the same computed base version in `scripts/build-windows.ps1` when no explicit version env is provided.
- [x] Keep explicit `SAKI_BASE_VERSION` / `BASE_VERSION` overrides available for manual builds.
- [x] Verify version formatting and existing tests.

## Playing Format Order and MP3 Speed Check

Status: completed.

- [x] Show sample rate before bit depth in the Playing audio format badge.
- [x] Inspect MP3 decode channel/frame-size assumptions for half-speed playback cases.
- [x] Add focused tests for the badge label and any decoder fix.
- [x] Verify with `go test ./...`.

## Main UI Mouse and Controls Polish

Status: completed.

- [x] Make list double-click activation require the same prior clicked item.
- [x] Make mouse wheel scrolling move the list selection with the viewport.
- [x] Split Controls into view/navigation shortcuts and playback shortcuts.
- [x] Keep Playing and Controls mouse-passive so they do not take focus.
- [x] Verify with focused UI tests and `go test ./...`.

## Settings Responsive Layout and Endpoint Ping

Status: completed.

- [x] Replace the fixed-width Settings form layout with a responsive Settings container.
- [x] Keep Settings editable after Save and show save/ping status inline.
- [x] Add a read-only Subsonic endpoint probe API with a 5s UI timeout.
- [x] Keep Settings Tab/Shift+Tab navigation inside the form instead of jumping to Queue.
- [x] Verify small-window layout, keyboard navigation, endpoint probing, and existing regressions.
