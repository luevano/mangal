import { defineStore } from 'pinia'
import { ref } from 'vue'
import { EventStream, type Listener, type StreamState, type Topic } from '@/api/events'
import { authHeaders } from '@/api/auth'

// the one event stream of the page; stores and views subscribe to its topics.
export const useEvents = defineStore('events', () => {
  const state = ref<StreamState>('idle')
  const error = ref('')
  const stream = new EventStream({
    headers: authHeaders,
    onState: (s, e) => {
      state.value = s
      error.value = e ?? ''
    }
  })

  function on(topic: Topic, fn: Listener) {
    return stream.on(topic, fn)
  }

  function start(since?: number) {
    stream.start(since)
  }

  function reconnect() {
    stream.reconnect()
  }

  return { state, error, on, start, reconnect, stream }
})
