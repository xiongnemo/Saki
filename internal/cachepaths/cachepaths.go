package cachepaths

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/xiongnemo/saki/internal/paths"
)

// Roots are the directories that hold one subdirectory per account namespace.
type Roots struct {
	Audio   string
	Covers  string
	Catalog string
	Offline string
}

// RootsOf returns the directories of a resolved storage layout.
func RootsOf(storage paths.Storage) Roots {
	return Roots{
		Audio:   storage.Audio.Path,
		Covers:  storage.Covers.Path,
		Catalog: storage.Catalog.Path,
		Offline: storage.Offline.Path,
	}
}

var (
	defaultMu    sync.Mutex
	defaultRoots *Roots
)

// SetDefaultRoots sets the directories used when settings.cacheDir is empty.
// The TUI resolves them once at startup, together with the notices for
// directories still at an old location.
func SetDefaultRoots(roots Roots) {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultRoots = &roots
}

// DefaultRoots returns the directories used when settings.cacheDir is empty,
// resolving them on first use if SetDefaultRoots was not called.
func DefaultRoots() Roots {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultRoots == nil {
		var roots Roots
		if env, err := paths.System(); err == nil {
			roots = RootsOf(paths.ResolveStorage(env, ""))
		} else {
			roots = RootsFor(filepath.Join(os.TempDir(), paths.App))
		}
		defaultRoots = &roots
	}
	return *defaultRoots
}

// RootsFor returns the directories for a settings.cacheDir value: everything
// below it when it is set, the default layout otherwise.
func RootsFor(root string) Roots {
	if strings.TrimSpace(root) == "" {
		return DefaultRoots()
	}
	return Roots{
		Audio:   filepath.Join(root, paths.AudioDirName),
		Covers:  filepath.Join(root, paths.CoversDirName),
		Catalog: filepath.Join(root, paths.CatalogDirName),
		Offline: filepath.Join(root, paths.OfflineDirName),
	}
}

type Scope struct {
	roots     Roots
	namespace string
}

// NewScope scopes the directories for root (a settings.cacheDir value, empty
// for the default layout) to one account and library.
func NewScope(root string, account string, endpoint string, fingerprint string) Scope {
	return Scope{roots: RootsFor(root), namespace: Namespace(account, endpoint, fingerprint)}
}

func Namespace(account string, endpoint string, fingerprint string) string {
	if strings.TrimSpace(fingerprint) != "" {
		return "verified-" + hashString(account+"\x00"+fingerprint)
	}
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	endpointKey := strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if err == nil && parsed.Host != "" {
		endpointKey = parsed.Scheme + "://" + strings.ToLower(parsed.Host) + strings.TrimRight(parsed.EscapedPath(), "/")
	}
	return "unverified-" + hashString(account+"\x00"+endpointKey)
}

func (s Scope) AudioDir() string {
	return filepath.Join(s.AudioRoot(), s.namespace)
}

func (s Scope) AudioRoot() string {
	return s.roots.Audio
}

func (s Scope) AudioPath(trackID string) string {
	return filepath.Join(s.AudioDir(), hashString(trackID)+".audio")
}

func (s Scope) CoversDir() string {
	return filepath.Join(s.roots.Covers, s.namespace)
}

func (s Scope) CoverPath(coverArtID string) string {
	return filepath.Join(s.CoversDir(), hashString(coverArtID)+".jpg")
}

func (s Scope) CatalogDir() string {
	return filepath.Join(s.roots.Catalog, s.namespace)
}

func (s Scope) CatalogPath() string {
	return filepath.Join(s.CatalogDir(), "library.db")
}

func (s Scope) OfflineDir() string {
	return filepath.Join(s.roots.Offline, s.namespace)
}

func hashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
