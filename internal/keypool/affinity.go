package keypool

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"gpt-load/internal/models"
	"gpt-load/internal/store"

	"github.com/sirupsen/logrus"
)

const (
	affinityBindPrefix     = "affinity:bind:"
	affinityCooldownPrefix = "affinity:cd:"
	affinityBackoffPrefix  = "affinity:bo:"
	affinityVideoPrefix    = "affinity:video:"
	defaultCooldown        = time.Second
	maxCooldown            = 30 * time.Second
)

// SelectOptions 控制亲和选 Key。
type SelectOptions struct {
	EnableAffinity    bool
	SessionID         string
	Model             string
	TTL               time.Duration
	VideoID           string
	RequireVideoBound bool
}

func bindKey(groupID uint, sessionID, model string) string {
	return affinityBindPrefix + fmt.Sprintf("%d:%s:%s", groupID, sessionID, model)
}

func cooldownKey(groupID, keyID uint) string {
	return affinityCooldownPrefix + fmt.Sprintf("%d:%d", groupID, keyID)
}

func backoffKey(groupID, keyID uint) string {
	return affinityBackoffPrefix + fmt.Sprintf("%d:%d", groupID, keyID)
}

func videoKey(groupID uint, videoID string) string {
	return affinityVideoPrefix + fmt.Sprintf("%d:%s", groupID, videoID)
}

func (p *KeyProvider) getBoundKeyID(cacheKey string) (uint, bool) {
	raw, err := p.store.Get(cacheKey)
	if err != nil {
		return 0, false
	}
	id, err := strconv.ParseUint(string(raw), 10, 64)
	if err != nil {
		return 0, false
	}
	return uint(id), true
}

func (p *KeyProvider) setBoundKeyID(cacheKey string, keyID uint, ttl time.Duration) {
	if ttl <= 0 {
		ttl = time.Hour
	}
	if err := p.store.Set(cacheKey, []byte(strconv.FormatUint(uint64(keyID), 10)), ttl); err != nil {
		logrus.WithError(err).Debug("affinity cache write failed, falling back to rotation")
	}
}

func (p *KeyProvider) IsCooling(groupID, keyID uint) bool {
	ok, err := p.store.Exists(cooldownKey(groupID, keyID))
	return err == nil && ok
}

// MarkCooldown 将 Key 标为 429 冷却。优先使用 retryAfter。
func (p *KeyProvider) MarkCooldown(groupID, keyID uint, retryAfter time.Duration) time.Duration {
	ttl := retryAfter
	if ttl <= 0 {
		level := p.nextBackoffLevel(groupID, keyID)
		ttl = defaultCooldown * time.Duration(1<<level)
		if ttl > maxCooldown {
			ttl = maxCooldown
		}
	}
	if err := p.store.Set(cooldownKey(groupID, keyID), []byte("1"), ttl); err != nil {
		logrus.WithError(err).Debug("failed to persist 429 cooldown")
	}
	return ttl
}

func (p *KeyProvider) nextBackoffLevel(groupID, keyID uint) int {
	raw, err := p.store.Get(backoffKey(groupID, keyID))
	level := 0
	if err == nil {
		if n, parseErr := strconv.Atoi(string(raw)); parseErr == nil {
			level = n + 1
		}
	}
	_ = p.store.Set(backoffKey(groupID, keyID), []byte(strconv.Itoa(level)), time.Hour)
	return level
}

// BindVideo 把视频任务粘到创建 Key。
func (p *KeyProvider) BindVideo(groupID, keyID uint, videoID string, ttl time.Duration) {
	videoID = strings.TrimSpace(videoID)
	if videoID == "" {
		return
	}
	p.setBoundKeyID(videoKey(groupID, videoID), keyID, ttl)
}

func (p *KeyProvider) loadKeyByID(groupID, keyID uint) (*models.APIKey, error) {
	keyHashKey := fmt.Sprintf("key:%d", keyID)
	keyDetails, err := p.store.HGetAll(keyHashKey)
	if err != nil {
		return nil, err
	}
	if len(keyDetails) == 0 {
		return nil, store.ErrNotFound
	}
	failureCount, _ := strconv.ParseInt(keyDetails["failure_count"], 10, 64)
	createdAt, _ := strconv.ParseInt(keyDetails["created_at"], 10, 64)
	encryptedKeyValue := keyDetails["key_string"]
	decryptedKeyValue, err := p.encryptionSvc.Decrypt(encryptedKeyValue)
	if err != nil {
		decryptedKeyValue = encryptedKeyValue
	}
	return &models.APIKey{
		ID:           keyID,
		KeyValue:     decryptedKeyValue,
		Status:       keyDetails["status"],
		FailureCount: failureCount,
		GroupID:      groupID,
		CreatedAt:    time.Unix(createdAt, 0),
	}, nil
}

func (p *KeyProvider) keyUsable(groupID uint, key *models.APIKey) bool {
	if key == nil {
		return false
	}
	if key.Status != "" && key.Status != models.KeyStatusActive {
		return false
	}
	return !p.IsCooling(groupID, key.ID)
}

// SelectKeyWithOptions 在可选亲和/冷却约束下选 Key。
func (p *KeyProvider) SelectKeyWithOptions(groupID uint, opts SelectOptions) (*models.APIKey, error) {
	if opts.RequireVideoBound {
		id, ok := p.getBoundKeyID(videoKey(groupID, opts.VideoID))
		if !ok {
			return nil, fmt.Errorf("视频任务未绑定可用 Key")
		}
		key, err := p.loadKeyByID(groupID, id)
		if err != nil || !p.keyUsable(groupID, key) {
			return nil, fmt.Errorf("视频任务绑定的 Key 不可用")
		}
		return key, nil
	}

	if opts.EnableAffinity && opts.SessionID != "" {
		if id, ok := p.getBoundKeyID(bindKey(groupID, opts.SessionID, opts.Model)); ok {
			key, err := p.loadKeyByID(groupID, id)
			if err == nil && p.keyUsable(groupID, key) {
				p.setBoundKeyID(bindKey(groupID, opts.SessionID, opts.Model), key.ID, opts.TTL)
				return key, nil
			}
		}
	}

	var last *models.APIKey
	for range 16 {
		key, err := p.SelectKey(groupID)
		if err != nil {
			return last, err
		}
		last = key
		if !p.IsCooling(groupID, key.ID) {
			if opts.EnableAffinity && opts.SessionID != "" {
				p.setBoundKeyID(bindKey(groupID, opts.SessionID, opts.Model), key.ID, opts.TTL)
			}
			return key, nil
		}
	}
	if last != nil {
		return last, nil
	}
	return nil, fmt.Errorf("no usable keys")
}
