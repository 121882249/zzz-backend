package repository

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

type redisImageTurnStore struct{ client redis.UniversalClient }

func NewTokenProImageTurnStore(client redis.UniversalClient) service.TokenProImageTurnStore {
	return &redisImageTurnStore{client: client}
}

func (s *redisImageTurnStore) Bind(ctx context.Context, key string, route service.TokenProImageTurnRoute) error {
	data, err := json.Marshal(route)
	if err != nil {
		return err
	}
	// Atomic compare-or-create across replicas, with a fixed lifetime.
	result, err := s.client.Eval(ctx, `local v=redis.call('GET',KEYS[1]); if v then if v==ARGV[1] then return 1 else return 0 end end; redis.call('SET',KEYS[1],ARGV[1],'PX',ARGV[2]); return 1`, []string{key}, string(data), service.TokenProImageTurnTTL.Milliseconds()).Int()
	if err != nil {
		return err
	}
	if result != 1 {
		return service.ErrImageTurnConflict
	}
	return nil
}

func (s *redisImageTurnStore) Lookup(ctx context.Context, key string) (service.TokenProImageTurnRoute, error) {
	var route service.TokenProImageTurnRoute
	data, err := s.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return route, service.ErrImageTurnMissing
	}
	if err != nil {
		return route, err
	}
	if err := json.Unmarshal(data, &route); err != nil {
		return route, err
	}
	if route.GroupID <= 0 || route.Model == "" {
		return route, service.ErrImageTurnMissing
	}
	return route, nil
}
