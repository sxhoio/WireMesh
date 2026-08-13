<script setup lang="ts">
import { computed, onMounted, onUnmounted } from 'vue'
import { confirmState, resolveConfirm } from '../utils/confirm'

const toneClass = computed(() => {
  if (confirmState.variant === 'danger') return 'border-red-500/35 bg-red-500/15 text-red-300 ring-red-500/35'
  if (confirmState.variant === 'info') return 'border-cyan-500/35 bg-cyan-500/15 text-cyan-300 ring-cyan-500/35'
  return 'border-amber-500/35 bg-amber-500/15 text-amber-300 ring-amber-500/35'
})

const confirmClass = computed(() => {
  if (confirmState.variant === 'danger') return 'bg-red-500 text-white hover:bg-red-400 focus:ring-red-500/30'
  if (confirmState.variant === 'info') return 'bg-cyan-500 text-ink-950 hover:bg-cyan-400 focus:ring-cyan-500/30'
  return 'bg-amber-500 text-ink-950 hover:bg-amber-400 focus:ring-amber-500/30'
})

function onKeydown(event: KeyboardEvent) {
  if (!confirmState.open) return
  if (event.key === 'Escape') {
    event.preventDefault()
    resolveConfirm(false)
  }
}

onMounted(() => window.addEventListener('keydown', onKeydown))
onUnmounted(() => window.removeEventListener('keydown', onKeydown))
</script>

<template>
  <Teleport to="body">
    <Transition name="confirm-dialog">
      <div
        v-if="confirmState.open"
        class="fixed inset-0 z-[120] flex items-center justify-center bg-black/70 p-4 backdrop-blur-sm"
        @click.self="resolveConfirm(false)"
      >
        <div role="dialog" aria-modal="true" class="w-full max-w-md overflow-hidden rounded-2xl border border-ink-600 bg-ink-900 shadow-2xl shadow-black/50 ring-1 ring-white/5">
          <div class="flex items-start gap-4 px-6 py-5">
            <span class="flex h-11 w-11 shrink-0 items-center justify-center rounded-2xl border ring-1" :class="toneClass">
              <svg v-if="confirmState.variant === 'danger'" viewBox="0 0 24 24" fill="none" class="h-5 w-5" stroke="currentColor" stroke-width="2">
                <path stroke-linecap="round" stroke-linejoin="round" d="M12 9v4m0 4h.01M10.3 3.8L2.5 17.3A2 2 0 004.2 20h15.6a2 2 0 001.7-2.7L13.7 3.8a2 2 0 00-3.4 0z" />
              </svg>
              <svg v-else-if="confirmState.variant === 'info'" viewBox="0 0 24 24" fill="none" class="h-5 w-5" stroke="currentColor" stroke-width="2">
                <path stroke-linecap="round" stroke-linejoin="round" d="M11.25 11.25l.041-.02a.75.75 0 011.063.852l-.708 2.836a.75.75 0 001.063.853l.041-.021M12 8.25h.008v.008H12V8.25zM21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
              </svg>
              <svg v-else viewBox="0 0 24 24" fill="none" class="h-5 w-5" stroke="currentColor" stroke-width="2">
                <path stroke-linecap="round" stroke-linejoin="round" d="M12 9v3.75m9-.75a9 9 0 11-18 0 9 9 0 0118 0zm-9 3.75h.008v.008H12v-.008z" />
              </svg>
            </span>
            <div class="min-w-0 flex-1">
              <h3 class="text-base font-semibold text-white">{{ confirmState.title }}</h3>
              <p class="mt-2 whitespace-pre-wrap text-sm leading-6 text-slate-400">{{ confirmState.message }}</p>
            </div>
          </div>
          <div class="flex justify-end gap-2 border-t border-ink-700 bg-ink-950/45 px-6 py-4">
            <button
              type="button"
              class="rounded-xl border border-ink-600 bg-ink-800/70 px-4 py-2 text-sm font-medium text-slate-300 transition hover:border-slate-500 hover:text-white focus:outline-none focus:ring-2 focus:ring-slate-500/20"
              @click="resolveConfirm(false)"
            >
              {{ confirmState.cancelText }}
            </button>
            <button
              type="button"
              class="rounded-xl px-4 py-2 text-sm font-semibold transition focus:outline-none focus:ring-2 active:scale-[0.98]"
              :class="confirmClass"
              @click="resolveConfirm(true)"
            >
              {{ confirmState.confirmText }}
            </button>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.confirm-dialog-enter-active,
.confirm-dialog-leave-active {
  transition: opacity 0.18s ease;
}
.confirm-dialog-enter-active > div,
.confirm-dialog-leave-active > div {
  transition: transform 0.18s ease, opacity 0.18s ease;
}
.confirm-dialog-enter-from,
.confirm-dialog-leave-to {
  opacity: 0;
}
.confirm-dialog-enter-from > div,
.confirm-dialog-leave-to > div {
  opacity: 0;
  transform: translateY(10px) scale(0.98);
}
</style>
