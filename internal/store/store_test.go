package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestUpsertAndGet(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.Upsert(ctx, Player{UserID: 1, Name: "ana", Cash: 100, Rebirths: 2}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "ana" || got.Cash != 100 || got.Rebirths != 2 {
		t.Fatalf("dados errados: %+v", got)
	}
}

func TestGetNotFound(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Get(context.Background(), 99); !errors.Is(err, ErrNotFound) {
		t.Fatalf("esperava ErrNotFound, veio %v", err)
	}
}

func TestRebirthsNeverDecrease(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	s.Upsert(ctx, Player{UserID: 1, Name: "ana", Rebirths: 5})
	s.Upsert(ctx, Player{UserID: 1, Name: "ana", Rebirths: 3}) // dado velho

	got, _ := s.Get(ctx, 1)
	if got.Rebirths != 5 {
		t.Fatalf("rebirths diminuiu: %d", got.Rebirths)
	}
}

func TestTopOrdersByStat(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	s.Upsert(ctx, Player{UserID: 1, Name: "a", Cash: 10, Rebirths: 9})
	s.Upsert(ctx, Player{UserID: 2, Name: "b", Cash: 50, Rebirths: 1})
	s.Upsert(ctx, Player{UserID: 3, Name: "c", Cash: 30, Rebirths: 4})

	byCash, err := s.Top(ctx, "cash", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(byCash) != 2 || byCash[0].UserID != 2 || byCash[1].UserID != 3 {
		t.Fatalf("ranking de cash errado: %+v", byCash)
	}

	byRebirths, _ := s.Top(ctx, "rebirths", 10)
	if byRebirths[0].UserID != 1 {
		t.Fatalf("ranking de rebirths errado: %+v", byRebirths)
	}
}

func TestTopRejectsUnknownStat(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Top(context.Background(), "cash; DROP TABLE players", 5); err == nil {
		t.Fatal("deveria recusar ranking desconhecido")
	}
}
