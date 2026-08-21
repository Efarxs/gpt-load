package keypool

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	app_errors "gpt-load/internal/errors"
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

// AffinityBinding 入口组上的会话绑定：Key + 上游 + 子分组。
type AffinityBinding struct {
	KeyID       uint   `json:"key_id"`
	UpstreamIdx int    `json:"upstream_idx"`
	BaseURL     string `json:"base_url"`
	SubGroup    string `json:"sub_group"`
}

// SelectOptions 控制亲和选 Key。
type SelectOptions struct {
	EnableAffinity    bool
	SessionID         string
	Model             string
	TTL               time.Duration
	VideoID           string
	RequireVideoBound bool
	MaxConcurrency    int
	RequestTimeout    time.Duration
	EntryGroupID      uint
	SubGroup          string
	UpstreamIdx       int
	UpstreamBaseURL   string
	SkipBindWrite     bool
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

func bindGroupID(groupID uint, opts SelectOptions) uint {
	if opts.EntryGroupID != 0 {
		return opts.EntryGroupID
	}
	return groupID
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

// GetBinding 读取入口组上的 JSON 绑定。旧的纯数字缓存视为未命中。
func (p *KeyProvider) GetBinding(groupID uint, sessionID, model string) (*AffinityBinding, bool) {
	raw, err := p.store.Get(bindKey(groupID, sessionID, model))
	if err != nil {
		return nil, false
	}
	var b AffinityBinding
	if err := json.Unmarshal(raw, &b); err != nil || b.KeyID == 0 {
		return nil, false
	}
	return &b, true
}

// SetBinding 写入或续期会话绑定。
func (p *KeyProvider) SetBinding(groupID uint, sessionID, model string, b AffinityBinding, ttl time.Duration) {
	if sessionID == "" || b.KeyID == 0 {
		return
	}
	if ttl <= 0 {
		ttl = time.Hour
	}
	raw, err := json.Marshal(b)
	if err != nil {
		logrus.WithError(err).Debug("affinity binding marshal failed")
		return
	}
	if err := p.store.Set(bindKey(groupID, sessionID, model), raw, ttl); err != nil {
		logrus.WithError(err).Debug("affinity cache write failed, falling back to rotation")
	}
}

// DeleteBinding 删除入口组上的会话绑定。
func (p *KeyProvider) DeleteBinding(groupID uint, sessionID, model string) {
	_ = p.store.Delete(bindKey(groupID, sessionID, model))
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

// GetKeyByID 读取一把 Key 的缓存详情。
func (p *KeyProvider) GetKeyByID(groupID, keyID uint) (*models.APIKey, error) {
	return p.loadKeyByID(groupID, keyID)
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

func (p *KeyProvider) tryAcquire(groupID uint, key *models.APIKey, opts SelectOptions) (bool, error) {
	if key == nil {
		return false, nil
	}
	return p.AcquireKey(groupID, key.ID, opts.MaxConcurrency, opts.RequestTimeout)
}

func (p *KeyProvider) writeBinding(groupID uint, key *models.APIKey, opts SelectOptions, existing *AffinityBinding) {
	if opts.SkipBindWrite || !opts.EnableAffinity || opts.SessionID == "" || key == nil {
		return
	}
	b := AffinityBinding{
		KeyID:       key.ID,
		UpstreamIdx: opts.UpstreamIdx,
		BaseURL:     opts.UpstreamBaseURL,
		SubGroup:    opts.SubGroup,
	}
	if existing != nil && existing.KeyID == key.ID {
		if b.BaseURL == "" {
			b.UpstreamIdx = existing.UpstreamIdx
			b.BaseURL = existing.BaseURL
		}
		if b.SubGroup == "" {
			b.SubGroup = existing.SubGroup
		}
	}
	p.SetBinding(bindGroupID(groupID, opts), opts.SessionID, opts.Model, b, opts.TTL)
}

// SelectKeyWithOptions 在可选亲和/冷却/并发约束下选 Key。
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
		acquired, acqErr := p.tryAcquire(groupID, key, opts)
		if acqErr != nil {
			return nil, acqErr
		}
		if !acquired {
			return nil, app_errors.ErrNoActiveKeys
		}
		return key, nil
	}

	var existing *AffinityBinding
	capacitySkip := false
	if opts.EnableAffinity && opts.SessionID != "" {
		if b, ok := p.GetBinding(bindGroupID(groupID, opts), opts.SessionID, opts.Model); ok {
			existing = b
			key, err := p.loadKeyByID(groupID, b.KeyID)
			if err == nil && p.keyUsable(groupID, key) {
				acquired, acqErr := p.tryAcquire(groupID, key, opts)
				if acqErr != nil {
					return nil, acqErr
				}
				if acquired {
					p.writeBinding(groupID, key, opts, existing)
					return key, nil
				}
				capacitySkip = true
			}
		}
	}

	attempts := int64(16)
	if n, err := p.store.LLen(fmt.Sprintf("group:%d:active_keys", groupID)); err == nil && n > 0 {
		attempts = n
	}

	var last *models.APIKey
	for range attempts {
		key, err := p.SelectKey(groupID)
		if err != nil {
			if last != nil && opts.MaxConcurrency <= 0 {
				return last, err
			}
			return last, err
		}
		last = key
		if !p.keyUsable(groupID, key) {
			continue
		}
		acquired, acqErr := p.tryAcquire(groupID, key, opts)
		if acqErr != nil {
			return nil, acqErr
		}
		if !acquired {
			continue
		}
		if !capacitySkip {
			p.writeBinding(groupID, key, opts, existing)
		}
		return key, nil
	}

	if opts.MaxConcurrency > 0 {
		return nil, app_errors.ErrNoActiveKeys
	}
	if last != nil {
		return last, nil
	}
	return nil, fmt.Errorf("no usable keys")
}
