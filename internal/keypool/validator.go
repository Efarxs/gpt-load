package keypool

import (
	"context"
	"fmt"
	"gpt-load/internal/channel"
	"gpt-load/internal/config"
	"gpt-load/internal/encryption"
	"gpt-load/internal/models"
	"time"

	"github.com/sirupsen/logrus"
	"go.uber.org/dig"
	"gorm.io/gorm"
)

// KeyTestResult holds the validation result for a single key.
type KeyTestResult struct {
	KeyValue     string `json:"key_value"`
	IsValid      bool   `json:"is_valid"`
	Error        string `json:"error,omitempty"`
	StatusCode   int    `json:"status_code,omitempty"`
	ResponseBody string `json:"response_body,omitempty"`
}

// KeyValidator provides methods to validate API keys.
type KeyValidator struct {
	DB              *gorm.DB
	channelFactory  *channel.Factory
	SettingsManager *config.SystemSettingsManager
	keypoolProvider *KeyProvider
	encryptionSvc   encryption.Service
}

type KeyValidatorParams struct {
	dig.In
	DB              *gorm.DB
	ChannelFactory  *channel.Factory
	SettingsManager *config.SystemSettingsManager
	KeypoolProvider *KeyProvider
	EncryptionSvc   encryption.Service
}

// NewKeyValidator creates a new KeyValidator.
func NewKeyValidator(params KeyValidatorParams) *KeyValidator {
	return &KeyValidator{
		DB:              params.DB,
		channelFactory:  params.ChannelFactory,
		SettingsManager: params.SettingsManager,
		keypoolProvider: params.KeypoolProvider,
		encryptionSvc:   params.EncryptionSvc,
	}
}

// ValidateSingleKey performs a validation check on a single API key.
func (s *KeyValidator) ValidateSingleKey(key *models.APIKey, group *models.Group) (bool, error) {
	result := s.probeAndUpdate(key, group)
	if !result.IsValid {
		if result.Error != "" {
			return false, fmt.Errorf("%s", result.Error)
		}
		return false, fmt.Errorf("key is invalid")
	}
	return true, nil
}

func (s *KeyValidator) probeAndUpdate(key *models.APIKey, group *models.Group) KeyTestResult {
	if group.EffectiveConfig.AppUrl == "" {
		group.EffectiveConfig = s.SettingsManager.GetEffectiveConfig(group.Config)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(group.EffectiveConfig.KeyValidationTimeoutSeconds)*time.Second)
	defer cancel()

	result := KeyTestResult{KeyValue: key.KeyValue}
	ch, err := s.channelFactory.GetChannel(group)
	if err != nil {
		result.Error = fmt.Sprintf("failed to get channel for group %s: %v", group.Name, err)
		return result
	}

	probe := ch.ValidateKey(ctx, key, group)
	result.IsValid = probe.Valid
	result.StatusCode = probe.StatusCode
	result.ResponseBody = probe.Body
	if probe.Err != nil {
		result.Error = probe.Err.Error()
	}

	var errorMsg string
	if !probe.Valid && probe.Err != nil {
		errorMsg = probe.Err.Error()
	}
	s.keypoolProvider.UpdateStatus(key, group, probe.Valid, errorMsg)

	if !probe.Valid {
		logrus.WithFields(logrus.Fields{
			"error":    probe.Err,
			"key_id":   key.ID,
			"group_id": group.ID,
		}).Debug("Key validation failed")
		return result
	}

	logrus.WithFields(logrus.Fields{
		"key_id":   key.ID,
		"is_valid": probe.Valid,
	}).Debug("Key validation successful")

	return result
}

// TestMultipleKeys performs a synchronous validation for a list of key values within a specific group.
func (s *KeyValidator) TestMultipleKeys(group *models.Group, keyValues []string) ([]KeyTestResult, error) {
	results := make([]KeyTestResult, len(keyValues))

	// Generate hashes for all key values
	var keyHashes []string
	for _, keyValue := range keyValues {
		keyHash := s.encryptionSvc.Hash(keyValue)
		if keyHash == "" {
			continue
		}
		keyHashes = append(keyHashes, keyHash)
	}

	// Find which of the provided keys actually exist in the database for this group
	var existingKeys []models.APIKey
	if len(keyHashes) > 0 {
		if err := s.DB.Where("group_id = ? AND key_hash IN ?", group.ID, keyHashes).Find(&existingKeys).Error; err != nil {
			return nil, fmt.Errorf("failed to query keys from DB: %w", err)
		}
	}

	// Create a map of key_hash to APIKey for quick lookup
	existingKeyMap := make(map[string]models.APIKey)
	for _, k := range existingKeys {
		existingKeyMap[k.KeyHash] = k
	}

	for i, kv := range keyValues {
		keyHash := s.encryptionSvc.Hash(kv)
		apiKey, exists := existingKeyMap[keyHash]
		if !exists {
			results[i] = KeyTestResult{
				KeyValue: kv,
				IsValid:  false,
				Error:    "Key does not exist in this group or has been removed.",
			}
			continue
		}

		apiKey.KeyValue = kv

		results[i] = s.probeAndUpdate(&apiKey, group)
		results[i].KeyValue = kv
	}

	return results, nil
}
