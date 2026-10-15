<script setup lang="ts">
defineProps<{ title: string; open: boolean; submitText?: string; disabled?: boolean; size?: 'sm' | 'lg' | 'xl' }>()
const emit = defineEmits<{ close: []; submit: [] }>()
</script>

<template>
  <template v-if="open">
    <div class="modal d-block" tabindex="-1" role="dialog" aria-modal="true">
      <div class="modal-dialog modal-dialog-scrollable" :class="size ? `modal-${size}` : ''">
        <form class="modal-content" @submit.prevent="emit('submit')">
          <div class="modal-header">
            <h5 class="modal-title">{{ title }}</h5>
            <button type="button" class="btn-close" aria-label="Close" @click="emit('close')" />
          </div>
          <div class="modal-body"><slot /></div>
          <div class="modal-footer">
            <button type="button" class="btn btn-secondary" @click="emit('close')">Cancel</button>
            <button type="submit" class="btn btn-primary" :disabled="disabled" data-test="form-submit">{{ submitText ?? 'OK' }}</button>
          </div>
        </form>
      </div>
    </div>
    <div class="modal-backdrop show" />
  </template>
</template>
