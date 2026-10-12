// Package store persists links atomically on a local filesystem. Run one Waymark
// process per data directory; the mutex coordinates requests within that process.
package store

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/yaqyn/waymark-link-shortener/internal/apperr"
)

var (
	ErrNotFound = errors.New("link not found")
	ErrConflict = errors.New("short code already exists or link changed; refresh and try again")
	ErrGone     = errors.New("link expired or disabled")
	codePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{4,64}$`)
)

// Link is the persistent representation; Revision supports optimistic updates.
type Link struct {
	ShortCode     string     `json:"short_code"`
	LongURL       string     `json:"long_url"`
	Title         string     `json:"title"`
	CreatedAt     time.Time  `json:"created_at"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	Disabled      bool       `json:"disabled"`
	Clicks        uint64     `json:"clicks"`
	LastVisitedAt *time.Time `json:"last_visited_at,omitempty"`
	Revision      uint64     `json:"revision"`
}
type ShortURL = Link

type Store struct {
	dir string
	mu  sync.RWMutex
}

func ValidCode(code string) bool {
	if !codePattern.MatchString(code) {
		return false
	}
	switch strings.ToLower(code) {
	case "metrics", "healthz", "readyz", "api", "admin", "debug", "assets":
		return false
	}
	return true
}

func New(dir string, _ *slog.Logger) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func (s *Store) Create(ctx context.Context, destination string) (string, error) {
	link, err := s.CreateLink(ctx, Link{LongURL: destination})
	return link.ShortCode, err
}

func (s *Store) CreateLink(ctx context.Context, link Link) (Link, error) {
	if err := ctx.Err(); err != nil {
		return Link{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	custom := link.ShortCode != ""
	for range 10 {
		if !custom {
			link.ShortCode = rand.Text()[:10]
		}
		if !ValidCode(link.ShortCode) {
			return Link{}, errors.New("short code must contain 4–64 letters, numbers, underscores or hyphens and cannot be reserved")
		}
		if _, err := s.read(link.ShortCode); err == nil {
			if custom {
				return Link{}, ErrConflict
			}
			continue
		} else if !errors.Is(err, ErrNotFound) {
			return Link{}, err
		}
		link.CreatedAt = time.Now().UTC()
		link.Revision = 1
		if err := s.write(link, true); err != nil {
			return Link{}, err
		}
		return link, nil
	}
	return Link{}, errors.New("failed to generate unique short code")
}

func (s *Store) read(code string) (Link, error) {
	if !ValidCode(code) {
		return Link{}, ErrNotFound
	}
	path := filepath.Join(s.dir, code)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) && strings.ToUpper(code) != code {
		code = strings.ToUpper(code)
		path = filepath.Join(s.dir, code)
		info, err = os.Lstat(path)
	}
	if errors.Is(err, os.ErrNotExist) {
		return Link{}, ErrNotFound
	}
	if err != nil {
		return Link{}, apperr.WithAttrs(err, "path", path)
	}
	if !info.Mode().IsRegular() || info.Size() > 16384 {
		return Link{}, errors.New("invalid link record")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Link{}, apperr.WithAttrs(err, "path", path)
	}
	var link Link
	if strings.HasPrefix(strings.TrimSpace(string(data)), "{") {
		if err := json.Unmarshal(data, &link); err != nil {
			return Link{}, apperr.WithAttrs(err, "path", path)
		}
		if link.ShortCode != code {
			return Link{}, errors.New("link record code mismatch")
		}
	} else {
		link = Link{ShortCode: code, LongURL: string(data), CreatedAt: info.ModTime().UTC(), Revision: 1}
	}
	return link, nil
}

// write publishes a complete, synced record, so readers never see partial JSON.
func (s *Store) write(link Link, exclusive bool) error {
	data, err := json.Marshal(link)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(s.dir, ".link-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	path := filepath.Join(s.dir, link.ShortCode)
	if exclusive {
		if err := os.Link(tmp, path); err != nil {
			if errors.Is(err, os.ErrExist) {
				return ErrConflict
			}
			return err
		}
	} else if err := os.Rename(tmp, path); err != nil {
		return err
	}
	dir, err := os.Open(s.dir)
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}

func (s *Store) Get(ctx context.Context, code string) (Link, error) {
	if err := ctx.Err(); err != nil {
		return Link{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.read(code)
}
func (s *Store) Lookup(ctx context.Context, code string) (string, error) {
	link, err := s.Get(ctx, code)
	if err != nil {
		return "", err
	}
	if link.Disabled || link.ExpiresAt != nil && !link.ExpiresAt.After(time.Now()) {
		return "", ErrGone
	}
	return link.LongURL, nil
}

// Visit increments a bounded counter instead of retaining a string per request.
func (s *Store) Visit(ctx context.Context, code string) (Link, error) {
	if err := ctx.Err(); err != nil {
		return Link{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	link, err := s.read(code)
	if err != nil {
		return Link{}, err
	}
	if link.Disabled || link.ExpiresAt != nil && !link.ExpiresAt.After(time.Now()) {
		return Link{}, ErrGone
	}
	now := time.Now().UTC()
	link.Clicks++
	link.LastVisitedAt = &now
	if err := s.write(link, false); err != nil {
		return Link{}, err
	}
	return link, nil
}

func (s *Store) Update(ctx context.Context, code string, revision uint64, change func(*Link) error) (Link, error) {
	if err := ctx.Err(); err != nil {
		return Link{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	link, err := s.read(code)
	if err != nil {
		return Link{}, err
	}
	if revision != link.Revision {
		return Link{}, ErrConflict
	}
	if err := change(&link); err != nil {
		return Link{}, err
	}
	link.Revision++
	if err := s.write(link, false); err != nil {
		return Link{}, err
	}
	return link, nil
}

func (s *Store) List(ctx context.Context) ([]Link, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, apperr.WithAttrs(err, "path", s.dir)
	}
	links := make([]Link, 0, len(entries))
	var errs []error
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if !ValidCode(entry.Name()) {
			continue
		}
		link, err := s.read(entry.Name())
		if err != nil {
			errs = append(errs, fmt.Errorf("read link %s: %w", entry.Name(), err))
			continue
		}
		links = append(links, link)
	}
	sort.Slice(links, func(i, j int) bool {
		if links[i].CreatedAt.Equal(links[j].CreatedAt) {
			return links[i].ShortCode < links[j].ShortCode
		}
		return links[i].CreatedAt.After(links[j].CreatedAt)
	})
	return links, errors.Join(errs...)
}
