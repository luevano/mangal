<script setup lang="ts">
import { onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import LoginForm from '@/components/LoginForm.vue'
import { useStatus } from '@/stores/status'

const route = useRoute()
const router = useRouter()
const st = useStatus()

function next() {
  const to = typeof route.query.next === 'string' && route.query.next.startsWith('/') ? route.query.next : '/'
  router.replace(to)
}

onMounted(async () => {
  // already logged in (another tab), or nothing to log into (serve.auth none)
  if (!st.checked) await st.loadMe()
  if (st.me) next()
})
</script>

<template>
  <div class="min-vh-100 d-flex align-items-center justify-content-center bg-body-tertiary px-3">
    <div class="card shadow-sm w-100" style="max-width: 24rem" data-test="login-page">
      <div class="card-body p-4">
        <h1 class="h4 mb-4 text-center">mangal</h1>
        <div v-if="st.error" class="alert alert-danger py-2">The server doesn't answer: {{ st.error }}</div>
        <LoginForm @done="next" />
        <p class="small text-secondary mt-3 mb-0">
          Users are made on the server: <code>mangal user add &lt;name&gt;</code>.
        </p>
      </div>
    </div>
  </div>
</template>
