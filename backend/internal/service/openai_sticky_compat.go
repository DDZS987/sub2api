package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/gin-gonic/gin"
)

type openAILegacySessionHashContextKey struct{}

var openAILegacySessionHashKey = openAILegacySessionHashContextKey{}

var (
	openAIStickyLegacyReadFallbackTotal atomic.Int64
	openAIStickyLegacyReadFallbackHit   atomic.Int64
	openAIStickyLegacyDualWriteTotal    atomic.Int64
)

func openAIStickyCompatStats() (legacyReadFallbackTotal, legacyReadFallbackHit, legacyDualWriteTotal int64) {
	return openAIStickyLegacyReadFallbackTotal.Load(),
		openAIStickyLegacyReadFallbackHit.Load(),
		openAIStickyLegacyDualWriteTotal.Load()
}

// DeriveSessionHashFromSeed computes the current-format sticky-session hash
// from an arbitrary seed string.
func DeriveSessionHashFromSeed(seed string) string {
	currentHash, _ := deriveOpenAISessionHashes(seed)
	return currentHash
}

func deriveOpenAISessionHashes(sessionID string) (currentHash string, legacyHash string) {
	normalized := strings.TrimSpace(sessionID)
	if normalized == "" {
		return "", ""
	}

	currentHash = fmt.Sprintf("%016x", xxhash.Sum64String(normalized))
	sum := sha256.Sum256([]byte(normalized))
	legacyHash = hex.EncodeToString(sum[:])
	return currentHash, legacyHash
}

func withOpenAILegacySessionHash(ctx context.Context, legacyHash string) context.Context {
	if ctx == nil {
		return nil
	}
	trimmed := strings.TrimSpace(legacyHash)
	if trimmed == "" {
		return ctx
	}
	return context.WithValue(ctx, openAILegacySessionHashKey, trimmed)
}

func openAILegacySessionHashFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(openAILegacySessionHashKey).(string)
	return strings.TrimSpace(value)
}

func attachOpenAILegacySessionHashToGin(c *gin.Context, legacyHash string) {
	if c == nil || c.Request == nil {
		return
	}
	c.Request = c.Request.WithContext(withOpenAILegacySessionHash(c.Request.Context(), legacyHash))
}

func (s *OpenAIGatewayService) openAISessionHashReadOldFallbackEnabled() bool {
	if s == nil || s.cfg == nil {
		return true
	}
	return s.cfg.Gateway.OpenAIWS.SessionHashReadOldFallback
}

func (s *OpenAIGatewayService) openAISessionHashDualWriteOldEnabled() bool {
	if s == nil || s.cfg == nil {
		return true
	}
	return s.cfg.Gateway.OpenAIWS.SessionHashDualWriteOld
}

func (s *OpenAIGatewayService) openAISessionCacheKey(sessionHash string) string {
	normalized := strings.TrimSpace(sessionHash)
	if normalized == "" {
		return ""
	}
	return "openai:" + normalized
}

func (s *OpenAIGatewayService) openAISessionAffinityCacheKey(sessionHash string, affinity OpenAIAccountAffinityDomain) string {
	baseKey := s.openAISessionCacheKey(sessionHash)
	if baseKey == "" {
		return ""
	}
	normalized := normalizeOpenAIAccountAffinity(affinity)
	if normalized == OpenAIAccountAffinityAny {
		return ""
	}
	return baseKey + ":affinity:" + string(normalized)
}

func (s *OpenAIGatewayService) openAISessionScopedCacheKeys(sessionHash string) []string {
	keys := make([]string, 0, 2)
	for _, affinity := range []OpenAIAccountAffinityDomain{OpenAIAccountAffinityOAuth, OpenAIAccountAffinityAPIKey} {
		if key := s.openAISessionAffinityCacheKey(sessionHash, affinity); key != "" {
			keys = append(keys, key)
		}
	}
	return keys
}

func (s *OpenAIGatewayService) openAISessionCacheKeyForContext(ctx context.Context, sessionHash string) string {
	if !OpenAIStickyAffinityScopeEnabled(ctx) {
		return s.openAISessionCacheKey(sessionHash)
	}
	return s.openAISessionAffinityCacheKey(sessionHash, OpenAIAccountAffinityFromContext(ctx))
}

