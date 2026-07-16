import { describe, expect, it } from 'vitest'
import type { GroupAccountAvailability } from '@/api/groups'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

function getLocaleValue(messages: Record<string, any>, key: string): unknown {
  return key.split('.').reduce<unknown>((value, part) => {
    if (typeof value !== 'object' || value === null) return undefined
    return (value as Record<string, unknown>)[part]
  }, messages)
}

const baseKeys = [
  'dashboard.groupStatus.title',
  'dashboard.groupStatus.updatedAt',
  'dashboard.groupStatus.loadFailed',
  'dashboard.groupStatus.accountSummary',
  'dashboard.groupStatus.noAccounts',
  'dashboard.groupStatus.normal',
  'dashboard.groupStatus.degraded',
  'dashboard.groupStatus.unavailable',
  'dashboard.groupStatus.stale',
  'dashboard.groupStatus.sampledAt',
  'dashboard.groupStatus.retryAt',
  'dashboard.groupStatus.fallbackReserved',
  'dashboard.groupStatus.remainingValue',
  'dashboard.groupStatus.noQuotaData',
  'dashboard.groupStatus.resetIn',
  'dashboard.groupStatus.resetUnknown'
]

const availabilityKeys: GroupAccountAvailability[] = [
  'available',
  'error',
  'rate_limited',
  'overloaded',
  'temp_unavailable',
  'unschedulable',
  'quota_exhausted',
  'unknown'
]

describe.each([
  ['zh', zh],
  ['en', en]
])('group status locale keys: %s', (_locale, messages) => {
  it('has every label used by UserGroupStatusCard', () => {
    for (const key of baseKeys) {
      expect(getLocaleValue(messages, key), key).toEqual(expect.any(String))
    }
  })

  it('has every account availability label', () => {
    for (const key of availabilityKeys) {
      expect(getLocaleValue(messages, `dashboard.groupStatus.availability.${key}`), key).toEqual(expect.any(String))
    }
  })
})
