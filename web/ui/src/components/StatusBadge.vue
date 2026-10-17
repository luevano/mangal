<script setup lang="ts">
import { computed } from 'vue'
import { useStatus } from '@/stores/status'
import { useEvents } from '@/stores/events'

const st = useStatus()
const ev = useEvents()

const label = computed(() => {
  if (st.error) return 'offline'
  if (!st.status) return '…'
  if (st.status.checking) return 'checking'
  return 'online'
})
const cls = computed(() => {
  if (st.error) return 'text-bg-danger'
  if (st.status?.checking) return 'text-bg-info'
  return ev.state === 'open' ? 'text-bg-success' : 'text-bg-warning'
})
const title = computed(() => {
  const s = st.status
  const parts = [s?.version ? `mangal ${s.version}` : '', s?.auth ? `as ${s.auth}` : '', `permissions: ${st.scopes.join(', ') || 'none'}`, `events: ${ev.state}${ev.error ? ` (${ev.error})` : ''}`]
  if (st.error) parts.unshift(st.error)
  return parts.filter(Boolean).join(' · ')
})
</script>

<template>
  <span class="d-inline-flex align-items-center gap-2">
    <span class="badge" :class="cls" :title="title" data-test="status-badge">{{ label }}</span>
    <span v-if="st.me" class="small text-secondary" :title="`permissions: ${st.scopes.join(', ')}`" data-test="me">
      <i class="bi bi-person me-1" />{{ st.me.kind === 'none' ? 'no login' : st.me.name }}
    </span>
    <button v-if="st.me?.kind === 'user'" class="btn btn-sm btn-outline-secondary py-0" title="Log out" data-test="logout" @click="st.logout()">
      <i class="bi bi-box-arrow-right" />
    </button>
  </span>
</template>
