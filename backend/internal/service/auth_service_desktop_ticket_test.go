//go:build unit

package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type desktopTicketCacheStub struct {
	refreshTokenCacheStub
	mu   sync.Mutex
	data map[string]*RefreshTokenData
}

func (s *desktopTicketCacheStub) StoreRefreshToken(_ context.Context, tokenHash string, data *RefreshTokenData, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[tokenHash] = data
	return nil
}

func (s *desktopTicketCacheStub) ConsumeRefreshToken(_ context.Context, tokenHash string) (*RefreshTokenData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.data[tokenHash]
	if !ok {
		return nil, ErrRefreshTokenNotFound
	}
	delete(s.data, tokenHash)
	return data, nil
}

func TestDesktopTicketIsShortLivedAndSingleUse(t *testing.T) {
	ctx := context.Background()
	user := &User{
		ID:                   7,
		Email:                "desktop@example.com",
		Role:                 RoleUser,
		Status:               StatusActive,
		TokenVersion:         3,
		TokenVersionResolved: true,
	}
	cache := &desktopTicketCacheStub{data: make(map[string]*RefreshTokenData)}
	service := NewAuthService(
		nil,
		&userRepoStub{user: user},
		nil,
		cache,
		&config.Config{},
		nil, nil, nil, nil, nil, nil, nil, nil,
	)

	ticket, expiresIn, err := service.GenerateDesktopTicket(ctx, user)
	require.NoError(t, err)
	require.Regexp(t, `^dt_[0-9a-f]{64}$`, ticket)
	require.Equal(t, 60, expiresIn)
	require.Len(t, cache.data, 1)

	consumedUser, err := service.ConsumeDesktopTicket(ctx, ticket)
	require.NoError(t, err)
	require.Equal(t, user.ID, consumedUser.ID)
	require.Empty(t, cache.data)

	_, err = service.ConsumeDesktopTicket(ctx, ticket)
	require.ErrorIs(t, err, ErrDesktopTicketInvalid)
}
