<script setup lang="ts">
import { ref } from 'vue'
import { useToasts } from '@/stores/toasts'

const t = useToasts()
const open = ref(false)

function toggle() {
  open.value = !open.value
  if (open.value) t.markRead()
}
</script>

<template>
  <div class="dropdown me-2">
    <button
      type="button"
      class="btn btn-sm btn-outline-secondary py-0 position-relative"
      title="Notifications of this browser tab"
      aria-label="Notifications"
      :aria-expanded="open"
      data-test="bell"
      @click="toggle"
    >
      <i class="bi bi-bell" />
      <span v-if="t.unread" class="badge rounded-pill text-bg-danger ms-1" data-test="unread">{{ t.unread }}</span>
    </button>
    <div
      class="dropdown-menu dropdown-menu-end p-0 overflow-auto"
      :class="{ show: open }"
      style="width: min(32rem, 90vw); max-height: 70vh"
      data-test="history"
      @keydown.esc="open = false"
    >
      <div v-if="!t.history.length" class="p-3 text-secondary small">No notifications yet.</div>
      <ul v-else class="list-group list-group-flush small">
        <li v-for="n in t.history" :key="n.id" class="list-group-item d-flex gap-2">
          <span class="text-secondary text-nowrap">{{ n.at.toLocaleTimeString() }}</span>
          <span class="badge align-self-start" :class="`text-bg-${n.kind}`">{{ n.kind === 'danger' ? 'error' : n.kind }}</span>
          <span class="text-break">{{ n.text }}</span>
        </li>
      </ul>
    </div>
  </div>
</template>
