package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestPersistenceAndConcurrentVisits(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	link, err := s.CreateLink(ctx, Link{LongURL: "https://example.com", ShortCode: "persist"})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 30 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Visit(ctx, link.ShortCode); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	reopened, err := New(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.Get(ctx, link.ShortCode)
	if err != nil || got.Clicks != 30 || got.Revision != 1 {
		t.Fatalf("persisted visits: %+v %v", got, err)
	}
}
func TestLegacyAndPathSafety(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir, nil)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(dir, "ABCDEF"), []byte("https://example.org"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Lookup(ctx, "abcdef"); err != nil || got != "https://example.org" {
		t.Fatalf("legacy lookup: %q %v", got, err)
	}
	if _, err := s.Lookup(ctx, "../secret"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("traversal: %v", err)
	}
	if err := os.Symlink(filepath.Join(dir, "ABCDEF"), filepath.Join(dir, "symlink")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Lookup(ctx, "symlink"); err == nil {
		t.Fatal("symlink accepted")
	}
	if _, err := s.Visit(ctx, "abcdef"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, "ABCDEF")
	if err != nil || got.Clicks != 1 {
		t.Fatalf("legacy upgrade: %+v %v", got, err)
	}
}
func TestConcurrentCreationAndListErrors(t *testing.T) {
	s, _ := New(t.TempDir(), nil)
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := range 25 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.CreateLink(ctx, Link{LongURL: "https://example.com", ShortCode: fmt.Sprintf("link-%d", i)}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	links, err := s.List(ctx)
	if err != nil || len(links) != 25 {
		t.Fatalf("list: %d %v", len(links), err)
	}
	for _, code := range []string{"broken-one", "broken-two"} {
		if err := os.WriteFile(filepath.Join(s.dir, code), []byte("{invalid json"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	links, err = s.List(ctx)
	if err == nil || len(links) != 25 {
		t.Fatalf("joined errors: %d %v", len(links), err)
	}
	if err := os.RemoveAll(s.dir); err != nil {
		t.Fatal(err)
	}
	if _, err := s.List(ctx); err == nil {
		t.Fatal("directory error swallowed")
	}
}
