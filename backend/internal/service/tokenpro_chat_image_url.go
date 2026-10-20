package service

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const tokenProChatImageDefaultTTL = 7 * 24 * time.Hour

var (
	tokenProChatImageIDPattern  = regexp.MustCompile(`^[a-f0-9]{64}$`)
	tokenProIssuedImagePattern  = regexp.MustCompile(`图片已生成：(https://[^\s]+)`)
	tokenProHTTPSURLPattern     = regexp.MustCompile(`https://[^\s\])"'<>]+`)
	ErrTokenProChatImageExpired = errors.New("chat image url expired")
	ErrTokenProChatImageDenied  = errors.New("chat image url denied")
)

// TokenProChatImageFileConfig configures the signed download fallback.
// PublicBase is the site origin, for example https://tokenpro.work, without /v1.
type TokenProChatImageFileConfig struct {
	Dir        string
	Secret     string
	PublicBase string
	TTL        time.Duration
	Now        func() time.Time
}

// TokenProChatImageFiles stores generated images and signs expiring HTTPS URLs.
type TokenProChatImageFiles struct {
	mu         sync.Mutex
	dir        string
	secret     []byte
	publicBase string
	ttl        time.Duration
	now        func() time.Time
	memory     map[string]tokenProChatImageObject
}

type tokenProChatImageObject struct {
	ContentType string
	ExpiresAt   time.Time
	Data        []byte
}

