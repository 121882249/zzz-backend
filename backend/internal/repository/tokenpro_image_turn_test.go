package repository

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestImageTurnStoreIsolationConflictExpiry(t *testing.T) {
	r := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: r.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	a, b := NewTokenProImageTurnStore(client), NewTokenProImageTurnStore(client)
	ctx := context.Background()
	key := &service.APIKey{ID: 7, UserID: 8, Key: "old-key"}
	route := service.TokenProImageTurnRoute{GroupID: 65, Model: "gpt-image-2.5-flare", ThreadID: "thread"}
	index := service.TokenProImageTurnKey(key, "turn")
	require.NoError(t, a.Bind(ctx, index, route))
	got, err := b.Lookup(ctx, index)
	require.NoError(t, err)
	require.Equal(t, route, got)
	conflict := route
	conflict.Model = "gpt-image-2.5-sunburst"
	require.ErrorIs(t, b.Bind(ctx, index, conflict), service.ErrImageTurnConflict)
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); require.NoError(t, b.Bind(ctx, index, route)) }()
	}
	wg.Wait()
	rotated := *key
	rotated.Key = "new-key"
	_, err = b.Lookup(ctx, service.TokenProImageTurnKey(&rotated, "turn"))
	require.ErrorIs(t, err, service.ErrImageTurnMissing)
	other := *key
	other.UserID++
	_, err = b.Lookup(ctx, service.TokenProImageTurnKey(&other, "turn"))
	require.ErrorIs(t, err, service.ErrImageTurnMissing)
	r.FastForward(20 * time.Minute)
	require.NoError(t, a.Bind(ctx, index, route))
	r.FastForward(11 * time.Minute)
	_, err = b.Lookup(ctx, index)
	require.ErrorIs(t, err, service.ErrImageTurnMissing)
	r.Close()
	require.Error(t, a.Bind(ctx, index, route))
}

func TestImageThreadStoreLatestWinsAndIsolatesKeys(t *testing.T) {
	r := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: r.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := NewTokenProImageTurnStore(client).(service.TokenProImageThreadStore)
	ctx := context.Background()
	key := &service.APIKey{ID: 7, UserID: 8, Key: "key-a"}
	thread := "11111111-1111-4111-8111-111111111111"
	index := service.TokenProImageThreadKey(key, thread)
	first := service.TokenProImageTurnRoute{GroupID: 65, Model: "gpt-image-2.5-flare", ThreadID: thread}
	require.NoError(t, store.RememberThread(ctx, index, first))
	next := first
	next.Model = "gpt-image-2.5-sunburst"
	require.NoError(t, store.RememberThread(ctx, index, next))
	got, err := store.LookupThread(ctx, index)
	require.NoError(t, err)
	require.Equal(t, next, got)
	otherThread := service.TokenProImageThreadKey(key, "22222222-2222-4222-8222-222222222222")
	_, err = store.LookupThread(ctx, otherThread)
	require.ErrorIs(t, err, service.ErrImageTurnMissing)
	rotated := *key
	rotated.Key = "key-b"
	_, err = store.LookupThread(ctx, service.TokenProImageThreadKey(&rotated, thread))
	require.ErrorIs(t, err, service.ErrImageTurnMissing)
	r.FastForward(24 * time.Hour)
	_, err = store.LookupThread(ctx, index)
	require.ErrorIs(t, err, service.ErrImageTurnMissing)
}
