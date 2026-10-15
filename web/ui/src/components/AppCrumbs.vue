<script setup lang="ts">
import { RouterLink } from 'vue-router'
import { useCrumbs } from '@/stores/crumbs'
const c = useCrumbs()
</script>

<template>
  <nav v-if="c.list.length" aria-label="breadcrumb">
    <ol class="breadcrumb small mb-2" data-test="crumbs">
      <li v-for="(cr, i) in c.list" :key="i" class="breadcrumb-item" :class="{ active: i === c.list.length - 1 }">
        <RouterLink v-if="cr.to && i < c.list.length - 1" :to="cr.to">{{ cr.title }}</RouterLink>
        <span v-else>{{ cr.title }}</span>
      </li>
    </ol>
  </nav>
</template>
