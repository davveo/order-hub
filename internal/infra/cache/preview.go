package cache

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/davveo/order-hub/internal/application/port"
	"github.com/redis/go-redis/v9"
)

type PreviewStore struct {
	rdb *redis.Client
	mem *memPreview
}

func NewPreviewStore(rdb *redis.Client) *PreviewStore {
	s := &PreviewStore{rdb: rdb}
	if rdb == nil {
		s.mem = newMemPreview()
	}
	return s
}

func quoteKey(tenantID, quoteID string) string {
	return "oh:quote:" + tenantID + ":" + quoteID
}

func (s *PreviewStore) Put(ctx context.Context, tenantID, userID, quoteID string, snap port.PreviewSnapshot, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	body, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	if s.rdb == nil {
		s.mem.put(quoteKey(tenantID, quoteID), body, ttl)
		return nil
	}
	return s.rdb.Set(ctx, quoteKey(tenantID, quoteID), body, ttl).Err()
}

func (s *PreviewStore) GetByQuote(ctx context.Context, tenantID, quoteID string) (*port.PreviewSnapshot, error) {
	var raw []byte
	if s.rdb == nil {
		raw = s.mem.get(quoteKey(tenantID, quoteID))
		if raw == nil {
			return nil, errMiss
		}
	} else {
		val, err := s.rdb.Get(ctx, quoteKey(tenantID, quoteID)).Bytes()
		if err != nil {
			return nil, err
		}
		raw = val
	}
	var snap port.PreviewSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}

type RedisLocker struct {
	rdb *redis.Client
	mem *memLock
}

func NewLocker(rdb *redis.Client) *RedisLocker {
	l := &RedisLocker{rdb: rdb}
	if rdb == nil {
		l.mem = newMemLock()
	}
	return l
}

func (l *RedisLocker) TryLock(ctx context.Context, key string, ttl time.Duration) (func(), bool, error) {
	k := "oh:lock:" + key
	if l.rdb == nil {
		ok := l.mem.try(k, ttl)
		if !ok {
			return nil, false, nil
		}
		return func() { l.mem.del(k) }, true, nil
	}
	ok, err := l.rdb.SetNX(ctx, k, "1", ttl).Result()
	if err != nil {
		return nil, false, err
	}
	if !ok {
		return nil, false, nil
	}
	return func() { _ = l.rdb.Del(context.Background(), k).Err() }, true, nil
}

var errMiss = errCacheMiss{}

type errCacheMiss struct{}

func (errCacheMiss) Error() string { return "preview not found" }

type memPreview struct {
	mu   sync.Mutex
	data map[string]memItem
}

type memItem struct {
	raw []byte
	exp time.Time
}

func newMemPreview() *memPreview { return &memPreview{data: map[string]memItem{}} }

func (m *memPreview) put(k string, raw []byte, ttl time.Duration) {
	m.mu.Lock()
	m.data[k] = memItem{raw: raw, exp: time.Now().Add(ttl)}
	m.mu.Unlock()
}

func (m *memPreview) get(k string) []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	it, ok := m.data[k]
	if !ok || time.Now().After(it.exp) {
		delete(m.data, k)
		return nil
	}
	return it.raw
}

type memLock struct {
	mu   sync.Mutex
	data map[string]time.Time
}

func newMemLock() *memLock { return &memLock{data: map[string]time.Time{}} }

func (m *memLock) try(k string, ttl time.Duration) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if exp, ok := m.data[k]; ok && time.Now().Before(exp) {
		return false
	}
	m.data[k] = time.Now().Add(ttl)
	return true
}

func (m *memLock) del(k string) {
	m.mu.Lock()
	delete(m.data, k)
	m.mu.Unlock()
}
