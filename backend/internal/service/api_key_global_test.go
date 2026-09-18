//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type ensureGlobalKeyRepoStub struct {
	*apiKeyRepoStub
	global         *APIKey
	createErr      error
	winnerOnCreate *APIKey
	createCalls    int
}

func (s *ensureGlobalKeyRepoStub) GetGlobalByUserID(_ context.Context, userID int64) (*APIKey, error) {
	if s.global == nil || s.global.UserID != userID {
		return nil, ErrAPIKeyNotFound
	}
	copy := *s.global
	return &copy, nil
}

func (s *ensureGlobalKeyRepoStub) Create(_ context.Context, key *APIKey) error {
	s.createCalls++
	if s.winnerOnCreate != nil {
		copy := *s.winnerOnCreate
		s.global = &copy
	}
	if s.createErr != nil {
		return s.createErr
	}
	copy := *key
	copy.ID = 501
	s.global = &copy
	key.ID = copy.ID
	return nil
}

func TestAPIKeyServiceEnsureGlobalKeyReturnsExistingCredential(t *testing.T) {
	existing := &APIKey{ID: 41, UserID: 7, Key: "sk-existing-global", Name: "TokenPro", KeyType: APIKeyTypeGlobal, Status: StatusActive}
	repo := &ensureGlobalKeyRepoStub{apiKeyRepoStub: &apiKeyRepoStub{}, global: existing}
	svc := &APIKeyService{apiKeyRepo: repo, cfg: &config.Config{}}

	key, err := svc.EnsureGlobalKey(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, existing.Key, key.Key)
	require.Equal(t, APIKeyTypeGlobal, key.KeyType)
	require.Zero(t, repo.createCalls)
}

func TestAPIKeyServiceEnsureGlobalKeyCreatesMissingCredentialOnce(t *testing.T) {
	repo := &ensureGlobalKeyRepoStub{apiKeyRepoStub: &apiKeyRepoStub{}}
	svc := &APIKeyService{apiKeyRepo: repo, cfg: &config.Config{Default: config.DefaultConfig{APIKeyPrefix: "tp-"}}}

	first, err := svc.EnsureGlobalKey(context.Background(), 9)
	require.NoError(t, err)
	second, err := svc.EnsureGlobalKey(context.Background(), 9)
	require.NoError(t, err)
	require.Equal(t, int64(501), first.ID)
	require.Equal(t, first.Key, second.Key)
	require.Equal(t, APIKeyTypeGlobal, first.KeyType)
	require.Nil(t, first.GroupID)
	require.Equal(t, 1, repo.createCalls)
}

func TestAPIKeyServiceEnsureGlobalKeyLoadsConcurrentWinner(t *testing.T) {
	winner := &APIKey{ID: 77, UserID: 11, Key: "sk-race-winner", Name: "TokenPro", KeyType: APIKeyTypeGlobal, Status: StatusActive}
	repo := &ensureGlobalKeyRepoStub{
		apiKeyRepoStub: &apiKeyRepoStub{},
		createErr:      errors.New("unique constraint"),
		winnerOnCreate: winner,
	}
	svc := &APIKeyService{apiKeyRepo: repo, cfg: &config.Config{}}

	key, err := svc.EnsureGlobalKey(context.Background(), 11)
	require.NoError(t, err)
	require.Equal(t, winner.ID, key.ID)
	require.Equal(t, winner.Key, key.Key)
	require.Equal(t, 1, repo.createCalls)
}
