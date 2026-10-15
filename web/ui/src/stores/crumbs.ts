import { defineStore } from 'pinia'
import { ref, watchEffect } from 'vue'
import type { RouteLocationRaw } from 'vue-router'

export interface Crumb {
  title: string
  to?: RouteLocationRaw
}

// the breadcrumbs of the shown view (the TUI's window titles); the page title follows.
export const useCrumbs = defineStore('crumbs', () => {
  const list = ref<Crumb[]>([])
  function set(c: Crumb[]) {
    list.value = c
    document.title = c.length ? `${c[c.length - 1]!.title} · mangal` : 'mangal'
  }
  return { list, set }
})

// crumbs sets the view's breadcrumbs, again when what they read changes.
export function crumbs(fn: () => Crumb[]) {
  const c = useCrumbs()
  watchEffect(() => c.set(fn()))
}
