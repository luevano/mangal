<script setup lang="ts">
import { ref } from 'vue'
import { ApiError, errorMessage } from '@/api/errors'
import { useStatus } from '@/stores/status'

defineProps<{ reason?: string }>()
const emit = defineEmits<{ done: [] }>()

const st = useStatus()
const name = ref('')
const password = ref('')
const remember = ref(false)
const error = ref('')
const busy = ref(false)

async function submit() {
  error.value = ''
  busy.value = true
  try {
    await st.login(name.value, password.value, remember.value)
    password.value = ''
    emit('done')
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) error.value = 'Wrong name or password.'
    else error.value = errorMessage(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <!-- a real form with name/password fields, so password managers fill and save it -->
  <form method="post" action="/api/v1/auth/login" data-test="login-form" @submit.prevent="submit">
    <p v-if="reason" class="small text-secondary">{{ reason }}</p>
    <div class="mb-3">
      <label for="login-name" class="form-label">Name</label>
      <input
        id="login-name"
        v-model="name"
        name="username"
        class="form-control"
        autocomplete="username"
        autocapitalize="none"
        spellcheck="false"
        required
        autofocus
        data-test="login-name"
      />
    </div>
    <div class="mb-3">
      <label for="login-password" class="form-label">Password</label>
      <input
        id="login-password"
        v-model="password"
        type="password"
        name="password"
        class="form-control"
        autocomplete="current-password"
        required
        data-test="login-password"
      />
    </div>
    <div class="form-check mb-3">
      <input id="login-remember" v-model="remember" class="form-check-input" type="checkbox" name="remember" />
      <label class="form-check-label" for="login-remember">Remember me</label>
    </div>
    <div v-if="error" class="alert alert-danger py-2" role="alert" data-test="login-error">{{ error }}</div>
    <button type="submit" class="btn btn-primary w-100" :disabled="busy || !name.trim() || !password" data-test="login-submit">
      <span v-if="busy" class="spinner-border spinner-border-sm me-1" />Log in
    </button>
  </form>
</template>