type tokenProChatImageMeta struct {
	ContentType string    `json:"content_type"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// NewTokenProChatImageFiles returns nil when the public HTTPS origin or HMAC
// secret is missing. Callers then fall back to object storage or a config error.
func NewTokenProChatImageFiles(cfg TokenProChatImageFileConfig) *TokenProChatImageFiles {
	secret := strings.TrimSpace(cfg.Secret)
	base := strings.TrimRight(strings.TrimSpace(cfg.PublicBase), "/")
	parsed, err := url.Parse(base)
	if secret == "" || err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return nil
	}
	ttl := cfg.TTL
	if ttl <= 0 {
		ttl = tokenProChatImageDefaultTTL
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	dir := strings.TrimSpace(cfg.Dir)
	store := &TokenProChatImageFiles{
		dir:        dir,
		secret:     []byte(secret),
		publicBase: base,
		ttl:        ttl,
		now:        now,
	}
	if dir == "" {
		store.memory = map[string]tokenProChatImageObject{}
	}
	return store
}

// Save writes the image and returns an unguessable, expiring HTTPS URL.
func (s *TokenProChatImageFiles) Save(contentType string, data []byte) (string, error) {
	if s == nil {
		return "", ErrTokenProChatImageStorage
	}
	if len(data) == 0 {
		return "", ErrTokenProChatImageEmpty
	}
	contentType = tokenProImageContentType(contentType)
	id, err := tokenProChatImageID()
	if err != nil {
		return "", err
	}
	expires := s.now().Add(s.ttl)
	if s.memory != nil {
		s.mu.Lock()
		s.memory[id] = tokenProChatImageObject{ContentType: contentType, ExpiresAt: expires, Data: append([]byte(nil), data...)}
		s.mu.Unlock()
	} else if err := s.writeFile(id, contentType, expires, data); err != nil {
		return "", err
	}
	return SignTokenProChatImageURL(s.secret, s.publicBase, id, expires), nil
}

// Open checks the signature and expiry, then returns the image bytes.
// A valid signature for an expired object deletes it.
func (s *TokenProChatImageFiles) Open(id, expRaw, sig string) (string, []byte, error) {
	if s == nil || !VerifyTokenProChatImageSignature(s.secret, id, expRaw, sig) {
		return "", nil, ErrTokenProChatImageDenied
	}
	exp, _ := strconv.ParseInt(expRaw, 10, 64)
	if s.now().Unix() > exp {
		s.delete(id)
		return "", nil, ErrTokenProChatImageExpired
	}
	if s.memory != nil {
		s.mu.Lock()
		object, ok := s.memory[id]
		if ok && s.now().After(object.ExpiresAt) {
			delete(s.memory, id)
			ok = false
		}
		var data []byte
		contentType := ""
		if ok {
			contentType = object.ContentType
			data = append([]byte(nil), object.Data...)
		}
		s.mu.Unlock()
		if !ok {
			if s.now().Unix() > exp {
				return "", nil, ErrTokenProChatImageExpired
			}
			return "", nil, ErrTokenProChatImageDenied
		}
		return contentType, data, nil
	}
	return s.readFile(id)
}

func (s *TokenProChatImageFiles) writeFile(id, contentType string, expires time.Time, data []byte) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	path := s.objectPath(id)
	temp := path + ".tmp"
	if err := os.WriteFile(temp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(temp, path); err != nil {
		_ = os.Remove(temp)
		return err
	}
	meta := tokenProChatImageMeta{ContentType: contentType, ExpiresAt: expires}
	encoded, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return os.WriteFile(path+".meta", encoded, 0o600)
}

func (s *TokenProChatImageFiles) readFile(id string) (string, []byte, error) {
	path := s.objectPath(id)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil, ErrTokenProChatImageDenied
		}
		return "", nil, err
	}
	contentType := "image/png"
	if metaBytes, err := os.ReadFile(path + ".meta"); err == nil {
		var meta tokenProChatImageMeta
		if json.Unmarshal(metaBytes, &meta) == nil && meta.ContentType != "" {
			contentType = tokenProImageContentType(meta.ContentType)
			if !meta.ExpiresAt.IsZero() && s.now().After(meta.ExpiresAt) {
				s.delete(id)
				return "", nil, ErrTokenProChatImageExpired
			}
		}
	}
	return contentType, data, nil
}

// SetNow overrides the clock used for expiry checks. Tests use it after a URL is issued.
func (s *TokenProChatImageFiles) SetNow(now time.Time) {
	if s == nil {
		return
	}
	s.now = func() time.Time { return now }
}

func (s *TokenProChatImageFiles) delete(id string) {
	if !tokenProChatImageIDPattern.MatchString(id) {
		return
	}
	if s.memory != nil {
		s.mu.Lock()
		delete(s.memory, id)
		s.mu.Unlock()
		return
	}
	path := s.objectPath(id)
	_ = os.Remove(path)
	_ = os.Remove(path + ".meta")
}

func (s *TokenProChatImageFiles) objectPath(id string) string {
	return filepath.Join(s.dir, id)
}

// SignTokenProChatImageURL builds {publicBase}/v1/tokenpro/images/{id}?exp=&sig=.
func SignTokenProChatImageURL(secret []byte, publicBase, id string, expires time.Time) string {
	exp := strconv.FormatInt(expires.Unix(), 10)
	mac := tokenProChatImageMAC(secret, id, exp)
	base, err := url.Parse(strings.TrimRight(publicBase, "/"))
	if err != nil {
		return ""
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/v1/tokenpro/images/" + id
	query := base.Query()
	query.Set("exp", exp)
	query.Set("sig", hex.EncodeToString(mac))
	base.RawQuery = query.Encode()
	return base.String()
}

// VerifyTokenProChatImageSignature reports whether id, exp and sig match.
// Expiry is checked separately so a valid but stale link can be deleted.
func VerifyTokenProChatImageSignature(secret []byte, id, expRaw, sig string) bool {
	if !tokenProChatImageIDPattern.MatchString(id) || strings.TrimSpace(expRaw) == "" {
		return false
	}
	if _, err := strconv.ParseInt(expRaw, 10, 64); err != nil {
		return false
	}
	decoded, err := hex.DecodeString(strings.TrimSpace(sig))
	if err != nil {
		return false
	}
	expected := tokenProChatImageMAC(secret, id, expRaw)
	return hmac.Equal(expected, decoded)
}

func tokenProChatImageMAC(secret []byte, id, expRaw string) []byte {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(id))
	_, _ = mac.Write([]byte{'.'})
	_, _ = mac.Write([]byte(expRaw))
	return mac.Sum(nil)
}

func tokenProChatImageID() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

func tokenProImageContentType(contentType string) string {
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	switch contentType {
	case "image/png", "image/jpeg", "image/webp", "image/gif":
		return contentType
	default:
		return "image/png"
	}
}
