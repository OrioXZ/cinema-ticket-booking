package booking

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

var releaseScript = goredis.NewScript(`
local current = redis.call("GET", KEYS[1])
if not current then
  return 0
end
if current ~= ARGV[1] then
  return 2
end
redis.call("DEL", KEYS[1])
return 1
`)

var verifyAndExtendScript = goredis.NewScript(`
local current = redis.call("GET", KEYS[1])
if not current then
  return 0
end
if current ~= ARGV[1] then
  return 2
end
redis.call("PEXPIRE", KEYS[1], ARGV[2])
return 1
`)

type RedisLockRepository struct {
	client goredis.UniversalClient
}

func NewRedisLockRepository(client goredis.UniversalClient) *RedisLockRepository {
	return &RedisLockRepository{client: client}
}

func (r *RedisLockRepository) Acquire(ctx context.Context, lock SeatLock, ttl time.Duration) (bool, error) {
	value, err := lockValue(lock)
	if err != nil {
		return false, err
	}
	return r.client.SetNX(ctx, lockKey(lock.ShowtimeID, lock.SeatNo), value, ttl).Result()
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

func (r *RedisLockRepository) GetMany(ctx context.Context, showtimeID string, seatNos []string) (map[string]SeatLock, error) {
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

func (r *RedisLockRepository) VerifyAndExtend(
	ctx context.Context,
	lock SeatLock,
	ttl time.Duration,
) (OwnershipResult, error) {
	value, err := lockValue(lock)
	if err != nil {
		return OwnershipMissing, err
	}
	result, err := verifyAndExtendScript.Run(
		ctx,
		r.client,
		[]string{lockKey(lock.ShowtimeID, lock.SeatNo)},
		value,
		ttl.Milliseconds(),
	).Int()
	if err != nil {
		return OwnershipMissing, err
	}
	return OwnershipResult(result), nil
}

func (r *RedisLockRepository) Release(ctx context.Context, lock SeatLock) (ReleaseResult, error) {
	value, err := lockValue(lock)
	if err != nil {
		return ReleaseMissing, err
	}
	result, err := releaseScript.Run(ctx, r.client, []string{lockKey(lock.ShowtimeID, lock.SeatNo)}, value).Int()
	if err != nil {
		return ReleaseMissing, err
	}
	return ReleaseResult(result), nil
}

func lockKey(showtimeID, seatNo string) string {
	return fmt.Sprintf("seat_lock:%s:%s", showtimeID, seatNo)
}

func lockValue(lock SeatLock) (string, error) {
	value := struct {
		UserID         string `json:"user_id"`
		OwnershipToken string `json:"ownership_token"`
	}{
		UserID:         lock.UserID,
		OwnershipToken: lock.OwnershipToken,
	}
	encoded, err := json.Marshal(value)
	return string(encoded), err
}

func decodeLock(showtimeID, seatNo, value string) (SeatLock, error) {
	var owner struct {
		UserID         string `json:"user_id"`
		OwnershipToken string `json:"ownership_token"`
	}
	if err := json.Unmarshal([]byte(value), &owner); err != nil {
		return SeatLock{}, fmt.Errorf("decode Redis lock: %w", err)
	}
	return SeatLock{
		ShowtimeID:     showtimeID,
		SeatNo:         seatNo,
		UserID:         owner.UserID,
		OwnershipToken: owner.OwnershipToken,
	}, nil
}
