package audio

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xiongnemo/saki/internal/models"
)

type MPVBackend struct {
	settings models.Settings

	mu       sync.RWMutex
	cmd      *exec.Cmd
	conn     net.Conn
	ipcPath  string
	seq      atomic.Int64
	events   chan Event
	done     chan struct{}
	position float64
	duration float64
	paused   bool
	loaded   bool
	volume   float64
}

func NewMPVBackend(settings models.Settings) *MPVBackend {
	return &MPVBackend{
		settings: settings,
		events:   make(chan Event, 64),
		done:     make(chan struct{}),
		paused:   true,
		volume:   1,
	}
}

func (b *MPVBackend) SetSettings(settings models.Settings) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.settings = settings
}

func (b *MPVBackend) Load(ctx context.Context, request LoadRequest) error {
	if err := b.ensureStarted(ctx); err != nil {
		return err
	}
	if err := b.command("loadfile", request.URI, "replace"); err != nil {
		return err
	}
	b.mu.Lock()
	b.loaded = true
	b.paused = false
	b.position = 0
	b.duration = 0
	b.mu.Unlock()
	return nil
}

func (b *MPVBackend) Play() error {
	if err := b.ensureStarted(context.Background()); err != nil {
		return err
	}
	if err := b.setProperty("pause", false); err != nil {
		return err
	}
	b.mu.Lock()
	b.paused = false
	b.mu.Unlock()
	return nil
}

func (b *MPVBackend) Pause() error {
	if err := b.setProperty("pause", true); err != nil {
		return err
	}
	b.mu.Lock()
	b.paused = true
	b.mu.Unlock()
	return nil
}

func (b *MPVBackend) Stop() error {
	if err := b.command("stop"); err != nil {
		return err
	}
	b.mu.Lock()
	b.loaded = false
	b.paused = true
	b.position = 0
	b.duration = 0
	b.mu.Unlock()
	return nil
}

func (b *MPVBackend) Seek(position float64) error {
	b.mu.RLock()
	duration := b.duration
	b.mu.RUnlock()
	if duration <= 0 {
		return nil
	}
	if position < 0 {
		position = 0
	}
	if position > 1 {
		position = 1
	}
	return b.command("seek", position*duration, "absolute")
}

func (b *MPVBackend) SetVolume(volume float64) error {
	if volume < 0 {
		volume = 0
	}
	if volume > 1 {
		volume = 1
	}
	b.mu.RLock()
	started := b.conn != nil
	b.mu.RUnlock()
	if !started {
		b.mu.Lock()
		b.volume = volume
		b.mu.Unlock()
		return nil
	}
	if err := b.setProperty("volume", volume*100); err != nil {
		return err
	}
	b.mu.Lock()
	b.volume = volume
	b.mu.Unlock()
	return nil
}

func (b *MPVBackend) Position() float64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.position
}

func (b *MPVBackend) Duration() float64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.duration
}

func (b *MPVBackend) IsPlaying() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.loaded && !b.paused
}

func (b *MPVBackend) IsPaused() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.loaded && b.paused
}

func (b *MPVBackend) Events() <-chan Event {
	return b.events
}

func (b *MPVBackend) Close() error {
	select {
	case <-b.done:
	default:
		close(b.done)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.conn != nil {
		_ = b.conn.Close()
		b.conn = nil
	}
	if b.cmd != nil && b.cmd.Process != nil {
		_ = b.commandLocked("quit")
		_ = b.cmd.Process.Kill()
		_, _ = b.cmd.Process.Wait()
		b.cmd = nil
	}
	if b.ipcPath != "" {
		_ = os.Remove(b.ipcPath)
		b.ipcPath = ""
	}
	return nil
}

func (b *MPVBackend) ensureStarted(ctx context.Context) error {
	b.mu.RLock()
	started := b.conn != nil
	b.mu.RUnlock()
	if started {
		return nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.conn != nil {
		return nil
	}

	exe, err := b.findMPV()
	if err != nil {
		return err
	}
	ipcPath := mpvIPCPath()
	cmd := exec.CommandContext(ctx, exe,
		"--idle=yes",
		"--no-video",
		"--terminal=no",
		"--force-window=no",
		"--input-ipc-server="+ipcPath,
	)
	if err := cmd.Start(); err != nil {
		return err
	}

	conn, err := waitForMPVIPC(ctx, ipcPath, 5*time.Second)
	if err != nil {
		_ = cmd.Process.Kill()
		return err
	}

	b.cmd = cmd
	b.conn = conn
	b.ipcPath = ipcPath
	go b.readLoop(conn)

	_ = b.commandLocked("observe_property", 1, "time-pos")
	_ = b.commandLocked("observe_property", 2, "duration")
	_ = b.commandLocked("observe_property", 3, "pause")
	_ = b.commandLocked("observe_property", 4, "volume")
	_ = b.commandLocked("set_property", "volume", b.volume*100)
	return nil
}

func (b *MPVBackend) findMPV() (string, error) {
	if b.settings.MPVPath != "" {
		return b.settings.MPVPath, nil
	}
	for _, name := range []string{"mpv", "mpv.exe"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", errors.New("mpv executable not found; install mpv or set it in Settings")
}

func (b *MPVBackend) setProperty(name string, value any) error {
	return b.command("set_property", name, value)
}

func (b *MPVBackend) command(args ...any) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.commandLocked(args...)
}

func (b *MPVBackend) commandLocked(args ...any) error {
	if b.conn == nil {
		return errors.New("mpv IPC is not connected")
	}
	id := b.seq.Add(1)
	message := map[string]any{
		"command":    args,
		"request_id": id,
	}
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = b.conn.Write(data)
	return err
}

func (b *MPVBackend) readLoop(conn net.Conn) {
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		var message map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			continue
		}
		b.handleMessage(message)
	}
}

func (b *MPVBackend) handleMessage(message map[string]any) {
	event, _ := message["event"].(string)
	switch event {
	case "property-change":
		name, _ := message["name"].(string)
		b.handleProperty(name, message["data"])
	case "end-file":
		b.mu.Lock()
		b.loaded = false
		b.paused = true
		position := b.position
		duration := b.duration
		b.mu.Unlock()
		b.emit(Event{Type: EventCompleted, Position: position, Duration: duration})
	}
}

func (b *MPVBackend) handleProperty(name string, data any) {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch name {
	case "time-pos":
		if value, ok := floatValue(data); ok {
			b.position = value
			b.emitLocked(Event{Type: EventPosition, Position: b.position, Duration: b.duration})
		}
	case "duration":
		if value, ok := floatValue(data); ok {
			b.duration = value
		}
	case "pause":
		if value, ok := data.(bool); ok {
			b.paused = value
		}
	case "volume":
		if value, ok := floatValue(data); ok {
			b.volume = value / 100
		}
	}
}

func (b *MPVBackend) emit(event Event) {
	select {
	case b.events <- event:
	default:
	}
}

func (b *MPVBackend) emitLocked(event Event) {
	select {
	case b.events <- event:
	default:
	}
}

func floatValue(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case json.Number:
		ret, err := strconv.ParseFloat(string(v), 64)
		return ret, err == nil
	default:
		return 0, false
	}
}

func mpvIPCDir() string {
	dir := os.TempDir()
	if dir == "" {
		dir = "."
	}
	return dir
}

func mpvIPCFilename() string {
	return fmt.Sprintf("saki-mpv-%d-%d", os.Getpid(), time.Now().UnixNano())
}

func mpvIPCPathUnix() string {
	return filepath.Join(mpvIPCDir(), mpvIPCFilename()+".sock")
}
