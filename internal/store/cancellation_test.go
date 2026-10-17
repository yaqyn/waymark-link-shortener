package store

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
)

func TestCanceledListReturnsCancellationEvenForAnEmptyStore(t *testing.T) {
	st, err := New(t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	links, err := st.List(ctx)
	if !errors.Is(err, context.Canceled) || links != nil {
		t.Fatalf("links=%v err=%v", links, err)
	}
}
