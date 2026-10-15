<script setup lang="ts">
import { onMounted, onUnmounted } from 'vue'
import { RouterView, useRoute } from 'vue-router'
import AppNav from '@/components/AppNav.vue'
import AppCrumbs from '@/components/AppCrumbs.vue'
import JobBar from '@/components/JobBar.vue'
import PlanDialog from '@/components/PlanDialog.vue'
import TokenDialog from '@/components/TokenDialog.vue'
import ToastHost from '@/components/ToastHost.vue'
import { useStatus } from '@/stores/status'
import { useEvents } from '@/stores/events'
import { useJobs } from '@/stores/jobs'
import { useWatches } from '@/stores/watches'
import { useChanges } from '@/stores/changes'

const route = useRoute()
const st = useStatus()
const events = useEvents()
const jobs = useJobs()
const watches = useWatches()
let timer: ReturnType<typeof setInterval> | undefined

onMounted(async () => {
  jobs.listen()
  watches.listen()
  useChanges().listen()
  st.onTokenChange(() => {
    events.reconnect()
    jobs.load()
  })
  const s = await st.refresh()
  events.start(s?.event_seq)
  jobs.load()
  timer = setInterval(() => st.refresh(), 30000)
})

onUnmounted(() => clearInterval(timer))
</script>

<template>
  <template v-if="route.meta.bare">
    <RouterView />
  </template>
  <template v-else>
    <AppNav />
    <main class="container-fluid py-3">
      <div v-if="st.error" class="alert alert-danger py-2">The server doesn't answer: {{ st.error }}</div>
      <AppCrumbs />
      <RouterView />
    </main>
    <JobBar />
  </template>
  <PlanDialog />
  <TokenDialog />
  <ToastHost />
</template>
