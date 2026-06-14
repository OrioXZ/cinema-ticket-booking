package booking

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/OrioXZ/cinema-ticket-booking/backend/internal/events"
	goredis "github.com/redis/go-redis/v9"
)

const expirationMarkerGrace = time.Second

var acquireScript = goredis.NewScript(`
if redis.call("HGET", KEYS[3], "state") == "BOOKED" then
  return {0, 0}
end
if redis.call("EXISTS", KEYS[1]) == 1 then
  return {0, 0}
end

local generation = redis.call("INCR", KEYS[2])
local lock = cjson.decode(ARGV[1])
lock["generation"] = generation
local marker = ARGV[2] .. generation
local event = cjson.decode(ARGV[6])
event["generation"] = generation

redis.call("PSETEX", KEYS[1], ARGV[3], cjson.encode(lock))
redis.call("PSETEX", marker, ARGV[4], "1")
redis.call("HSET", KEYS[3], "generation", generation, "state", "LOCKED")
redis.call("PUBLISH", ARGV[5], cjson.encode(event))
return {1, generation}
`)

var releaseScript = goredis.NewScript(`
local current = redis.call("GET", KEYS[1])
if not current then
  return 0
end
local lock = cjson.decode(current)
if lock["user_id"] ~= ARGV[1] or lock["ownership_token"] ~= ARGV[2] then
  return 2
end

local generation = tonumber(lock["generation"])
redis.call("DEL", KEYS[1])
redis.call("DEL", ARGV[3] .. generation)

local state = redis.call("HGET", KEYS[2], "state")
local stateGeneration = tonumber(redis.call("HGET", KEYS[2], "generation") or "0")
if state == "LOCKED" and stateGeneration == generation then
  local event = cjson.decode(ARGV[5])
  event["generation"] = generation
  redis.call("HSET", KEYS[2], "generation", generation, "state", "AVAILABLE")
  redis.call("PUBLISH", ARGV[4], cjson.encode(event))
end
return 1
`)

var verifyOwnershipScript = goredis.NewScript(`
local current = redis.call("GET", KEYS[1])
if not current then
  return {0, 0}
end
local lock = cjson.decode(current)
if lock["user_id"] ~= ARGV[1] or lock["ownership_token"] ~= ARGV[2] then
  return {2, 0}
end
return {1, tonumber(lock["generation"])}
`)

var confirmScript = goredis.NewScript(`
local generation = tonumber(ARGV[1])
local stateGeneration = tonumber(redis.call("HGET", KEYS[3], "generation") or "0")
if stateGeneration > generation then
  generation = stateGeneration
end

local current = redis.call("GET", KEYS[1])
if current then
  local currentLock = cjson.decode(current)
  local currentGeneration = tonumber(currentLock["generation"])
  if currentGeneration > generation then
    generation = currentGeneration
  end
  redis.call("DEL", ARGV[2] .. currentGeneration)
end

local allocatedGeneration = tonumber(redis.call("GET", KEYS[2]) or "0")
if allocatedGeneration > generation then
  generation = allocatedGeneration
end
if generation <= 0 then
  return redis.error_reply("missing booking generation")
end

local event = cjson.decode(ARGV[4])
event["generation"] = generation
redis.call("HSET", KEYS[3], "generation", generation, "state", "BOOKED")
redis.call("DEL", KEYS[1])
redis.call("DEL", ARGV[2] .. tonumber(ARGV[1]))
redis.call("PUBLISH", ARGV[3], cjson.encode(event))
return generation
`)

var expireScript = goredis.NewScript(`
local state = redis.call("HGET", KEYS[2], "state")
if state == "BOOKED" then
  return 0
end
local generation = tonumber(ARGV[1])
local stateGeneration = tonumber(redis.call("HGET", KEYS[2], "generation") or "0")
if state ~= "LOCKED" or stateGeneration ~= generation then
  return 0
end
if redis.call("EXISTS", KEYS[1]) == 1 then
  return 0
end

local event = cjson.decode(ARGV[3])
event["generation"] = generation
redis.call("HSET", KEYS[2], "generation", generation, "state", "AVAILABLE")
redis.call("PUBLISH", ARGV[2], cjson.encode(event))
return 1
`)