func (s *OpenAIGatewayService) openAILegacySessionCacheKey(ctx context.Context, sessionHash string) string {
	if OpenAIStickyAffinityScopeEnabled(ctx) && OpenAIAccountAffinityFromContext(ctx) != OpenAIAccountAffinityAny {
		return ""
	}
	legacyHash := openAILegacySessionHashFromContext(ctx)
	if legacyHash == "" {
		return ""
	}
	legacyKey := "openai:" + legacyHash
	if legacyKey == s.openAISessionCacheKey(sessionHash) {
		return ""
	}
	return legacyKey
}

func (s *OpenAIGatewayService) openAIStickyLegacyTTL(ttl time.Duration) time.Duration {
	legacyTTL := ttl
	if legacyTTL <= 0 {
		legacyTTL = openaiStickySessionTTL
	}
	if legacyTTL > 10*time.Minute {
		return 10 * time.Minute
	}
	return legacyTTL
}

func (s *OpenAIGatewayService) getStickySessionAccountID(ctx context.Context, groupID *int64, sessionHash string) (int64, error) {
	if s == nil || s.cache == nil {
		return 0, nil
	}

	if OpenAIStickyAffinityScopeEnabled(ctx) {
		affinity := OpenAIAccountAffinityFromContext(ctx)
		if affinity != OpenAIAccountAffinityAny {
			primaryKey := s.openAISessionCacheKeyForContext(ctx, sessionHash)
			if primaryKey == "" {
				return 0, nil
			}
			accountID, err := s.cache.GetSessionAccountID(ctx, derefGroupID(groupID), primaryKey)
			if err == nil && accountID > 0 {
				slog.Debug("openai.sticky_affinity_read_hit",
					"group_id", derefGroupID(groupID),
					"session_hash", strings.TrimSpace(sessionHash),
					"account_id", accountID,
					"affinity", OpenAIAccountAffinityLogValue(affinity),
				)
			}
			return accountID, err
		}

		var matchedID int64
		var matchedAffinity OpenAIAccountAffinityDomain
		for _, affinity := range []OpenAIAccountAffinityDomain{OpenAIAccountAffinityOAuth, OpenAIAccountAffinityAPIKey} {
			key := s.openAISessionAffinityCacheKey(sessionHash, affinity)
			if key == "" {
				continue
			}
			accountID, err := s.cache.GetSessionAccountID(ctx, derefGroupID(groupID), key)
			if err != nil || accountID <= 0 {
				continue
			}
			if matchedID > 0 && matchedID != accountID {
				slog.Warn("openai.sticky_affinity_conflict",
					"group_id", derefGroupID(groupID),
					"session_hash", strings.TrimSpace(sessionHash),
					"first_account_id", matchedID,
					"first_affinity", OpenAIAccountAffinityLogValue(matchedAffinity),
					"conflict_account_id", accountID,
					"conflict_affinity", OpenAIAccountAffinityLogValue(affinity),
				)
				return 0, nil
			}
			matchedID = accountID
			matchedAffinity = affinity
		}
		if matchedID > 0 {
			slog.Debug("openai.sticky_affinity_read_hit",
				"group_id", derefGroupID(groupID),
				"session_hash", strings.TrimSpace(sessionHash),
				"account_id", matchedID,
				"affinity", OpenAIAccountAffinityLogValue(matchedAffinity),
			)
		}
		return matchedID, nil
	}

	primaryKey := s.openAISessionCacheKeyForContext(ctx, sessionHash)
	if primaryKey == "" {
		return 0, nil
	}

	accountID, err := s.cache.GetSessionAccountID(ctx, derefGroupID(groupID), primaryKey)
	if err == nil && accountID > 0 {
		return accountID, nil
	}
	if !s.openAISessionHashReadOldFallbackEnabled() {
		return accountID, err
	}

	legacyKey := s.openAILegacySessionCacheKey(ctx, sessionHash)
	if legacyKey == "" {
		return accountID, err
	}

	openAIStickyLegacyReadFallbackTotal.Add(1)
	legacyAccountID, legacyErr := s.cache.GetSessionAccountID(ctx, derefGroupID(groupID), legacyKey)
	if legacyErr == nil && legacyAccountID > 0 {
		openAIStickyLegacyReadFallbackHit.Add(1)
		return legacyAccountID, nil
	}
	return accountID, err
}

