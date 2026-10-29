import { createRouter, createWebHistory } from 'vue-router'
import { BASE, loadSession, session, setUnauthorizedHandler } from './lib/api'

export const router = createRouter({
  history: createWebHistory(BASE),
  routes: [
    { path: '/login', name: 'login', component: () => import('./views/LoginView.vue'), meta: { public: true } },
    {
      path: '/',
      component: () => import('./layouts/AppLayout.vue'),
      children: [
        { path: '', redirect: { name: 'nodes' } },
        { path: 'nodes', name: 'nodes', component: () => import('./views/NodesView.vue'), meta: { title: '服务器' } },
        { path: 'settings', name: 'settings', component: () => import('./views/SettingsView.vue'), meta: { title: '设置' } },
      ],
    },
    { path: '/:pathMatch(.*)*', redirect: '/' },
  ],
})

router.beforeEach(async (to) => {
  if (!session.loaded) {
    try {
      await loadSession()
    } catch {
      session.loaded = true
    }
  }
  if (!to.meta.public && !session.authenticated) return { name: 'login', query: { next: to.fullPath } }
  if (to.name === 'login' && session.authenticated) return { name: 'nodes' }
})

router.afterEach((to) => {
  document.title = to.meta.title ? `${to.meta.title} · x-hub` : 'x-hub'
})

setUnauthorizedHandler(() => {
  session.authenticated = false
  if (router.currentRoute.value.name !== 'login') router.push({ name: 'login' })
})
