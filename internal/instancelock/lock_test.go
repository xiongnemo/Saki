package instancelock

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestSharedLocksCoexistAndBlockExclusive(t *testing.T) {
	if !Supported {
		t.Skip("no file locking on this platform")
	}
	path := filepath.Join(t.TempDir(), "state", "saki.lock")

	if busy, err := Probe(path); err != nil || busy {
		t.Fatalf("probe before any lock = %v, %v", busy, err)
	}
	first, err := Shared(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Shared(path)
	if err != nil {
		t.Fatalf("second shared lock: %v", err)
	}
	if _, err := Exclusive(path); !errors.Is(err, ErrLocked) {
		t.Fatalf("exclusive while shared held = %v, want ErrLocked", err)
	}
	if busy, err := Probe(path); err != nil || !busy {
		t.Fatalf("probe while shared held = %v, %v", busy, err)
	}
	_ = first.Close()
	_ = second.Close()

	exclusive, err := Exclusive(path)
	if err != nil {
		t.Fatalf("exclusive after release: %v", err)
	}
	if _, err := Shared(path); !errors.Is(err, ErrLocked) {
		t.Fatalf("shared while exclusive held = %v, want ErrLocked", err)
	}
	_ = exclusive.Close()
}
