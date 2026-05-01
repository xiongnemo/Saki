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

## System Page Tabs and Endpoint Identity Validation

Status: completed.

- [x] Rename the Settings surface to a tabbed System page with About, Settings, and Properties views.
- [x] Move Endpoint Ping to a full-width bottom diagnostics panel.
- [x] Add library fingerprint validation before saving multiple endpoints.
- [x] Add About and read-only runtime Properties content.
- [x] Verify layout, navigation, validation, and existing regressions.

## System Popup Settings Editing

Status: completed.

- [x] Replace inline Settings form editing with a selectable settings list.
- [x] Open an edit or choice popup with Enter for each setting.
- [x] Keep Endpoint Ping refreshing live while the endpoint popup text changes.
- [x] Merge Properties into About and reduce System tabs to About and Settings.
- [x] Add Left/Right tab switching between the two System tabs.
- [x] Check README and verify with focused UI tests plus `go test ./...`.

## System Popup Modal and About Polish

Status: completed.

- [x] Make System edit popups modal: ESC closes them and global navigation keys do not leak to lists/Queue.
- [x] Replace Save/Back list rows with button-style controls in the Settings content.
- [x] Keep save/ping messages compact so endpoint rows remain visible.
- [x] Remove duplicate About fields and add useful static technical/runtime information.
- [x] Update About wording with requested emoji and product expansion text.
- [x] Verify with UI tests and `go test ./...`, then commit.

## System Popup and About Alignment Follow-Up

Status: completed.

- [x] Route all popup key/mouse events to the popup and prevent leakage to outer lists.
- [x] Replace one-line text popup with a larger TextArea popup.
- [x] Draw Settings rows with aligned themed labels and values.
- [x] Draw About as intro plus Resolved Config and Media Support panels with aligned labels.
- [x] Expose active audio backend and actual cover renderer status.
- [x] Verify with focused UI/audio tests and `go test ./...`, then commit.

## System About Host/Media Layout and Search Filter Activation

Status: completed.

- [x] Make System edit popup Esc close work even when the popup page owns focus.
- [x] Move OS/Arch into the About intro with hostname when available.
- [x] Move Active backend into Resolved Config and remove Configured backend.
- [x] Render Media Support as label plus one wrapped value per line to avoid clipping long values.
- [x] Fix `/` search filter arrow-key navigation while the filter input is focused.
- [x] Make filter Enter trigger the selected original entry action instead of only jumping to it.
- [x] Verify with focused UI tests and `go test ./...`, then commit.

## Fullscreen Now Playing Overlay

Status: completed.

- [x] Add a fullscreen Now Playing overlay with large cover art, track metadata, progress, playback controls, and stream/cache/audio status.
- [x] Add a global shortcut to open and close Now Playing while keeping existing playback shortcuts unchanged.
- [x] Keep the existing bottom Playing panel as the compact global status surface.
- [x] Keep the fullscreen overlay playback-focused by omitting queue context.
- [x] Verify responsive rendering, overlay close/return behavior, and navigation with focused UI tests and `go test ./...`.

## Now Playing Style Variants

Status: completed.

- [x] Remove explanatory Controls text from the Now Playing overlay while preserving global keyboard shortcuts.
- [x] Auto-select between side-cover and top-cover Now Playing layouts based on terminal shape.
- [x] Show a clear terminal ratio hint when neither layout fits.
- [x] Emphasize the title and render `Artist - Album` metadata with horizontal scrolling when needed.
- [x] Add clickable Prev, Play/Pause, Next, Repeat, and Shuffle buttons with current state labels.
- [x] Verify layout selection, metadata scrolling, mouse buttons, and existing shortcuts with focused UI tests and `go test ./...`.

## Now Playing Polish Fix

Status: completed.

- [x] Restore the main-screen Controls bar and keep it to two non-wrapping rows through window resizes.
- [x] Keep side-cover Now Playing layouts cover-only on the left and move text, buttons, and Stream/Audio status to the right info area.
- [x] Render playback controls as bordered clickable button blocks in both Now Playing layouts.
- [x] Expand top-cover Now Playing cover art to use the available width and keep buttons below progress.
- [x] Verify controls resize behavior, side/top layout placement, cover width, and button clicks with focused UI tests and `go test ./...`.

## Now Playing Button Redesign

Status: completed.

- [x] Replace the bordered ASCII-style Now Playing controls with filled button labels matching the Settings Save button style.
- [x] Lay out Prev, Play/Pause, and Next as a centered first row, with Repeat and Shuffle as a centered second row.
- [x] Keep Repeat and Shuffle labels stateful while using compact labels only when width requires it.
- [x] Verify button rows, hitboxes, and state labels with focused UI tests and `go test ./...`.

## Now Playing Mouse Click Fix

Status: completed.

- [x] Preserve Now Playing overlay mouse move/down/up events through the global capture layer so `tview` can synthesize click actions.
- [x] Keep actual playback actions bound to button click hitboxes.
- [x] Add regression coverage for the overlay capture sequence and run `go test ./...`.

## Now Playing Side Status Alignment

Status: completed.

- [x] Center the side-cover Now Playing stream/cache/audio status inside the right info area.
- [x] Add focused UI coverage for the centered status placement and run `go test ./...`.

## Now Playing Top Cover Aspect

Status: completed.

- [x] Size the vertical/top-cover Now Playing cover from the image aspect ratio before filling extra width.
- [x] Keep enough rows for title, metadata, progress, two-row buttons, and status.
- [x] Add focused UI coverage for square and wide cover aspect behavior and run `go test ./...`.

## Headless Linux Playback Freeze

Status: completed.

- [x] Avoid holding `MiniAudioBackend.mu` while starting, stopping, or uninitializing miniaudio devices.
- [x] Move UI-triggered playback commands off the TUI event loop and report errors safely.
- [x] Fall back from miniaudio `Play()` errors to mpv in auto mode after a successful load.
- [x] Verify with focused tests, `go test ./...`, and a remote PipeWire playback check on `192.168.1.120`.

## Cross-Terminal Volume Shortcuts

Status: completed.

- [x] Add `Ctrl+Up` and `Ctrl+Down` as reliable volume shortcuts for terminals where `Ctrl+I` is indistinguishable from Tab.
- [x] Keep `Ctrl+I` and `Ctrl+K` as compatibility aliases.
- [x] Update Controls help text and focused shortcut tests, then run `go test ./...`.

## TTY Volume Shortcut Follow-Up

Status: completed.

- [x] Replace volume shortcuts with TTY-safe `-` for down and `=` for up.
- [x] Remove previous `Ctrl+Up`/`Ctrl+Down` and `Ctrl+I`/`Ctrl+K` compatibility aliases.
- [x] Update Controls help text and focused shortcut tests, then run `go test ./...`.

## TTY-Safe Global Bindings

Status: completed.

- [x] Replace Ctrl-based view/playback bindings with text-input-safe rune bindings.
- [x] Use `;` for previous track and `'` for next track, with `,`/`.` for seek.
- [x] Replace `Ctrl+M` add-to-queue with a TTY-safe rune binding.
- [x] Update Controls help text and focused input-routing tests, then run `go test ./...`.

## Fine Volume Shortcuts

Status: completed.

- [x] Add `[` and `]` as TTY-safe 1% volume down/up shortcuts.
- [x] Keep `[` and `]` available for System tab switching and text input.
- [x] Update Controls help text and focused shortcut tests, then run `go test ./...`.
