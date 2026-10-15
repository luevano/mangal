<script setup lang="ts">
import { ref, watch } from 'vue'
import { useStatus } from '@/stores/status'

const st = useStatus()
const token = ref('')
const remember = ref(false)

watch(
  () => st.tokenAsked,
  (open) => {
    if (open) token.value = ''
  }
)

async function save() {
  await st.saveToken(token.value, remember.value)
}
</script>

<template>
  <template v-if="st.tokenAsked">
    <div class="modal d-block" tabindex="-1" role="dialog" aria-modal="true" data-test="token-dialog">
      <div class="modal-dialog">
        <form class="modal-content" @submit.prevent="save">
          <div class="modal-header">
            <h5 class="modal-title">Token</h5>
            <button type="button" class="btn-close" aria-label="Close" @click="st.cancelToken()" />
          </div>
          <div class="modal-body">
            <p class="small text-secondary">
              {{ st.tokenReason || 'This needs a token.' }} A token with the scope comes from
              <code>mangal serve token --generate [--name web --scope read,watch,write]</code>.
            </p>
            <input
              v-model="token"
              type="password"
              class="form-control"
              placeholder="token"
              autocomplete="current-password"
              data-test="token-input"
            />
            <div class="form-check mt-2">
              <input id="token-remember" v-model="remember" class="form-check-input" type="checkbox" />
              <label class="form-check-label" for="token-remember">Remember it in this browser</label>
            </div>
          </div>
          <div class="modal-footer">
            <button type="button" class="btn btn-secondary" @click="st.cancelToken()">Cancel</button>
            <button type="submit" class="btn btn-primary" :disabled="!token.trim()" data-test="token-save">Use it</button>
          </div>
        </form>
      </div>
    </div>
    <div class="modal-backdrop show" />
  </template>
</template>
