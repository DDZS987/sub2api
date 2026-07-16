package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

const (
	GroupAccountAvailabilityAvailable       = "available"
	GroupAccountAvailabilityError           = "error"
	GroupAccountAvailabilityRateLimited     = "rate_limited"
	GroupAccountAvailabilityOverloaded      = "overloaded"
	GroupAccountAvailabilityTempUnavailable = "temp_unavailable"
	GroupAccountAvailabilityUnschedulable   = "unschedulable"
	GroupAccountAvailabilityQuotaExhausted  = "quota_exhausted"
	GroupAccountAvailabilityUnknown         = "unknown"

	groupStatusUsageStaleAfter = 15 * time.Minute
	groupStatusMaxAccountsScan = 200
)

var ErrGroupStatusForbidden = infraerrors.Forbidden("GROUP_STATUS_FORBIDDEN", "user cannot view this group status")

var groupStatusSensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/=-]+`),
	regexp.MustCompile(`sk-[A-Za-z0-9_-]{16,}`),
	regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`),
}

type GroupStatusService struct {
	apiKeyService  *APIKeyService
	accountService *AccountService
	groupService   *GroupService
}

type GroupStatusResponse struct {
	Groups []GroupStatus `json:"groups"`
}

type GroupStatus struct {
	Group     GroupStatusGroup     `json:"group"`
	Accounts  []GroupAccountStatus `json:"accounts"`
	Summary   GroupStatusSummary   `json:"summary"`
	Fallback  GroupStatusFallback  `json:"fallback"`
	UpdatedAt *time.Time           `json:"updated_at,omitempty"`
}

type GroupStatusGroup struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
}

type GroupStatusSummary struct {
	TotalAccounts     int `json:"total_accounts"`
	AvailableAccounts int `json:"available_accounts"`
	ErrorAccounts     int `json:"error_accounts"`
	StaleAccounts     int `json:"stale_accounts"`
}

type GroupStatusFallback struct {
	Reserved bool   `json:"reserved"`
	Ready    bool   `json:"ready"`
	Active   bool   `json:"active"`
	Mode     string `json:"mode,omitempty"`
}

type GroupAccountStatus struct {
	Label        string             `json:"label"`
	Platform     string             `json:"platform"`
	Type         string             `json:"type"`
	Status       string             `json:"status"`
	Schedulable  bool               `json:"schedulable"`
	Availability string             `json:"availability"`
	Error        *GroupAccountError `json:"error,omitempty"`
	Quota        GroupAccountQuota  `json:"quota"`
	UpdatedAt    *time.Time         `json:"updated_at,omitempty"`
	LastUsedAt   *time.Time         `json:"last_used_at,omitempty"`
}

type GroupAccountError struct {
	Message string     `json:"message"`
	Until   *time.Time `json:"until,omitempty"`
}

type GroupAccountQuota struct {
	Source    string            `json:"source,omitempty"`
	UpdatedAt *time.Time        `json:"updated_at,omitempty"`
	Stale     bool              `json:"stale"`
	FiveHour  *QuotaWindowState `json:"five_hour,omitempty"`
	SevenDay  *QuotaWindowState `json:"seven_day,omitempty"`
}

type QuotaWindowState struct {
	UsedPercent      float64    `json:"used_percent"`
	RemainingPercent float64    `json:"remaining_percent"`
	ResetAt          *time.Time `json:"reset_at,omitempty"`
}

func NewGroupStatusService(apiKeyService *APIKeyService, accountService *AccountService, groupService *GroupService) *GroupStatusService {
	return &GroupStatusService{
		apiKeyService:  apiKeyService,
		accountService: accountService,
		groupService:   groupService,
	}
}

