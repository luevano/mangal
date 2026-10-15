import { defineStore } from 'pinia'
import { ref } from 'vue'

export interface Toast {
  id: number
  kind: 'info' | 'success' | 'warning' | 'danger'
  text: string
}

export const useToasts = defineStore('toasts', () => {
  const list = ref<Toast[]>([])
  let next = 1

  function push(kind: Toast['kind'], text: string, ms = 5000) {
    const id = next++
    list.value.push({ id, kind, text })
    if (ms > 0) setTimeout(() => dismiss(id), ms)
    return id
  }

  function dismiss(id: number) {
    list.value = list.value.filter((t) => t.id !== id)
  }

  return { list, push, dismiss }
})
