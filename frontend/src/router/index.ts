import { createRouter, createWebHashHistory } from 'vue-router'
import { useAppStore } from '../stores/app'

const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: '/onboarding', name: 'onboarding', component: () => import('../views/OnboardingView.vue'), meta: { bare: true } },
    { path: '/login', name: 'login', component: () => import('../views/LoginView.vue'), meta: { bare: true } },
    {
      path: '/',
      component: () => import('../layouts/AppLayout.vue'),
      children: [
        { path: '', name: 'home', component: () => import('../views/HomeView.vue') },
        { path: 'nodes', name: 'nodes', component: () => import('../views/NodesView.vue') },
        { path: 'clients', name: 'clients', component: () => import('../views/ClientAccessView.vue') },
        { path: 'alerts', name: 'alerts', component: () => import('../views/AlertCenterView.vue') },
        { path: 'access', name: 'access', component: () => import('../views/AccessControlView.vue') },
        { path: 'dns', name: 'dns', component: () => import('../views/DNSView.vue') },
        { path: 'settings', name: 'settings', component: () => import('../views/SettingsView.vue') },
      ],
    },
    { path: '/:pathMatch(.*)*', redirect: '/' },
  ],
})

router.beforeEach(async (to) => {
  const app = useAppStore()
  await app.restore()
  if (!app.onboarded) {
    if (to.name !== 'onboarding') return { name: 'onboarding' }
    return true
  }
  if (to.name === 'onboarding') return app.authed ? { name: 'home' } : { name: 'login' }
  if (!app.authed && to.name !== 'login') return { name: 'login' }
  if (app.authed && to.name === 'login') return { name: 'home' }
  return true
})

export default router
