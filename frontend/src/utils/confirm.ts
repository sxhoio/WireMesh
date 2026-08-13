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

interface PendingConfirm {
  options: ConfirmOptions
  resolve: (confirmed: boolean) => void
}

// 队列化确认请求：并发触发多个确认时按顺序逐个弹出，避免前一个请求被静默丢弃。
const queue: PendingConfirm[] = []
let current: PendingConfirm | null = null

function showNext() {
  const nextItem = queue.shift()
  if (!nextItem) {
    current = null
    return
  }
  current = nextItem
  confirmState.title = nextItem.options.title
  confirmState.message = nextItem.options.message
  confirmState.confirmText = nextItem.options.confirmText || '确定'
  confirmState.cancelText = nextItem.options.cancelText || '取消'
  confirmState.variant = nextItem.options.variant || 'warning'
  confirmState.open = true
}

export function requestConfirm(options: ConfirmOptions) {
  return new Promise<boolean>((resolve) => {
    queue.push({ options, resolve })
    if (!current) showNext()
  })
}

export function resolveConfirm(confirmed: boolean) {
  if (!confirmState.open || !current) return
  confirmState.open = false
  const { resolve } = current
  current = null
  resolve(confirmed)
  showNext()
}
