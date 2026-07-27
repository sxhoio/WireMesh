import { reactive } from 'vue'

export type ConfirmVariant = 'danger' | 'warning' | 'info'

export interface ConfirmOptions {
  title: string
  message: string
  confirmText?: string
  cancelText?: string
  variant?: ConfirmVariant
}

export const confirmState = reactive({
  open: false,
  title: '',
  message: '',
  confirmText: '确定',
  cancelText: '取消',
  variant: 'warning' as ConfirmVariant,
})

let resolveCurrent: ((confirmed: boolean) => void) | null = null

export function requestConfirm(options: ConfirmOptions) {
  if (resolveCurrent) {
    resolveCurrent(false)
    resolveCurrent = null
  }
  confirmState.title = options.title
  confirmState.message = options.message
  confirmState.confirmText = options.confirmText || '确定'
  confirmState.cancelText = options.cancelText || '取消'
  confirmState.variant = options.variant || 'warning'
  confirmState.open = true
  return new Promise<boolean>((resolve) => {
    resolveCurrent = resolve
  })
}

export function resolveConfirm(confirmed: boolean) {
  if (!confirmState.open) return
  confirmState.open = false
  const resolve = resolveCurrent
  resolveCurrent = null
  resolve?.(confirmed)
}
