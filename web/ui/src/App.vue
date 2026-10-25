<script setup lang="ts">
import { onMounted, onUnmounted, watch } from 'vue'
import { RouterView, useRoute, useRouter } from 'vue-router'
import AppNav from '@/components/AppNav.vue'
import AppCrumbs from '@/components/AppCrumbs.vue'
import JobBar from '@/components/JobBar.vue'
import PlanDialog from '@/components/PlanDialog.vue'
import LoginDialog from '@/components/LoginDialog.vue'
import ToastHost from '@/components/ToastHost.vue'
import { useStatus } from '@/stores/status'
import { useEvents } from '@/stores/events'
import { useJobs } from '@/stores/jobs'
import { useWatches } from '@/stores/watches'
import { useChanges } from '@/stores/changes'
import { usePlan } from '@/stores/plan'

const route = useRoute()
const router = useRouter()
const st = useStatus()
const events = useEvents()
const jobs = useJobs()
const watches = useWatches()
let timer: ReturnType<typeof setInterval> | undefined

// a finished plan dialog doesn't follow to another page
const plan = usePlan()
watch(
  () => route.fullPath,
  () => {
    if (plan.step === 'done' || plan.step === 'error') plan.close()
  }
)

// the events and the jobs once logged in, again for another login
let started = false
async function startSession() {
  if (!st.me) return
  if (!started) {
    const s = await st.refresh()
    events.start(s?.event_seq)
    started = true
  } else {
    await st.refresh()
    events.reconnect()
  }
  jobs.load()
}

// the login ended (logged out, a new password): back to the login page
watch(
  () => st.loggedIn,
  (logged) => {
    if (!logged && st.checked && !route.meta.public) router.push({ name: 'login', query: { next: route.fullPath } })
  }
)

onMounted(async () => {
  jobs.listen()
  watches.listen()
  useChanges().listen()
  st.onLoginChange(startSession)
  await router.isReady()
  await startSession()
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
      <div v-if="st.status?.restart_needed?.length" class="alert alert-warning py-2" data-test="restart-needed">
        Restart needed: {{ st.status.restart_needed.join(', ') }} changed since mangal serve started; it keeps using the old
        {{ st.status.restart_needed.length === 1 ? 'value' : 'values' }} until it restarts.
      </div>
      <AppCrumbs />
      <RouterView />
    </main>
    <JobBar />
  </template>
  <PlanDialog />
  <LoginDialog />
  <ToastHost />
</template>
