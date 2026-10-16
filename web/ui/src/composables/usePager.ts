import { computed, ref, watch, type Ref } from 'vue'

// usePager pages a list in the browser: the page shown, back to the first when the list
// gets shorter than the page.
export function usePager<T>(list: Ref<T[] | undefined>, size = 100) {
  const offset = ref(0)
  const total = computed(() => list.value?.length ?? 0)
  const items = computed(() => (list.value ?? []).slice(offset.value, offset.value + size))
  watch(total, (n) => {
    if (offset.value >= n) offset.value = 0
  })
  return { offset, total, items, size }
}
