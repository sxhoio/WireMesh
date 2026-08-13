import { onUnmounted, ref, type Ref } from 'vue'

export function useClipboard<T>(resetValue: T, clearAfterMs = 1400) {
  const copied = ref(resetValue) as Ref<T>
  let clearTimer: number | undefined

  function clearCopied() {
    if (clearTimer) window.clearTimeout(clearTimer)
    clearTimer = undefined
    copied.value = resetValue
  }

  async function copyText(text: string, copiedValue: T) {
    const ok = await writeClipboard(text)
    if (!ok) return false
    copied.value = copiedValue
    if (clearTimer) window.clearTimeout(clearTimer)
    clearTimer = window.setTimeout(() => {
      copied.value = resetValue
      clearTimer = undefined
    }, clearAfterMs)
    return true
  }

  onUnmounted(() => {
    if (clearTimer) window.clearTimeout(clearTimer)
    clearTimer = undefined
  })

  return { copied, copyText, clearCopied }
}

async function writeClipboard(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    return true
  } catch {
    return fallbackCopyText(text)
  }
}

function fallbackCopyText(text: string) {
  try {
    const textarea = document.createElement('textarea')
    textarea.value = text
    textarea.setAttribute('readonly', 'true')
    textarea.style.position = 'fixed'
    textarea.style.left = '-9999px'
    document.body.appendChild(textarea)
    textarea.select()
    const ok = document.execCommand('copy')
    textarea.remove()
    return ok
  } catch {
    return false
  }
}
