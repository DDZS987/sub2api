package service

import (
	"context"
	"strings"
)

type OpenAIAccountAffinityDomain string

const (
	OpenAIAccountAffinityAny    OpenAIAccountAffinityDomain = ""
	OpenAIAccountAffinityOAuth  OpenAIAccountAffinityDomain = "oauth"
	OpenAIAccountAffinityAPIKey OpenAIAccountAffinityDomain = "apikey"
)

type openAIAccountAffinityContextKey struct{}
type openAIStickyAffinityScopeContextKey struct{}

var openAIAccountAffinityKey = openAIAccountAffinityContextKey{}
var openAIStickyAffinityScopeKey = openAIStickyAffinityScopeContextKey{}

func WithOpenAIAccountAffinity(ctx context.Context, affinity OpenAIAccountAffinityDomain) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	normalized := normalizeOpenAIAccountAffinity(affinity)
	if normalized == OpenAIAccountAffinityAny {
		return ctx
	}
	return context.WithValue(ctx, openAIAccountAffinityKey, normalized)
}

func WithoutOpenAIAccountAffinity(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, openAIAccountAffinityKey, OpenAIAccountAffinityAny)
}

func OpenAIAccountAffinityFromContext(ctx context.Context) OpenAIAccountAffinityDomain {
	if ctx == nil {
		return OpenAIAccountAffinityAny
	}
	if affinity, ok := ctx.Value(openAIAccountAffinityKey).(OpenAIAccountAffinityDomain); ok {
		return normalizeOpenAIAccountAffinity(affinity)
	}
	if affinity, ok := ctx.Value(openAIAccountAffinityKey).(string); ok {
		return normalizeOpenAIAccountAffinity(OpenAIAccountAffinityDomain(affinity))
	}
	return OpenAIAccountAffinityAny
}

func WithOpenAIStickyAffinityScope(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, openAIStickyAffinityScopeKey, true)
}

func OpenAIStickyAffinityScopeEnabled(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	enabled, _ := ctx.Value(openAIStickyAffinityScopeKey).(bool)
	return enabled
}

func OpenAIAccountAffinityForAccount(account *Account) OpenAIAccountAffinityDomain {
	if account == nil || !account.IsOpenAI() {
		return OpenAIAccountAffinityAny
	}
	if account.IsOAuth() {
		return OpenAIAccountAffinityOAuth
	}
	if account.Type == AccountTypeAPIKey || account.Type == AccountTypeUpstream {
		return OpenAIAccountAffinityAPIKey
	}
	return OpenAIAccountAffinityAny
}

func OpenAIAccountMatchesAffinity(account *Account, affinity OpenAIAccountAffinityDomain) bool {
	normalized := normalizeOpenAIAccountAffinity(affinity)
	if normalized == OpenAIAccountAffinityAny {
		return true
	}
	return OpenAIAccountAffinityForAccount(account) == normalized
}

func OpenAIAccountAffinityLogValue(affinity OpenAIAccountAffinityDomain) string {
	normalized := normalizeOpenAIAccountAffinity(affinity)
	if normalized == OpenAIAccountAffinityAny {
		return "any"
	}
	return string(normalized)
}

func withOpenAIAccountAffinityForAccount(ctx context.Context, account *Account) context.Context {
	if OpenAIAccountAffinityFromContext(ctx) != OpenAIAccountAffinityAny {
		return ctx
	}
	affinity := OpenAIAccountAffinityForAccount(account)
	if affinity == OpenAIAccountAffinityAny {
		return ctx
	}
	return WithOpenAIAccountAffinity(ctx, affinity)
}

func normalizeOpenAIAccountAffinity(affinity OpenAIAccountAffinityDomain) OpenAIAccountAffinityDomain {
	switch strings.ToLower(strings.TrimSpace(string(affinity))) {
	case string(OpenAIAccountAffinityOAuth):
		return OpenAIAccountAffinityOAuth
	case string(OpenAIAccountAffinityAPIKey), "api_key", "relay", "upstream":
		return OpenAIAccountAffinityAPIKey
	default:
		return OpenAIAccountAffinityAny
	}
}
