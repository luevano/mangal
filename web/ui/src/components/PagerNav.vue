<script setup lang="ts">
// PagerNav is prev/next over a list paged by size (the server's limit/offset or usePager).
defineProps<{ total: number; size: number }>()
const offset = defineModel<number>({ required: true })
</script>

<template>
  <nav v-if="total > size" class="d-flex gap-2 align-items-center my-2" data-test="pager">
    <button class="btn btn-sm btn-outline-secondary" :disabled="offset === 0" @click="offset = Math.max(0, offset - size)">‹ prev</button>
    <span class="small">{{ offset + 1 }}–{{ Math.min(offset + size, total) }} of {{ total }}</span>
    <button class="btn btn-sm btn-outline-secondary" :disabled="offset + size >= total" data-test="pager-next" @click="offset += size">next ›</button>
  </nav>
</template>
