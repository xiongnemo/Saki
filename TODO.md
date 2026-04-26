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