type RedisLockRepository struct {
	client  goredis.UniversalClient
	channel string
}

func NewRedisLockRepository(
	client goredis.UniversalClient,
	channel ...string,
) *RedisLockRepository {
	eventChannel := "cinema.events"
	if len(channel) > 0 && channel[0] != "" {
		eventChannel = channel[0]
	}
	return &RedisLockRepository{client: client, channel: eventChannel}
}

func (r *RedisLockRepository) Acquire(
	ctx context.Context,
	lock SeatLock,
	ttl time.Duration,
	event events.DomainEvent,
) (bool, int64, error) {
	value, err := lockValue(lock)
	if err != nil {
		return false, 0, err
	}
	eventData, err := events.Marshal(event)
	if err != nil {
		return false, 0, err
	}
	result, err := acquireScript.Run(
		ctx,
		r.client,
		[]string{
			lockKey(lock.ShowtimeID, lock.SeatNo),
			generationKey(lock.ShowtimeID, lock.SeatNo),
			realtimeStateKey(lock.ShowtimeID, lock.SeatNo),
		},
		value,
		expirationMarkerPrefix(lock.ShowtimeID, lock.SeatNo),
		ttl.Milliseconds(),
		(ttl + expirationMarkerGrace).Milliseconds(),
		r.channel,
		string(eventData),
	).Slice()
	if err != nil {
		return false, 0, err
	}
	acquired, err := redisInt64(result[0])
	if err != nil {
		return false, 0, err
	}
	generation, err := redisInt64(result[1])
	if err != nil {
		return false, 0, err
	}
	return acquired == 1, generation, nil
}

func (r *RedisLockRepository) Get(ctx context.Context, showtimeID, seatNo string) (*SeatLock, error) {
	value, err := r.client.Get(ctx, lockKey(showtimeID, seatNo)).Result()
	if err == goredis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	lock, err := decodeLock(showtimeID, seatNo, value)
	if err != nil {
		return nil, err
	}
	return &lock, nil
}

func (r *RedisLockRepository) GetMany(
	ctx context.Context,
	showtimeID string,
	seatNos []string,
) (map[string]SeatLock, error) {
	if len(seatNos) == 0 {
		return map[string]SeatLock{}, nil
	}
	keys := make([]string, len(seatNos))
	for i, seatNo := range seatNos {
		keys[i] = lockKey(showtimeID, seatNo)
	}
	values, err := r.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}
	locks := make(map[string]SeatLock)
	for i, value := range values {
		if value == nil {
			continue
		}
		encoded, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("unexpected Redis lock value type %T", value)
		}
		lock, err := decodeLock(showtimeID, seatNos[i], encoded)
		if err != nil {
			return nil, err
		}
		locks[seatNos[i]] = lock
	}
	return locks, nil
}

func (r *RedisLockRepository) VerifyOwnership(
	ctx context.Context,
	lock SeatLock,
) (OwnershipResult, int64, error) {
	result, err := verifyOwnershipScript.Run(
		ctx,
		r.client,
		[]string{lockKey(lock.ShowtimeID, lock.SeatNo)},
		lock.UserID,
		lock.OwnershipToken,
	).Slice()
	if err != nil {
		return OwnershipMissing, 0, err
	}
	ownership, err := redisInt64(result[0])
	if err != nil {
		return OwnershipMissing, 0, err
	}
	generation, err := redisInt64(result[1])
	if err != nil {
		return OwnershipMissing, 0, err
	}
	return OwnershipResult(ownership), generation, nil
}