func (s *OpenAIGatewayService) setStickySessionAccountID(ctx context.Context, groupID *int64, sessionHash string, accountID int64, ttl time.Duration) error {
	if s == nil || s.cache == nil || accountID <= 0 {
		return nil
	}
	primaryKey := s.openAISessionCacheKeyForContext(ctx, sessionHash)
	if primaryKey == "" {
		return nil
	}

	if err := s.cache.SetSessionAccountID(ctx, derefGroupID(groupID), primaryKey, accountID, ttl); err != nil {
		return err
	}

	if OpenAIStickyAffinityScopeEnabled(ctx) {
		slog.Info("openai.sticky_affinity_bound",
			"group_id", derefGroupID(groupID),
			"session_hash", strings.TrimSpace(sessionHash),
			"account_id", accountID,
			"affinity", OpenAIAccountAffinityLogValue(OpenAIAccountAffinityFromContext(ctx)),
			"ttl_seconds", int64(ttl/time.Second),
		)
		return nil
	}
	if !s.openAISessionHashDualWriteOldEnabled() {
		return nil
	}
	legacyKey := s.openAILegacySessionCacheKey(ctx, sessionHash)
	if legacyKey == "" {
		return nil
	}
	if err := s.cache.SetSessionAccountID(ctx, derefGroupID(groupID), legacyKey, accountID, s.openAIStickyLegacyTTL(ttl)); err != nil {
		return err
	}
	openAIStickyLegacyDualWriteTotal.Add(1)
	return nil
}

func (s *OpenAIGatewayService) refreshStickySessionTTL(ctx context.Context, groupID *int64, sessionHash string, ttl time.Duration) error {
	if s == nil || s.cache == nil {
		return nil
	}
	primaryKey := s.openAISessionCacheKeyForContext(ctx, sessionHash)
	if primaryKey == "" {
		if OpenAIStickyAffinityScopeEnabled(ctx) {
			var err error
			for _, key := range s.openAISessionScopedCacheKeys(sessionHash) {
				if e := s.cache.RefreshSessionTTL(ctx, derefGroupID(groupID), key, ttl); err == nil && e != nil {
					err = e
				}
			}
			return err
		}
		return nil
	}

	err := s.cache.RefreshSessionTTL(ctx, derefGroupID(groupID), primaryKey, ttl)
	if !s.openAISessionHashReadOldFallbackEnabled() && !s.openAISessionHashDualWriteOldEnabled() {
		return err
	}

	legacyKey := s.openAILegacySessionCacheKey(ctx, sessionHash)
	if legacyKey != "" {
		_ = s.cache.RefreshSessionTTL(ctx, derefGroupID(groupID), legacyKey, s.openAIStickyLegacyTTL(ttl))
	}
	return err
}

func (s *OpenAIGatewayService) deleteStickySessionAccountID(ctx context.Context, groupID *int64, sessionHash string) error {
	if s == nil || s.cache == nil {
		return nil
	}
	primaryKey := s.openAISessionCacheKeyForContext(ctx, sessionHash)
	if primaryKey == "" {
		if OpenAIStickyAffinityScopeEnabled(ctx) {
			var err error
			for _, key := range s.openAISessionScopedCacheKeys(sessionHash) {
				if e := s.cache.DeleteSessionAccountID(ctx, derefGroupID(groupID), key); err == nil && e != nil {
					err = e
				}
			}
			return err
		}
		return nil
	}

	err := s.cache.DeleteSessionAccountID(ctx, derefGroupID(groupID), primaryKey)
	if !s.openAISessionHashReadOldFallbackEnabled() && !s.openAISessionHashDualWriteOldEnabled() {
		return err
	}

	legacyKey := s.openAILegacySessionCacheKey(ctx, sessionHash)
	if legacyKey != "" {
		_ = s.cache.DeleteSessionAccountID(ctx, derefGroupID(groupID), legacyKey)
	}
	return err
}
