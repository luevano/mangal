<script setup lang="ts">
import { computed, ref } from 'vue'
import { RouterLink } from 'vue-router'
import StatusBadge from './StatusBadge.vue'
import NotificationBell from './NotificationBell.vue'
import { useStatus, type Operation } from '@/stores/status'

const open = ref(false)
const st = useStatus()

// the TUI's windows, each with the operation it shows
const links: { name: string; title: string; icon: string; op?: Operation }[] = [
  { name: 'library', title: 'Library', icon: 'bi-collection' },
  { name: 'watches', title: 'Watches', icon: 'bi-eye' },
  { name: 'health', title: 'Health', icon: 'bi-heart-pulse', op: 'GET /v1/health' },
  { name: 'downloads', title: 'Downloads', icon: 'bi-download' },
  { name: 'jobs', title: 'Jobs', icon: 'bi-list-task' },
  { name: 'providers', title: 'Providers', icon: 'bi-search' },
  { name: 'history', title: 'History', icon: 'bi-clock-history' },
  { name: 'logs', title: 'Logs', icon: 'bi-journal-text', op: 'GET /v1/logs' },
  { name: 'accounts', title: 'Accounts', icon: 'bi-person-badge', op: 'GET /v1/accounts' },
  { name: 'config', title: 'Config', icon: 'bi-gear' },
  // an admin's users and tokens, or your own tokens (the tokens permission)
  { name: 'users', title: 'Users', icon: 'bi-people', op: 'GET /v1/tokens' }
]
const shown = computed(() => links.filter((l) => !l.op || st.allows(l.op)))
</script>

<template>
  <nav class="navbar navbar-expand-lg bg-body-tertiary border-bottom">
    <div class="container-fluid">
      <RouterLink class="navbar-brand" :to="{ name: 'library' }">mangal</RouterLink>
      <button class="navbar-toggler" type="button" aria-label="Menu" @click="open = !open">
        <span class="navbar-toggler-icon" />
      </button>
      <div class="collapse navbar-collapse" :class="{ show: open }">
        <ul class="navbar-nav me-auto">
          <li v-for="l in shown" :key="l.name" class="nav-item">
            <RouterLink class="nav-link" active-class="active" :to="{ name: l.name }" :data-test="`nav-${l.name}`" @click="open = false">
              <i class="bi me-1" :class="l.icon" />{{ l.title }}
            </RouterLink>
          </li>
        </ul>
        <NotificationBell />
        <StatusBadge />
      </div>
    </div>
  </nav>
</template>