func (s *GroupStatusService) ListForUser(ctx context.Context, userID int64) (*GroupStatusResponse, error) {
	groupIDs, err := s.userVisibleGroupIDs(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(groupIDs) == 0 {
		return &GroupStatusResponse{Groups: []GroupStatus{}}, nil
	}

	out := make([]GroupStatus, 0, len(groupIDs))
	for _, groupID := range groupIDs {
		status, err := s.GetForUser(ctx, userID, groupID)
		if err != nil {
			if infraerrors.Code(err) == 403 || infraerrors.Code(err) == 404 {
				continue
			}
			return nil, err
		}
		if len(status.Accounts) > 0 {
			out = append(out, *status)
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Group.Name == out[j].Group.Name {
			return out[i].Group.ID < out[j].Group.ID
		}
		return out[i].Group.Name < out[j].Group.Name
	})
	return &GroupStatusResponse{Groups: out}, nil
}

func (s *GroupStatusService) GetForUser(ctx context.Context, userID, groupID int64) (*GroupStatus, error) {
	if groupID <= 0 {
		return nil, ErrGroupNotFound
	}
	if err := s.ensureUserCanViewGroup(ctx, userID, groupID); err != nil {
		return nil, err
	}

	group, err := s.groupService.GetByID(ctx, groupID)
	if err != nil {
		return nil, err
	}
	accounts, err := s.listVisibleAccounts(ctx, groupID)
	if err != nil {
		return nil, err
	}

	out := &GroupStatus{
		Group: GroupStatusGroup{
			ID:       group.ID,
			Name:     group.Name,
			Platform: group.Platform,
		},
		Accounts: make([]GroupAccountStatus, 0, len(accounts)),
		Fallback: GroupStatusFallback{
			Reserved: true,
			Ready:    false,
			Active:   false,
			Mode:     "reserved",
		},
	}

	for i := range accounts {
		account := accounts[i]
		if account.Platform != PlatformOpenAI || account.Type != AccountTypeOAuth {
			continue
		}
		item := buildGroupAccountStatus(&account)
		out.Accounts = append(out.Accounts, item)
		out.Summary.TotalAccounts++
		if item.Availability == GroupAccountAvailabilityAvailable {
			out.Summary.AvailableAccounts++
		}
		if item.Availability == GroupAccountAvailabilityError || item.Error != nil {
			out.Summary.ErrorAccounts++
		}
		if item.Quota.Stale {
			out.Summary.StaleAccounts++
		}
		if item.UpdatedAt != nil && (out.UpdatedAt == nil || item.UpdatedAt.After(*out.UpdatedAt)) {
			ts := *item.UpdatedAt
			out.UpdatedAt = &ts
		}
	}

	sort.SliceStable(out.Accounts, func(i, j int) bool {
		return out.Accounts[i].Label < out.Accounts[j].Label
	})
	return out, nil
}

func (s *GroupStatusService) ensureUserCanViewGroup(ctx context.Context, userID, groupID int64) error {
	groupIDs, err := s.userVisibleGroupIDs(ctx, userID)
	if err != nil {
		return err
	}
	for _, visibleGroupID := range groupIDs {
		if visibleGroupID == groupID {
			return nil
		}
	}
	return ErrGroupStatusForbidden
}

func (s *GroupStatusService) userVisibleGroupIDs(ctx context.Context, userID int64) ([]int64, error) {
	return s.apiKeyService.GetUserAssignedGroupIDs(ctx, userID)
}

func (s *GroupStatusService) listVisibleAccounts(ctx context.Context, groupID int64) ([]Account, error) {
	params := pagination.PaginationParams{
		Page:      1,
		PageSize:  groupStatusMaxAccountsScan,
		SortBy:    "priority",
		SortOrder: pagination.SortOrderAsc,
	}

	statuses := []string{
		StatusActive,
		"rate_limited",
		"temp_unschedulable",
		"unschedulable",
		StatusError,
	}
	seen := make(map[int64]struct{})
	out := make([]Account, 0, 4)
	for _, status := range statuses {
		accounts, _, err := s.accountService.ListWithFilters(ctx, params, PlatformOpenAI, AccountTypeOAuth, status, "", groupID, "")
		if err != nil {
			return nil, err
		}
		for i := range accounts {
			if _, ok := seen[accounts[i].ID]; ok {
				continue
			}
			seen[accounts[i].ID] = struct{}{}
			out = append(out, accounts[i])
		}
	}
	return out, nil
}

func buildGroupAccountStatus(account *Account) GroupAccountStatus {
	now := time.Now()
	quota := buildGroupAccountQuota(account, now)
	availability, errInfo := buildGroupAccountAvailability(account, quota, now)

	return GroupAccountStatus{
		Label:        sanitizeGroupAccountLabel(account.Name),
		Platform:     account.Platform,
		Type:         account.Type,
		Status:       account.Status,
		Schedulable:  account.Schedulable,
		Availability: availability,
		Error:        errInfo,
		Quota:        quota,
		UpdatedAt:    quota.UpdatedAt,
		LastUsedAt:   account.LastUsedAt,
	}
}

func buildGroupAccountAvailability(account *Account, quota GroupAccountQuota, now time.Time) (string, *GroupAccountError) {
	if account.Status == StatusError {
		return GroupAccountAvailabilityError, buildAccountError(account.ErrorMessage, nil)
	}
	if account.RateLimitResetAt != nil && now.Before(*account.RateLimitResetAt) {
		return GroupAccountAvailabilityRateLimited, buildAccountError("rate limited", account.RateLimitResetAt)
	}
	if account.OverloadUntil != nil && now.Before(*account.OverloadUntil) {
		return GroupAccountAvailabilityOverloaded, buildAccountError("overloaded", account.OverloadUntil)
	}
	if account.TempUnschedulableUntil != nil && now.Before(*account.TempUnschedulableUntil) {
		return GroupAccountAvailabilityTempUnavailable, buildAccountError(account.TempUnschedulableReason, account.TempUnschedulableUntil)
	}
	if !account.Schedulable {
		return GroupAccountAvailabilityUnschedulable, buildAccountError("not schedulable", nil)
	}
	if isQuotaExhausted(quota) {
		return GroupAccountAvailabilityQuotaExhausted, nil
	}
	if account.Status == StatusActive {
		return GroupAccountAvailabilityAvailable, nil
	}
	return GroupAccountAvailabilityUnknown, buildAccountError(account.ErrorMessage, nil)
}

func buildAccountError(message string, until *time.Time) *GroupAccountError {
	message = sanitizeAccountStatusMessage(message)
	if message == "" && until == nil {
		return nil
	}
	return &GroupAccountError{
		Message: message,
		Until:   until,
	}
}

func buildGroupAccountQuota(account *Account, now time.Time) GroupAccountQuota {
	updatedAt := parseGroupStatusExtraTime(account.Extra, "codex_usage_updated_at")
	quota := GroupAccountQuota{
		Source:    "passive",
		UpdatedAt: updatedAt,
	}
	if updatedAt == nil || now.Sub(*updatedAt) > groupStatusUsageStaleAfter {
		quota.Stale = true
	}

	quota.FiveHour = buildQuotaWindow(account.Extra, "codex_5h_used_percent", "codex_5h_reset_at")
	quota.SevenDay = buildQuotaWindow(account.Extra, "codex_7d_used_percent", "codex_7d_reset_at")
	return quota
}

func buildQuotaWindow(extra map[string]any, percentKey, resetKey string) *QuotaWindowState {
	used, ok := parseGroupStatusExtraFloat(extra, percentKey)
	if !ok {
		return nil
	}
	used = clampPercent(used)
	remaining := math.Max(0, 100-used)
	return &QuotaWindowState{
		UsedPercent:      roundPercent(used),
		RemainingPercent: roundPercent(remaining),
		ResetAt:          parseGroupStatusExtraTime(extra, resetKey),
	}
}

func isQuotaExhausted(quota GroupAccountQuota) bool {
	return (quota.FiveHour != nil && quota.FiveHour.UsedPercent >= 100) ||
		(quota.SevenDay != nil && quota.SevenDay.UsedPercent >= 100)
}

func sanitizeGroupAccountLabel(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return "Account"
	}
	label = redactGroupStatusSensitiveFragments(label)
	if len([]rune(label)) > 40 {
		return string([]rune(label)[:40])
	}
	return label
}

