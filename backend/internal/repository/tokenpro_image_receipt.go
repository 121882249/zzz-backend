package repository

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

func (s *redisImageTurnStore) PrepareCall(ctx context.Context, key string, call service.TokenProImageCall) (service.TokenProImageCall, error) {
	if !service.ValidTokenProImageReceiptCall(call.CallID) || call.RequestHash == "" || call.PromptHash == "" {
		return call, service.ErrImageTurnConflict
	}
	call.State = "pending"
	data, err := json.Marshal(call)
	if err != nil {
		return call, err
	}
	value, err := s.client.Eval(ctx, `
local old=redis.call('GET',KEYS[1])
if old then
 local previous=cjson.decode(old)
 if previous.request_hash==ARGV[2] then return old end
 if previous.state=='pending' or previous.state=='running' then return '' end
end
redis.call('SET',KEYS[1],ARGV[1],'PX',ARGV[3])
return ARGV[1]`, []string{key + ":receipt:active"}, string(data), call.RequestHash, service.TokenProImageTurnTTL.Milliseconds()).Text()
	if err != nil {
		return call, err
	}
	if value == "" {
		return call, service.ErrImageTurnConflict
	}
	err = json.Unmarshal([]byte(value), &call)
	return call, err
}

func (s *redisImageTurnStore) ClaimCall(ctx context.Context, key, promptHash string) (string, error) {
	value, err := s.client.Eval(ctx, `
local old=redis.call('GET',KEYS[1])
if not old then return '' end
local call=cjson.decode(old)
if call.prompt_hash~=ARGV[1] or call.state~='pending' then return 'conflict' end
call.state='running'
redis.call('SET',KEYS[1],cjson.encode(call),'KEEPTTL')
return call.call_id`, []string{key + ":receipt:active"}, promptHash).Text()
	if err != nil {
		return "", err
	}
	if value == "conflict" {
		return "", service.ErrImageTurnConflict
	}
	return value, nil
}

func (s *redisImageTurnStore) FinishCall(ctx context.Context, key, callID string, success bool) error {
	if !service.ValidTokenProImageReceiptCall(callID) {
		return service.ErrImageTurnConflict
	}
	state := "failed"
	if success {
		state = "succeeded"
	}
	result, err := s.client.Eval(ctx, `
local old=redis.call('GET',KEYS[1])
if not old then return 0 end
local call=cjson.decode(old)
if call.call_id~=ARGV[1] then return 0 end
if call.state==ARGV[2] then return 1 end
if call.state~='running' then return 0 end
call.state=ARGV[2]
redis.call('SET',KEYS[1],cjson.encode(call),'KEEPTTL')
redis.call('SET',KEYS[2],ARGV[2],'PX',ARGV[3])
return 1`, []string{key + ":receipt:active", key + ":receipt:result:" + callID}, callID, state, service.TokenProImageTurnTTL.Milliseconds()).Int()
	if err != nil {
		return err
	}
	if result != 1 {
		return service.ErrImageTurnConflict
	}
	return nil
}

func (s *redisImageTurnStore) CallResult(ctx context.Context, key, callID string) (string, error) {
	if !service.ValidTokenProImageReceiptCall(callID) {
		return "", service.ErrImageTurnConflict
	}
	value, err := s.client.Get(ctx, key+":receipt:result:"+callID).Result()
	if err == nil {
		return value, nil
	}
	if !errors.Is(err, redis.Nil) {
		return "", err
	}
	data, err := s.client.Get(ctx, key+":receipt:active").Bytes()
	if errors.Is(err, redis.Nil) {
		return "", service.ErrImageTurnMissing
	}
	if err != nil {
		return "", err
	}
	var call service.TokenProImageCall
	if err := json.Unmarshal(data, &call); err != nil {
		return "", err
	}
	if call.CallID != callID {
		return "", service.ErrImageTurnMissing
	}
	return call.State, nil
}
