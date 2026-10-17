import { defineStore } from 'pinia'
import { ref } from 'vue'
import { ApiError, errorMessage } from '@/api/errors'
import type { ApplyFields, ChangeResponse, Plan } from '@/api/schemas'
import { useStatus } from './status'
import { useChanges } from './changes'

// a change to plan and apply: run asks the API with apply false (the plan) or true (with
// the fields: the plan's hash as expect, force, confirm).
export interface PlanSpec {
  title: string
  run: (apply: boolean, fields: ApplyFields) => Promise<ChangeResponse>
  // what the apply button says (Apply by default)
  verb?: string
  onApplied?: (res: ChangeResponse) => void
}

export type PlanStep = 'idle' | 'planning' | 'review' | 'applying' | 'confirm' | 'done' | 'error'

// the plan dialog: every destructive change of the web UI goes through it, like the TUI's
// confirm. Plan → review (lines, warnings, blocked) → apply with expect; a changed plan
// (409 plan_changed) comes back for review, a needed confirm (428) asks for it, a missing
// login or permission asks to log in.
export const usePlan = defineStore('plan', () => {
  const spec = ref<PlanSpec>()
  const step = ref<PlanStep>('idle')
  const plan = ref<Plan>()
  const result = ref<ChangeResponse>()
  const error = ref('')
  const notice = ref('')
  const force = ref(false)
  const confirmWord = ref('')
  const confirmed = ref(false)

  function reset() {
    plan.value = undefined
    result.value = undefined
    error.value = ''
    notice.value = ''
    force.value = false
    confirmWord.value = ''
    confirmed.value = false
  }

  async function withLogin<T>(fn: () => Promise<T>): Promise<T> {
    try {
      return await fn()
    } catch (e) {
      if (e instanceof ApiError && e.needsLogin) {
        if (await useStatus().askLogin(e.message)) return await fn()
      }
      throw e
    }
  }

  async function open(s: PlanSpec) {
    reset()
    spec.value = s
    step.value = 'planning'
    try {
      const res = await withLogin(() => s.run(false, {}))
      plan.value = res.plan
      step.value = 'review'
    } catch (e) {
      error.value = errorMessage(e)
      step.value = 'error'
    }
  }

  async function apply() {
    const s = spec.value
    const p = plan.value
    if (!s || !p) return
    const fields: ApplyFields = { expect: p.hash }
    if (force.value) fields.force = true
    if (confirmWord.value && confirmed.value) fields.confirm = confirmWord.value
    step.value = 'applying'
    notice.value = ''
    try {
      const res = await withLogin(() => s.run(true, fields))
      result.value = res
      step.value = 'done'
      useChanges().bump()
      s.onApplied?.(res)
    } catch (e) {
      if (e instanceof ApiError && e.code === 'plan_changed' && e.plan) {
        plan.value = e.plan
        force.value = false
        notice.value = 'The library changed since the plan: review the new one.'
        step.value = 'review'
        return
      }
      if (e instanceof ApiError && e.code === 'confirm_required') {
        confirmWord.value = e.confirm ?? ''
        confirmed.value = false
        notice.value = e.message
        step.value = 'confirm'
        return
      }
      if (e instanceof ApiError && e.code === 'busy') {
        notice.value = `${e.message}: try again in a moment.`
        step.value = 'review'
        return
      }
      error.value = errorMessage(e)
      step.value = 'error'
    }
  }

  function close() {
    step.value = 'idle'
    spec.value = undefined
    reset()
  }

  return { spec, step, plan, result, error, notice, force, confirmWord, confirmed, open, apply, close }
})
