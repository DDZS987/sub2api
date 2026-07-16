<template>
  <div v-if="shouldRender" class="card">
    <div class="flex items-center justify-between border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <div>
        <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('dashboard.groupStatus.title') }}</h2>
        <p v-if="lastUpdatedText" class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
          {{ t('dashboard.groupStatus.updatedAt', { time: lastUpdatedText }) }}
        </p>
      </div>
      <button
        type="button"
        class="inline-flex h-9 w-9 items-center justify-center rounded-lg text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700 disabled:cursor-not-allowed disabled:opacity-50 dark:text-gray-400 dark:hover:bg-dark-700 dark:hover:text-gray-200"
        :disabled="loading"
        :title="t('common.refresh')"
        @click="load"
      >
        <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
      </button>
    </div>

    <div class="p-4">
      <div v-if="loading && groups.length === 0" class="flex items-center justify-center py-8">
        <LoadingSpinner size="sm" />
      </div>

      <div v-else-if="loadError" class="rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-900/60 dark:bg-red-900/20 dark:text-red-300">
        {{ t('dashboard.groupStatus.loadFailed') }}
      </div>

      <div v-else class="space-y-4">
        <section
          v-for="group in groups"
          :key="group.group.id"
          class="rounded-lg border border-gray-200 bg-white dark:border-dark-600 dark:bg-dark-800/40"
        >
          <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-100 px-4 py-3 dark:border-dark-700">
            <div class="min-w-0">
              <div class="flex items-center gap-2">
                <Icon name="server" size="sm" class="shrink-0 text-primary-500 dark:text-primary-400" />
                <h3 class="truncate text-sm font-semibold text-gray-900 dark:text-white">{{ group.group.name }}</h3>
              </div>
              <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                {{ t('dashboard.groupStatus.accountSummary', {
                  available: group.summary.available_accounts,
                  total: group.summary.total_accounts
                }) }}
              </p>
            </div>
            <span
              :class="[
                'inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-xs font-medium',
                groupBadgeClass(group)
              ]"
            >
              <span :class="['h-1.5 w-1.5 rounded-full', groupDotClass(group)]" />
              {{ groupStateLabel(group) }}
            </span>
          </div>

          <div class="divide-y divide-gray-100 dark:divide-dark-700">
            <article v-for="account in group.accounts" :key="account.label" class="space-y-3 px-4 py-3">
              <div class="flex flex-wrap items-start justify-between gap-2">
                <div class="min-w-0">
                  <div class="flex items-center gap-2">
                    <span :class="['h-2 w-2 shrink-0 rounded-full', accountDotClass(account.availability)]" />
                    <p class="truncate text-sm font-medium text-gray-900 dark:text-white">{{ account.label }}</p>
                  </div>
                  <p v-if="account.error?.message" class="mt-1 max-w-full truncate text-xs text-amber-700 dark:text-amber-300" :title="account.error.message">
                    {{ account.error.message }}
                  </p>
                </div>
                <span
                  :class="[
                    'shrink-0 rounded px-2 py-0.5 text-xs font-medium',
                    accountBadgeClass(account.availability)
                  ]"
                >
                  {{ availabilityLabel(account.availability) }}
                </span>
              </div>

              <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
                <QuotaRow
                  label="5h"
                  :window="account.quota.five_hour"
                  :stale="account.quota.stale"
                />
                <QuotaRow
                  label="7d"
                  :window="account.quota.seven_day"
                  :stale="account.quota.stale"
                />
              </div>

              <div class="flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px] text-gray-400 dark:text-gray-500">
                <span v-if="account.quota.stale" class="text-amber-600 dark:text-amber-400">
                  {{ t('dashboard.groupStatus.stale') }}
                </span>
                <span v-if="account.quota.updated_at">
                  {{ t('dashboard.groupStatus.sampledAt', { time: formatShortTime(account.quota.updated_at) }) }}
                </span>
                <span v-if="account.error?.until">
                  {{ t('dashboard.groupStatus.retryAt', { time: formatShortTime(account.error.until) }) }}
                </span>
              </div>
            </article>
          </div>

          <div v-if="group.fallback?.reserved" class="border-t border-gray-100 px-4 py-2 text-xs text-gray-500 dark:border-dark-700 dark:text-gray-400">
            {{ t('dashboard.groupStatus.fallbackReserved') }}
          </div>
        </section>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, onMounted, ref, type PropType } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import { userGroupsAPI, type GroupAccountAvailability, type GroupQuotaWindowState, type UserGroupStatus } from '@/api/groups'

const { t } = useI18n()

const groups = ref<UserGroupStatus[]>([])
const loading = ref(false)
const loadError = ref(false)

const shouldRender = computed(() => loading.value || loadError.value || groups.value.length > 0)
const newestUpdatedAt = computed(() => {
  const timestamps = groups.value
    .map((group) => group.updated_at)
    .filter((value): value is string => Boolean(value))
    .map((value) => new Date(value).getTime())
    .filter((value) => Number.isFinite(value))
  if (timestamps.length === 0) return null
  return new Date(Math.max(...timestamps)).toISOString()
})
const lastUpdatedText = computed(() => newestUpdatedAt.value ? formatShortTime(newestUpdatedAt.value) : '')

