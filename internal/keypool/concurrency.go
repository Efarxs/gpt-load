package keypool

import (
	"fmt"
	"strconv"
	"time"

	"github.com/sirupsen/logrus"
)

const minInflightTTL = 60 * time.Second

func inflightHashKey(groupID uint) string {
	return fmt.Sprintf("group:%d:inflight", groupID)
}

func inflightExpKey(groupID, keyID uint) string {
	return fmt.Sprintf("group:%d:inflight:exp:%d", groupID, keyID)
}

func inflightTTL(requestTimeout time.Duration) time.Duration {
	if requestTimeout < minInflightTTL {
		return minInflightTTL
	}
	return requestTimeout
}

// AcquireKey 占用一把 Key 的在途槽位。maxConc<=0 时不碰 store。
func (p *KeyProvider) AcquireKey(groupID, keyID uint, maxConc int, requestTimeout time.Duration) (bool, error) {
	if maxConc <= 0 {
		return true, nil
	}
	field := strconv.FormatUint(uint64(keyID), 10)
	ok, err := p.store.Exists(inflightExpKey(groupID, keyID))
	if err != nil {
		return false, fmt.Errorf("failed to check inflight ttl for key %d: %w", keyID, err)
	}
	if !ok {
		if err := p.store.HSet(inflightHashKey(groupID), map[string]any{field: 0}); err != nil {
			return false, fmt.Errorf("failed to reset leaked inflight for key %d: %w", keyID, err)
		}
	}

	count, err := p.store.HIncrBy(inflightHashKey(groupID), field, 1)
	if err != nil {
		return false, fmt.Errorf("failed to acquire key %d: %w", keyID, err)
	}
	if count > int64(maxConc) {
		if _, decrErr := p.store.HIncrBy(inflightHashKey(groupID), field, -1); decrErr != nil {
			logrus.WithError(decrErr).Errorf("failed to roll back over-limit slot for key %d", keyID)
		}
		return false, nil
	}
	if err := p.store.Set(inflightExpKey(groupID, keyID), []byte("1"), inflightTTL(requestTimeout)); err != nil {
		logrus.WithError(err).Errorf("failed to refresh inflight ttl for key %d", keyID)
	}
	return true, nil
}

// ReleaseKey 释放在途槽位。maxConc<=0 时为空操作；计数不会落到负数。
func (p *KeyProvider) ReleaseKey(groupID, keyID uint, maxConc int) {
	if maxConc <= 0 {
		return
	}
	field := strconv.FormatUint(uint64(keyID), 10)
	count, err := p.store.HIncrBy(inflightHashKey(groupID), field, -1)
	if err != nil {
		logrus.WithError(err).Errorf("failed to release key %d", keyID)
		return
	}
	if count < 0 {
		if err := p.store.HSet(inflightHashKey(groupID), map[string]any{field: 0}); err != nil {
			logrus.WithError(err).Errorf("failed to reset negative inflight for key %d", keyID)
		}
	}
}
