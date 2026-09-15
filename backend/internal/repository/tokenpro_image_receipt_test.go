package repository

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestImageReceiptStoreLifecycle(t *testing.T) {
	r := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: r.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	a, ok := NewTokenProImageTurnStore(client).(service.TokenProImageCallStore)
	require.True(t, ok)
	b, ok := NewTokenProImageTurnStore(client).(service.TokenProImageCallStore)
	require.True(t, ok)
	ctx := context.Background()
	key := service.TokenProImageTurnKey(&service.APIKey{ID: 7, UserID: 8, Key: "fixture"}, "turn")
	call := service.TokenProImageCall{CallID: "call_tp_receipt_" + strings.Repeat("a", 32), RequestHash: "req-A", PromptHash: "prompt"}
	prepared, err := a.PrepareCall(ctx, key, call)
	require.NoError(t, err)
	require.Equal(t, "pending", prepared.State)
	retry := call
	retry.CallID = "call_tp_receipt_" + strings.Repeat("b", 32)
	got, err := b.PrepareCall(ctx, key, retry)
	require.NoError(t, err)
	require.Equal(t, prepared, got, "a retried dispatch reuses the call ID across replicas")
	retry.RequestHash = "req-B"
	_, err = b.PrepareCall(ctx, key, retry)
	require.ErrorIs(t, err, service.ErrImageTurnConflict)
	_, err = a.ClaimCall(ctx, key, "wrong-prompt")
	require.ErrorIs(t, err, service.ErrImageTurnConflict)
	var winners atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, claimErr := b.ClaimCall(ctx, key, "prompt")
			if claimErr == nil && id == call.CallID {
				winners.Add(1)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, int32(1), winners.Load(), "duplicate Images requests must not generate twice")
	require.NoError(t, a.FinishCall(ctx, key, call.CallID, true))
	require.NoError(t, a.FinishCall(ctx, key, call.CallID, true))
	require.ErrorIs(t, a.FinishCall(ctx, key, call.CallID, false), service.ErrImageTurnConflict)
	state, err := b.CallResult(ctx, key, call.CallID)
	require.NoError(t, err)
	require.Equal(t, "succeeded", state)
	_, err = b.CallResult(ctx, key+"other-key-or-turn", call.CallID)
	require.ErrorIs(t, err, service.ErrImageTurnMissing)
	_, err = b.ClaimCall(ctx, key, "prompt")
	require.ErrorIs(t, err, service.ErrImageTurnConflict)
	_, err = a.PrepareCall(ctx, key, retry)
	require.NoError(t, err, "a new user request may start after the preceding generation finished")
	_, err = b.ClaimCall(ctx, key, "prompt")
	require.NoError(t, err)
	require.NoError(t, b.FinishCall(ctx, key, retry.CallID, false))
	state, err = a.CallResult(ctx, key, retry.CallID)
	require.NoError(t, err)
	require.Equal(t, "failed", state)
	state, err = a.CallResult(ctx, key, call.CallID)
	require.NoError(t, err)
	require.Equal(t, "succeeded", state, "later calls must not overwrite earlier completion records")
	r.FastForward(31 * time.Minute)
	_, err = a.CallResult(ctx, key, call.CallID)
	require.ErrorIs(t, err, service.ErrImageTurnMissing)
	id, err := a.ClaimCall(ctx, "legacy-turn-without-receipts", "prompt")
	require.NoError(t, err)
	require.Empty(t, id)
}
