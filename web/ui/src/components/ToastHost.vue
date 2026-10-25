<script setup lang="ts">
import { useToasts } from '@/stores/toasts'
const t = useToasts()
</script>

<template>
  <div class="toast-container position-fixed top-0 end-0 p-3" style="z-index: 2000">
    <div
      v-for="toast in t.list"
      :key="toast.id"
      class="toast show align-items-center border-0"
      :class="`text-bg-${toast.kind}`"
      role="status"
      @mouseenter="t.pause(toast.id)"
      @mouseleave="t.resume(toast.id)"
    >
      <div class="d-flex">
        <div class="toast-body">{{ toast.text }}</div>
        <button type="button" class="btn-close btn-close-white me-2 m-auto" aria-label="Close" @click="t.dismiss(toast.id)" />
      </div>
    </div>
  </div>
</template>
