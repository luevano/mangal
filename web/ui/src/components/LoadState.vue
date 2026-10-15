<script setup lang="ts">
defineProps<{ loading?: boolean; error?: string; empty?: boolean; emptyText?: string }>()
</script>

<template>
  <div v-if="error" class="alert alert-danger" data-test="load-error">{{ error }}</div>
  <div v-else-if="loading" class="text-secondary my-3"><span class="spinner-border spinner-border-sm me-2" />Loading…</div>
  <div v-else-if="empty" class="text-secondary my-3">{{ emptyText ?? 'Nothing here.' }}</div>
</template>
