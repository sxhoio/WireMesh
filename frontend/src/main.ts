import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import router from './router'
import { setUnauthorizedHandler } from './api'
import { useAppStore } from './stores/app'
import { useMeshStore } from './stores/mesh'
import './style.css'

const app = createApp(App)
const pinia = createPinia()
app.use(pinia)
app.use(router)

// S12：任意已认证请求返回 401（会话过期/用户被停用或删除）时，清空内存中
// 的敏感数据（用户、系统设置、节点/拓扑/审计等）并回到登录页，避免数据
// 滞留与页面假死。登录接口自身的 401 由 api.ts 排除，不触发。
setUnauthorizedHandler(() => {
  const appStore = useAppStore()
  const meshStore = useMeshStore()
  appStore.$reset()
  meshStore.$reset()
  appStore.onboarded = true
  if (router.currentRoute.value.name !== 'login' && router.currentRoute.value.name !== 'onboarding') {
    void router.replace({ name: 'login' })
  }
})

app.mount('#app')