func sanitizeAccountStatusMessage(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return ""
	}
	if strings.HasPrefix(message, "{") {
		var payload map[string]any
		if err := json.Unmarshal([]byte(message), &payload); err == nil {
			if msg, ok := payload["message"].(string); ok && strings.TrimSpace(msg) != "" {
				message = msg
			} else if code, ok := payload["status_code"]; ok {
				message = fmt.Sprintf("upstream status %v", code)
			}
		}
	}
	message = strings.ReplaceAll(message, "\n", " ")
	message = strings.ReplaceAll(message, "\r", " ")
	message = strings.Join(strings.Fields(message), " ")
	message = redactGroupStatusSensitiveFragments(message)
	if len([]rune(message)) > 160 {
		return string([]rune(message)[:160]) + "..."
	}
	return message
}

func redactGroupStatusSensitiveFragments(message string) string {
	for _, pattern := range groupStatusSensitivePatterns {
		message = pattern.ReplaceAllString(message, "[redacted]")
	}
	return message
}

func parseGroupStatusExtraFloat(extra map[string]any, key string) (float64, bool) {
	if extra == nil {
		return 0, false
	}
	value, ok := extra[key]
	if !ok || value == nil {
		return 0, false
	}
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return f, err == nil
	default:
		return 0, false
	}
}

func parseGroupStatusExtraTime(extra map[string]any, key string) *time.Time {
	if extra == nil {
		return nil
	}
	value, ok := extra[key]
	if !ok || value == nil {
		return nil
	}
	switch v := value.(type) {
	case time.Time:
		t := v
		return &t
	case string:
		return parseGroupStatusTimeString(v)
	case json.Number:
		if i, err := v.Int64(); err == nil {
			t := time.Unix(i, 0)
			return &t
		}
	case float64:
		if v > 0 {
			t := time.Unix(int64(v), 0)
			return &t
		}
	case int64:
		if v > 0 {
			t := time.Unix(v, 0)
			return &t
		}
	case int:
		if v > 0 {
			t := time.Unix(int64(v), 0)
			return &t
		}
	}
	return nil
}

func parseGroupStatusTimeString(value string) *time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return &t
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return &t
	}
	if unix, err := strconv.ParseInt(value, 10, 64); err == nil && unix > 0 {
		t := time.Unix(unix, 0)
		return &t
	}
	return nil
}

func clampPercent(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func roundPercent(value float64) float64 {
	return math.Round(value*10) / 10
}
