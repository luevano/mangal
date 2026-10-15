<script setup lang="ts">
import { RouterLink } from 'vue-router'
import { crumbs } from '@/stores/crumbs'
crumbs(() => [{ title: 'Not found' }])
</script>

<template>
  <p>Nothing here. <RouterLink :to="{ name: 'library' }">Library</RouterLink></p>
</template>
