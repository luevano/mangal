<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import { usePlan } from '@/stores/plan'

const p = usePlan()

const open = computed(() => p.step !== 'idle')
const blocked = computed(() => (p.plan?.blocked?.length ?? 0) > 0)
const canApply = computed(() => {
  if (!p.plan || p.plan.nothing) return false
  if (blocked.value && !(p.plan.forceable && p.force)) return false
  return true
})
const op = computed(() => p.result?.result?.op)
</script>

<template>
  <div v-if="open" class="modal d-block" tabindex="-1" role="dialog" aria-modal="true" data-test="plan-dialog">
    <div class="modal-dialog modal-lg modal-dialog-scrollable">
      <div class="modal-content">
        <div class="modal-header">
          <h5 class="modal-title">{{ p.spec?.title }}</h5>
          <button type="button" class="btn-close" aria-label="Close" @click="p.close()" />
        </div>
        <div class="modal-body">
          <div v-if="p.step === 'planning'" class="text-secondary">
            <span class="spinner-border spinner-border-sm me-2" />Planning…
          </div>

          <div v-if="p.notice" class="alert alert-warning py-2" data-test="plan-notice">{{ p.notice }}</div>

          <template v-if="p.plan && ['review', 'applying', 'confirm'].includes(p.step)">
            <p v-if="p.plan.summary" class="fw-semibold" data-test="plan-summary">{{ p.plan.summary }}</p>
            <p v-if="p.plan.nothing" class="text-secondary">Nothing to do.</p>
            <ul v-if="p.plan.lines.length" class="plan-lines small font-monospace" data-test="plan-lines">
              <li v-for="(l, i) in p.plan.lines" :key="i">{{ l }}</li>
            </ul>
            <div v-for="(w, i) in p.plan.warnings ?? []" :key="'w' + i" class="alert alert-warning py-1 my-1 small">
              {{ w }}
            </div>
            <div v-for="(b, i) in p.plan.blocked ?? []" :key="'b' + i" class="alert alert-danger py-1 my-1 small">
              {{ b }}
            </div>
            <div v-if="p.plan.forceable && blocked" class="form-check mt-2">
              <input id="plan-force" v-model="p.force" class="form-check-input" type="checkbox" />
              <label class="form-check-label" for="plan-force">Force it (loses what's blocked)</label>
            </div>
            <div v-if="p.plan.irreversible" class="alert alert-danger py-1 my-2 small">
              This can't be undone.
            </div>
          </template>

          <div v-if="p.step === 'confirm'" class="mt-2" data-test="plan-confirm">
            <div class="form-check">
              <input id="plan-confirmed" v-model="p.confirmed" class="form-check-input" type="checkbox" />
              <label class="form-check-label" for="plan-confirmed">
                I understand, go ahead ({{ p.confirmWord }})
              </label>
            </div>
          </div>

          <div v-if="p.step === 'done'" data-test="plan-done">
            <p class="text-success fw-semibold">{{ p.result?.result?.summary || 'Done.' }}</p>
            <ul v-if="p.result?.result?.lines?.length" class="plan-lines small font-monospace">
              <li v-for="(l, i) in p.result.result.lines" :key="i" :class="{ 'text-warning': l.warn }">{{ l.text }}</li>
            </ul>
            <p v-if="p.result?.undo" class="small text-secondary mb-0" data-test="plan-undo">
              Undo: {{ p.result.undo }}
              <RouterLink v-if="op" :to="{ name: 'ops', query: { op } }" @click="p.close()">(operations)</RouterLink>
            </p>
          </div>

          <div v-if="p.step === 'error'" class="alert alert-danger" data-test="plan-error">{{ p.error }}</div>
        </div>
        <div class="modal-footer">
          <button type="button" class="btn btn-secondary" @click="p.close()">
            {{ p.step === 'done' || p.step === 'error' ? 'Close' : 'Cancel' }}
          </button>
          <button
            v-if="p.step === 'review' || p.step === 'applying'"
            type="button"
            class="btn"
            :class="p.plan?.irreversible ? 'btn-danger' : 'btn-primary'"
            :disabled="!canApply || p.step === 'applying'"
            data-test="plan-apply"
            @click="p.apply()"
          >
            <span v-if="p.step === 'applying'" class="spinner-border spinner-border-sm me-1" />
            {{ p.spec?.verb || 'Apply' }}
          </button>
          <button
            v-if="p.step === 'confirm'"
            type="button"
            class="btn btn-danger"
            :disabled="!p.confirmed"
            data-test="plan-apply-confirmed"
            @click="p.apply()"
          >
            {{ p.spec?.verb || 'Apply' }}
          </button>
        </div>
      </div>
    </div>
  </div>
  <div v-if="open" class="modal-backdrop show" />
</template>

<style scoped>
.plan-lines {
  max-height: 50vh;
  overflow: auto;
  padding-left: 1.2rem;
  white-space: pre-wrap;
}
</style>
