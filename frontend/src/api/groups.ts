/**
 * User Groups API endpoints (non-admin)
 * Handles group-related operations for regular users
 */

import { apiClient } from './client'
import type { Group } from '@/types'

export type GroupAccountAvailability =
  | 'available'
  | 'error'
  | 'rate_limited'
  | 'overloaded'
  | 'temp_unavailable'
  | 'unschedulable'
  | 'quota_exhausted'
  | 'unknown'

export interface GroupQuotaWindowState {
  used_percent: number
  remaining_percent: number
  reset_at?: string | null
}

export interface GroupAccountQuotaStatus {
  source?: string
  updated_at?: string | null
  stale: boolean
  five_hour?: GroupQuotaWindowState | null
  seven_day?: GroupQuotaWindowState | null
}

export interface GroupAccountStatusError {
  message: string
  until?: string | null
}

export interface GroupAccountStatus {
  label: string
  platform: string
  type: string
  status: string
  schedulable: boolean
  availability: GroupAccountAvailability
  error?: GroupAccountStatusError | null
  quota: GroupAccountQuotaStatus
  updated_at?: string | null
  last_used_at?: string | null
}

export interface UserGroupStatusSummary {
  total_accounts: number
  available_accounts: number
  error_accounts: number
  stale_accounts: number
}

export interface UserGroupStatusFallback {
  reserved: boolean
  ready: boolean
  active: boolean
  mode?: string
}

export interface UserGroupStatus {
  group: {
    id: number
    name: string
    platform: string
  }
  accounts: GroupAccountStatus[]
  summary: UserGroupStatusSummary
  fallback: UserGroupStatusFallback
  updated_at?: string | null
}

export interface UserGroupStatusResponse {
  groups: UserGroupStatus[]
}

/**
 * Get available groups that the current user can bind to API keys
 * This returns groups based on user's permissions:
 * - Standard groups: public (non-exclusive) or explicitly allowed
 * - Subscription groups: user has active subscription
 * @returns List of available groups
 */
export async function getAvailable(): Promise<Group[]> {
  const { data } = await apiClient.get<Group[]>('/groups/available')
  return data
}

/**
 * Get current user's custom group rate multipliers
 * @returns Map of group_id to custom rate_multiplier
 */
export async function getUserGroupRates(): Promise<Record<number, number>> {
  const { data } = await apiClient.get<Record<number, number> | null>('/groups/rates')
  return data || {}
}

export async function getStatus(): Promise<UserGroupStatusResponse> {
  const { data } = await apiClient.get<UserGroupStatusResponse>('/groups/status')
  return data
}

export async function getStatusByID(groupID: number): Promise<UserGroupStatus> {
  const { data } = await apiClient.get<UserGroupStatus>(`/groups/${groupID}/status`)
  return data
}

export const userGroupsAPI = {
  getAvailable,
  getUserGroupRates,
  getStatus,
  getStatusByID
}

export default userGroupsAPI
