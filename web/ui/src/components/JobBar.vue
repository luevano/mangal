<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import { useJobs, activeStatuses } from '@/stores/jobs'

const jobs = useJobs()
const current = computed(() => jobs.list.find((j) => activeStatuses.includes(j.status)))
</script>

<template>
  <RouterLink
    v-if="jobs.running || jobs.queued"
    :to="{ name: 'jobs' }"
    class="jobbar d-flex align-items-center gap-2 px-3 py-1 small text-decoration-none text-body"
    data-test="jobbar"
  >
    <span class="spinner-grow spinner-grow-sm text-info" />
    <span>{{ jobs.running }} running · {{ jobs.queued }} queued</span>
    <span v-if="current" class="text-secondary text-truncate">— {{ current.title }}{{ current.message ? ': ' + current.message : '' }}</span>
    <div v-if="current?.part" class="progress flex-grow-1" style="height: 4px; max-width: 200px">
      <div class="progress-bar" :style="{ width: `${Math.round((current.part ?? 0) * 100)}%` }" />
    </div>
  </RouterLink>
</template>

<style scoped>
.jobbar {
  position: fixed;
  bottom: 0;
  left: 0;
  right: 0;
  background: var(--bs-tertiary-bg);
  border-top: 1px solid var(--bs-border-color);
  z-index: 1030;
}
</style>
