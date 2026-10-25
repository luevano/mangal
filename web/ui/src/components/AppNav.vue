<script setup lang="ts">
import { computed, ref } from 'vue'
import { RouterLink } from 'vue-router'
import StatusBadge from './StatusBadge.vue'
import NotificationBell from './NotificationBell.vue'
import { useStatus } from '@/stores/status'

const open = ref(false)
const st = useStatus()

// the TUI's windows
const links = [
  { name: 'library', title: 'Library', icon: 'bi-collection' },
  { name: 'watches', title: 'Watches', icon: 'bi-eye' },
  { name: 'health', title: 'Health', icon: 'bi-heart-pulse' },
  { name: 'downloads', title: 'Downloads', icon: 'bi-download' },
  { name: 'jobs', title: 'Jobs', icon: 'bi-list-task' },
  { name: 'providers', title: 'Providers', icon: 'bi-search' },
  { name: 'history', title: 'History', icon: 'bi-clock-history' },
  { name: 'logs', title: 'Logs', icon: 'bi-journal-text' },
  { name: 'accounts', title: 'Accounts', icon: 'bi-person-badge' },
  { name: 'config', title: 'Config', icon: 'bi-gear' }
]
// the users page is an admin's (every permission)
const shown = computed(() => (st.allows('GET /v1/users') ? [...links, { name: 'users', title: 'Users', icon: 'bi-people' }] : links))
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