async function load() {
  loading.value = true
  loadError.value = false
  try {
    const data = await userGroupsAPI.getStatus()
    groups.value = data.groups || []
  } catch (error) {
    console.warn('Failed to load group status:', error)
    loadError.value = true
  } finally {
    loading.value = false
  }
}

function groupStateLabel(group: UserGroupStatus): string {
  if (group.summary.total_accounts === 0) return t('dashboard.groupStatus.noAccounts')
  if (group.summary.available_accounts > 0 && group.summary.error_accounts === 0) return t('dashboard.groupStatus.normal')
  if (group.summary.available_accounts > 0) return t('dashboard.groupStatus.degraded')
  return t('dashboard.groupStatus.unavailable')
}

function groupBadgeClass(group: UserGroupStatus): string {
  if (group.summary.total_accounts === 0) return 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'
  if (group.summary.available_accounts > 0 && group.summary.error_accounts === 0) {
    return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
  }
  if (group.summary.available_accounts > 0) {
    return 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
  }
  return 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300'
}

function groupDotClass(group: UserGroupStatus): string {
  if (group.summary.total_accounts === 0) return 'bg-gray-400'
  if (group.summary.available_accounts > 0 && group.summary.error_accounts === 0) return 'bg-emerald-500'
  if (group.summary.available_accounts > 0) return 'bg-amber-500'
  return 'bg-red-500'
}

function availabilityLabel(value: GroupAccountAvailability): string {
  return t(`dashboard.groupStatus.availability.${value}`)
}

function accountBadgeClass(value: GroupAccountAvailability): string {
  switch (value) {
    case 'available':
      return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
    case 'quota_exhausted':
    case 'rate_limited':
    case 'overloaded':
    case 'temp_unavailable':
    case 'unschedulable':
      return 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
    case 'error':
      return 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300'
    default:
      return 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'
  }
}

function accountDotClass(value: GroupAccountAvailability): string {
  switch (value) {
    case 'available':
      return 'bg-emerald-500'
    case 'error':
      return 'bg-red-500'
    case 'unknown':
      return 'bg-gray-400'
    default:
      return 'bg-amber-500'
  }
}

function quotaBarClass(percent: number): string {
  if (percent >= 100) return 'bg-red-500'
  if (percent >= 80) return 'bg-amber-500'
  return 'bg-emerald-500'
}

function formatPercent(value?: number | null): string {
  if (value == null || Number.isNaN(value)) return '-'
  if (Number.isInteger(value)) return `${value}%`
  return `${value.toFixed(1)}%`
}

function formatShortTime(value?: string | null): string {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '-'
  return new Intl.DateTimeFormat(undefined, {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false
  }).format(date)
}

function formatReset(value?: string | null): string {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '-'
  const diffMs = date.getTime() - Date.now()
  if (diffMs <= 0) return t('usage.resetPending')
  const mins = Math.floor(diffMs / 60000)
  const hours = Math.floor(mins / 60)
  if (hours >= 24) {
    const days = Math.floor(hours / 24)
    return `${days}d ${hours % 24}h`
  }
  if (hours > 0) return `${hours}h ${mins % 60}m`
  return `${Math.max(1, mins)}m`
}

const QuotaRow = defineComponent({
  name: 'QuotaRow',
  props: {
    label: {
      type: String,
      required: true
    },
    window: {
      type: Object as PropType<GroupQuotaWindowState | null | undefined>,
      default: null
    },
    stale: {
      type: Boolean,
      default: false
    }
  },
  setup(props) {
    return () => {
      const used = props.window?.used_percent ?? 0
      const remaining = props.window?.remaining_percent ?? Math.max(0, 100 - used)
      const width = `${Math.min(Math.max(used, 0), 100)}%`
      return h('div', { class: 'space-y-1.5' }, [
        h('div', { class: 'flex items-center justify-between gap-2 text-xs' }, [
          h('span', { class: 'font-medium text-gray-600 dark:text-gray-300' }, props.label),
          h('span', { class: 'font-mono text-gray-700 dark:text-gray-200' }, props.window
            ? t('dashboard.groupStatus.remainingValue', {
                remaining: formatPercent(remaining),
                used: formatPercent(used)
              })
            : t('dashboard.groupStatus.noQuotaData'))
        ]),
        h('div', { class: 'h-1.5 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-700' }, [
          h('div', {
            class: ['h-full rounded-full transition-all', props.window ? quotaBarClass(used) : 'bg-gray-300 dark:bg-dark-600'],
            style: { width: props.window ? width : '0%' }
          })
        ]),
        h('p', { class: 'text-[11px] text-gray-400 dark:text-gray-500' }, props.window?.reset_at
          ? t('dashboard.groupStatus.resetIn', { time: formatReset(props.window.reset_at) })
          : t('dashboard.groupStatus.resetUnknown'))
      ])
    }
  }
})

onMounted(() => {
  load()
})
</script>
