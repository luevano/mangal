<script setup lang="ts">
import type { MetadataID } from '@/api/schemas'
import { sourceNames, sourceURL } from '@/composables/format'
defineProps<{ ids: MetadataID[] }>()
</script>

<template>
  <span class="d-inline-flex flex-wrap gap-1">
    <template v-for="id in ids" :key="id.source + id.id">
      <a v-if="sourceURL(id.source, id.id)" :href="sourceURL(id.source, id.id)" target="_blank" rel="noopener noreferrer" class="badge text-bg-secondary text-decoration-none">
        {{ sourceNames[id.source] ?? id.source }} {{ id.id }}
      </a>
      <span v-else class="badge text-bg-secondary">{{ sourceNames[id.source] ?? id.source }} {{ id.id }}</span>
    </template>
  </span>
</template>
