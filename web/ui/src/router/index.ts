import { createRouter, createWebHashHistory, type RouteRecordRaw } from 'vue-router'
import { useStatus } from '@/stores/status'

declare module 'vue-router' {
  interface RouteMeta {
    // a full-screen view (the reader): no navbar
    bare?: boolean
    // a page without a login (the login)
    public?: boolean
  }
}

export const routes: RouteRecordRaw[] = [
  { path: '/', redirect: { name: 'library' } },
  { path: '/login', name: 'login', component: () => import('@/views/LoginView.vue'), meta: { bare: true, public: true } },
  { path: '/library', name: 'library', component: () => import('@/views/LibraryView.vue') },
  { path: '/library/duplicates', name: 'duplicates', component: () => import('@/views/DuplicatesView.vue') },
  { path: '/library/trash', name: 'trash', component: () => import('@/views/TrashView.vue') },
  { path: '/library/ops', name: 'ops', component: () => import('@/views/OpsView.vue') },
  { path: '/library/:id(\\d+)', name: 'library-manga', component: () => import('@/views/LibraryMangaView.vue') },
  { path: '/history', name: 'history', component: () => import('@/views/HistoryView.vue') },
  { path: '/watches', name: 'watches', component: () => import('@/views/WatchesView.vue') },
  { path: '/watches/runs', name: 'runs', component: () => import('@/views/RunsView.vue') },
  { path: '/watches/runs/:id(\\d+)', name: 'run', component: () => import('@/views/RunView.vue') },
  { path: '/watches/:id(\\d+)', name: 'watch', component: () => import('@/views/WatchView.vue') },
  { path: '/health', name: 'health', component: () => import('@/views/HealthView.vue') },
  { path: '/jobs', name: 'jobs', component: () => import('@/views/JobsView.vue') },
  { path: '/logs', name: 'logs', component: () => import('@/views/LogsView.vue') },
  { path: '/providers', name: 'providers', component: () => import('@/views/ProvidersView.vue') },
  { path: '/providers/:provider/search', name: 'search', component: () => import('@/views/SearchView.vue') },
  { path: '/providers/:provider/manga/:manga', name: 'provider-manga', component: () => import('@/views/ProviderMangaView.vue') },
  { path: '/accounts', name: 'accounts', component: () => import('@/views/AccountsView.vue') },
  { path: '/users', name: 'users', component: () => import('@/views/UsersView.vue') },
  { path: '/config', name: 'config', component: () => import('@/views/ConfigView.vue') },
  { path: '/read/library/:id(\\d+)', name: 'read-library', component: () => import('@/views/ReaderView.vue'), meta: { bare: true } },
  { path: '/read/:provider/:manga', name: 'read-online', component: () => import('@/views/ReaderView.vue'), meta: { bare: true } },
  { path: '/:pathMatch(.*)*', name: 'not-found', component: () => import('@/views/NotFoundView.vue') }
]

const router = createRouter({
  history: createWebHashHistory(import.meta.env.BASE_URL),
  routes
})

// nobody logged in: the login page first, then back where they were going
router.beforeEach(async (to) => {
  if (to.meta.public) return true
  const st = useStatus()
  if (!st.checked) await st.loadMe()
  if (!st.me && !st.error) return { name: 'login', query: to.fullPath === '/' ? {} : { next: to.fullPath } }
  return true
})

export default router
