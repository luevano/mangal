<script setup lang="ts">
import { computed, ref } from 'vue'
import { RouterLink } from 'vue-router'
import type { HealthFinding } from '@/api/schemas'
import PagerNav from '@/components/PagerNav.vue'
import { usePager } from '@/composables/usePager'
import { applyLabel, choiceList, choiceLabel } from '@/composables/health'

// HealthFindingCard is a finding with its fix (the TUI's x and its choices) and, for a
// summary, its items, paged (the TUI's enter on a summary).
const props = defineProps<{ finding: HealthFinding; nested?: boolean }>()
const emit = defineEmits<{ fix: [f: HealthFinding, choice?: string]; dismiss: [f: HealthFinding, undo: boolean] }>()

const sevClass: Record<string, string> = { error: 'text-bg-danger', warning: 'text-bg-warning', info: 'text-bg-info' }
const f = computed(() => props.finding)
const choices = computed(() => choiceList(f.value))
const choice = ref(choices.value[0])
const label = computed(() => applyLabel(f.value))
const open = ref(false)
const items = computed(() => (open.value ? f.value.items : undefined))
const pager = usePager(items, 50)
</script>

<template>
  <div :class="nested ? 'border-top py-2' : 'card mb-2'" :data-test="nested ? 'finding-item' : 'finding'">
    <div :class="nested ? '' : 'card-body py-2'" :style="f.dismissed ? 'opacity: .5' : ''">
      <div class="d-flex flex-wrap gap-2 align-items-center">
        <span class="badge" :class="sevClass[f.severity] ?? 'text-bg-secondary'">{{ f.severity }}</span>
        <span v-if="!nested" class="badge text-bg-dark">{{ f.check }}</span>
        <RouterLink v-if="f.subject.kind === 'manga' && f.subject.id" :to="{ name: 'library-manga', params: { id: f.subject.id } }" class="fw-semibold">{{ f.subject.name }}</RouterLink>
        <RouterLink v-else-if="f.subject.kind === 'watch' && f.subject.id" :to="{ name: 'watch', params: { id: f.subject.id } }" class="fw-semibold">{{ f.subject.name }}</RouterLink>
        <RouterLink v-else-if="f.check === 'L6'" :to="{ name: 'trash' }" class="fw-semibold">{{ f.subject.name }}</RouterLink>
        <span v-else class="fw-semibold">{{ f.subject.name }}</span>
        <span class="me-auto">{{ f.message }}</span>
        <template v-if="f.fix.action">
          <select v-if="choices.length > 1" v-model="choice" class="form-select form-select-sm w-auto" data-test="fix-choice" :title="f.fix.action.kind === 'merge' ? 'the manga to keep' : 'the id to link'">
            <option v-for="c in choices" :key="c" :value="c">{{ choiceLabel(f, c) }}</option>
          </select>
          <button class="btn btn-sm btn-outline-primary py-0" data-test="fix" :title="label" @click="emit('fix', f, choices.length > 1 ? choice : undefined)">{{ label }}</button>
        </template>
        <button v-if="f.items?.length" class="btn btn-sm btn-outline-secondary py-0" data-test="items" @click="open = !open">{{ open ? 'hide' : 'list' }} them ({{ f.items.length }})</button>
        <button v-if="!f.dismissed" class="btn btn-sm btn-outline-secondary py-0" title="Fine as it is" @click="emit('dismiss', f, false)">Dismiss</button>
        <button v-else class="btn btn-sm btn-outline-secondary py-0" @click="emit('dismiss', f, true)">Bring back</button>
      </div>
      <ul v-if="f.details?.length" class="small mb-1 mt-1">
        <li v-for="(d, i) in f.details" :key="i">{{ d }}</li>
      </ul>
      <div v-if="f.fix.note" class="small text-secondary">{{ f.fix.note }}</div>
      <div v-if="f.fix.commands?.length && !f.fix.action" class="small">
        <code v-for="c in f.fix.commands" :key="c" class="me-3 d-inline-block">{{ c }}</code>
      </div>
      <div v-if="items?.length" class="ms-3 mt-2">
        <HealthFindingCard v-for="it in pager.items.value" :key="it.key" :finding="it" nested @fix="(x, c) => emit('fix', x, c)" @dismiss="(x, u) => emit('dismiss', x, u)" />
        <PagerNav v-model="pager.offset.value" :total="pager.total.value" :size="pager.size" />
      </div>
    </div>
  </div>
</template>
