//go:build windows

package audio

import (
	"context"
	"net"
	"time"

	"github.com/Microsoft/go-winio"
)

func mpvIPCPath() string {
	return `\\.\pipe\` + mpvIPCFilename()
}

func waitForMPVIPC(ctx context.Context, path string, timeout time.Duration) (net.Conn, error) {
	deadline := time.Now().Add(timeout)
	for {
		conn, err := winio.DialPipeContext(ctx, path)
		if err == nil {
			return conn, nil
		}
		if time.Now().After(deadline) {
			return nil, err
		}
		select {
		case <-time.After(50 * time.Millisecond):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}
