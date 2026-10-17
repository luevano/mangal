<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import LoginForm from './LoginForm.vue'
import { useStatus } from '@/stores/status'

const route = useRoute()
const st = useStatus()
const reason = computed(() => {
  const who = st.me?.kind === 'user' ? ` You're logged in as ${st.me.name} (${st.scopes.join(', ')}).` : ''
  return `${st.loginReason || 'This needs a login.'}${who}`
})
</script>

<template>
  <template v-if="st.loginAsked && !route.meta.public">
    <div class="modal d-block" tabindex="-1" role="dialog" aria-modal="true" data-test="login-dialog">
      <div class="modal-dialog">
        <div class="modal-content">
          <div class="modal-header">
            <h5 class="modal-title">Log in</h5>
            <button type="button" class="btn-close" aria-label="Close" @click="st.cancelLogin()" />
          </div>
          <div class="modal-body">
            <LoginForm :reason="reason" />
          </div>
        </div>
      </div>
    </div>
    <div class="modal-backdrop show" />
  </template>
</template>