func (r *RedisLockRepository) Release(
	ctx context.Context,
	lock SeatLock,
	event events.DomainEvent,
) (ReleaseResult, error) {
	eventData, err := events.Marshal(event)
	if err != nil {
		return ReleaseMissing, err
	}
	result, err := releaseScript.Run(
		ctx,
		r.client,
		[]string{
			lockKey(lock.ShowtimeID, lock.SeatNo),
			realtimeStateKey(lock.ShowtimeID, lock.SeatNo),
		},
		lock.UserID,
		lock.OwnershipToken,
		expirationMarkerPrefix(lock.ShowtimeID, lock.SeatNo),
		r.channel,
		string(eventData),
	).Int()
	if err != nil {
		return ReleaseMissing, err
	}
	return ReleaseResult(result), nil
}

func (r *RedisLockRepository) Confirm(
	ctx context.Context,
	lock SeatLock,
	event events.DomainEvent,
) error {
	eventData, err := events.Marshal(event)
	if err != nil {
		return err
	}
	return confirmScript.Run(
		ctx,
		r.client,
		[]string{
			lockKey(lock.ShowtimeID, lock.SeatNo),
			generationKey(lock.ShowtimeID, lock.SeatNo),
			realtimeStateKey(lock.ShowtimeID, lock.SeatNo),
		},
		lock.Generation,
		expirationMarkerPrefix(lock.ShowtimeID, lock.SeatNo),
		r.channel,
		string(eventData),
	).Err()
}

func (r *RedisLockRepository) PublishExpiration(
	ctx context.Context,
	showtimeID string,
	seatNo string,
	generation int64,
	event events.DomainEvent,
) (bool, error) {
	eventData, err := events.Marshal(event)
	if err != nil {
		return false, err
	}
	result, err := expireScript.Run(
		ctx,
		r.client,
		[]string{
			lockKey(showtimeID, seatNo),
			realtimeStateKey(showtimeID, seatNo),
		},
		generation,
		r.channel,
		string(eventData),
	).Int()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

func lockKey(showtimeID, seatNo string) string {
	return fmt.Sprintf("seat_lock:%s:%s", showtimeID, seatNo)
}

func generationKey(showtimeID, seatNo string) string {
	return fmt.Sprintf("seat_lock_generation:%s:%s", showtimeID, seatNo)
}

func realtimeStateKey(showtimeID, seatNo string) string {
	return fmt.Sprintf("seat_realtime_state:%s:%s", showtimeID, seatNo)
}

func expirationMarkerPrefix(showtimeID, seatNo string) string {
	return fmt.Sprintf("seat_lock_expiry:%s:%s:", showtimeID, seatNo)
}

func lockValue(lock SeatLock) (string, error) {
	value := struct {
		UserID         string `json:"user_id"`
		OwnershipToken string `json:"ownership_token"`
		Generation     int64  `json:"generation"`
	}{
		UserID:         lock.UserID,
		OwnershipToken: lock.OwnershipToken,
		Generation:     lock.Generation,
	}
	encoded, err := json.Marshal(value)
	return string(encoded), err
}

func decodeLock(showtimeID, seatNo, value string) (SeatLock, error) {
	var owner struct {
		UserID         string `json:"user_id"`
		OwnershipToken string `json:"ownership_token"`
		Generation     int64  `json:"generation"`
	}
	if err := json.Unmarshal([]byte(value), &owner); err != nil {
		return SeatLock{}, fmt.Errorf("decode Redis lock: %w", err)
	}
	return SeatLock{
		ShowtimeID:     showtimeID,
		SeatNo:         seatNo,
		UserID:         owner.UserID,
		OwnershipToken: owner.OwnershipToken,
		Generation:     owner.Generation,
	}, nil
}

func redisInt64(value any) (int64, error) {
	switch typed := value.(type) {
	case int64:
		return typed, nil
	case string:
		return strconv.ParseInt(typed, 10, 64)
	default:
		return 0, fmt.Errorf("unexpected Redis integer type %T", value)
	}
}
