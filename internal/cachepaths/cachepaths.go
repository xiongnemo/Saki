package cachepaths

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type Scope struct {
	root      string
	namespace string
}

func NewScope(root string, account string, endpoint string, fingerprint string) Scope {
	return Scope{root: ResolveRoot(root), namespace: Namespace(account, endpoint, fingerprint)}
}

func ResolveRoot(root string) string {
	if root != "" {
		return root
	}
	if userCache, err := os.UserCacheDir(); err == nil {
		return filepath.Join(userCache, "saki")
	}
	return filepath.Join(os.TempDir(), "saki")
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
	return filepath.Join(s.root, "audio")
}

func (s Scope) AudioPath(trackID string) string {
	return filepath.Join(s.AudioDir(), hashString(trackID)+".audio")
}

func (s Scope) CoversDir() string {
	return filepath.Join(s.root, "covers", s.namespace)
}

func (s Scope) CoverPath(coverArtID string) string {
	return filepath.Join(s.CoversDir(), hashString(coverArtID)+".jpg")
}

func hashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
