<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import type { LiveEvent } from '@/api/schemas'

const props = defineProps<{ events: LiveEvent[]; height?: string }>()
const box = ref<HTMLElement>()
const follow = ref(true)

const lines = computed(() => props.events.slice(-300))

watch(
  () => props.events.length,
  async () => {
    if (!follow.value) return
    await nextTick()
    if (box.value) box.value.scrollTop = box.value.scrollHeight
  }
)

function time(s: string) {
  return new Date(s).toLocaleTimeString()
}
</script>

<template>
  <div>
    <div class="form-check form-switch small mb-1">
      <input id="live-follow" v-model="follow" class="form-check-input" type="checkbox" />
      <label class="form-check-label" for="live-follow">follow</label>
    </div>
    <div ref="box" class="livelog font-monospace small border rounded p-2" :style="{ height: height ?? '16rem' }" data-test="live-log">
      <div v-if="!lines.length" class="text-secondary">No live events yet (they show while a check runs).</div>
      <div v-for="e in lines" :key="e.seq + e.kind + e.time" :class="{ 'text-danger': e.status === 'error', 'text-success': e.status === 'new' }">
        <span class="text-secondary">{{ time(e.time) }}</span>
        <span v-if="e.provider" class="text-info"> {{ e.provider }}</span>
        <span v-if="e.title"> {{ e.title }}:</span>
        {{ e.message }}
      </div>
    </div>
  </div>
</template>

<style scoped>
.livelog {
  overflow: auto;
  white-space: pre-wrap;
}
</style>
