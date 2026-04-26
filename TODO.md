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
