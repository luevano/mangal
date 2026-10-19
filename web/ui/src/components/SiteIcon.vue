<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { metadataIconURL, providerIconURL } from '@/api/urls'

// a provider's or metadata source's site icon (fetched by the server), nothing when the
// site has none
const props = defineProps<{ provider?: string; source?: string; title?: string }>()
const src = computed(() => (props.provider ? providerIconURL(props.provider) : props.source ? metadataIconURL(props.source) : ''))
const failed = ref(false)
watch(src, () => (failed.value = false))
</script>

<template>
  <img v-if="src && !failed" :src="src" class="site-icon" alt="" :title="title" loading="lazy" @error="failed = true" />
</template>
