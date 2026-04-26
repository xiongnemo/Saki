//go:build windows

package mediaintegration

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	"github.com/xiongnemo/saki/internal/models"
)

const smtcDLLName = "saki_smtc.dll"

type windowsIntegration struct {
	dll      *syscall.LazyDLL
	dllPath  string
	dllErr   error
	init     *syscall.LazyProc
	update   *syscall.LazyProc
	setState *syscall.LazyProc
	poll     *syscall.LazyProc
	close    *syscall.LazyProc
	disabled bool
}

func New() MediaIntegration {
	dllPath, dllErr := findSMTCDLL()
	dll := syscall.NewLazyDLL(dllPath)
	return &windowsIntegration{
		dll:      dll,
		dllPath:  dllPath,
		dllErr:   dllErr,
		init:     dll.NewProc("saki_smtc_init"),
		update:   dll.NewProc("saki_smtc_update_now_playing"),
		setState: dll.NewProc("saki_smtc_set_playback_state"),
		poll:     dll.NewProc("saki_smtc_poll_command"),
		close:    dll.NewProc("saki_smtc_close"),
	}
}

func findSMTCDLL() (string, error) {
	var embeddedErr error
	if embeddedPath, err := extractEmbeddedSMTCDLL(); err == nil {
		return embeddedPath, nil
	} else {
		embeddedErr = err
	}

	candidates := make([]string, 0, 4)
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), smtcDLLName))
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(cwd, smtcDLLName),
			filepath.Join(cwd, "internal", "mediaintegration", "smtc_shim", "windows", smtcDLLName),
		)
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, embeddedErr
		}
	}
	return smtcDLLName, embeddedErr
}

func extractEmbeddedSMTCDLL() (string, error) {
	if len(embeddedSMTCDLL) == 0 {
		return "", fmt.Errorf("embedded %s is empty", smtcDLLName)
	}
	cacheRoot, err := os.UserCacheDir()
	if err != nil || cacheRoot == "" {
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			cacheRoot = localAppData
		} else if err != nil {
			return "", err
		} else {
			return "", fmt.Errorf("user cache directory is unavailable")
		}
	}
	dir := filepath.Join(cacheRoot, "Saki", "smtc")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, smtcDLLName)
	if current, err := os.ReadFile(path); err == nil && bytes.Equal(current, embeddedSMTCDLL) {
		return path, nil
	}
	if err := os.WriteFile(path, embeddedSMTCDLL, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func (w *windowsIntegration) Init() error {
	if err := w.dll.Load(); err != nil {
		w.disabled = true
		if w.dllErr != nil {
			return fmt.Errorf("extract embedded %s: %v; load %s: %w", smtcDLLName, w.dllErr, w.dllPath, err)
		}
		return fmt.Errorf("load %s: %w", w.dllPath, err)
	}
	ret, _, _ := w.init.Call()
	if ret != 0 {
		w.disabled = true
		return fmt.Errorf("initialize SMTC: error %d", ret)
	}
	return nil
}

func (w *windowsIntegration) UpdateNowPlaying(song models.Song) error {
	if w.disabled {
		return nil
	}
	title, err := syscall.UTF16PtrFromString(song.Title)
	if err != nil {
		return err
	}
	artist, err := syscall.UTF16PtrFromString(song.Artist)
	if err != nil {
		return err
	}
	album, err := syscall.UTF16PtrFromString(song.Album)
	if err != nil {
		return err
	}
	thumb, err := syscall.UTF16PtrFromString(song.Image)
	if err != nil {
		return err
	}
	ret, _, _ := w.update.Call(
		uintptr(unsafe.Pointer(title)),
		uintptr(unsafe.Pointer(artist)),
		uintptr(unsafe.Pointer(album)),
		uintptr(unsafe.Pointer(thumb)),
	)
	if ret != 0 {
		return fmt.Errorf("update SMTC metadata: error %d", ret)
	}
	return nil
}

func (w *windowsIntegration) SetPlaybackState(state models.PlaybackState) error {
	if w.disabled {
		return nil
	}
	ret, _, _ := w.setState.Call(uintptr(state))
	if ret != 0 {
		return fmt.Errorf("set SMTC playback state: error %d", ret)
	}
	return nil
}

func (w *windowsIntegration) PollCommand() Command {
	if w.disabled {
		return CommandNone
	}
	ret, _, _ := w.poll.Call()
	return Command(ret)
}

func (w *windowsIntegration) Close() error {
	if w.disabled {
		return nil
	}
	w.close.Call()
	return nil
}
