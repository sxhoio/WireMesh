<script setup lang="ts">
withDefaults(defineProps<{
  panelClass?: string
  bodyClass?: string
  overlayClass?: string
  ariaLabel?: string
}>(), {
  panelClass: 'max-h-[80vh] max-w-3xl',
  bodyClass: 'p-5',
  overlayClass: 'bg-black/65',
  ariaLabel: '弹窗',
})

const emit = defineEmits<{ close: [] }>()
</script>

<template>
  <div class="fixed inset-0 z-[80] flex items-center justify-center p-4" :class="overlayClass" @click.self="emit('close')">
    <div role="dialog" aria-modal="true" :aria-label="ariaLabel" class="panel flex w-full flex-col overflow-hidden" :class="panelClass">
      <div v-if="$slots.header || $slots.actions" class="flex shrink-0 flex-wrap items-start justify-between gap-3 border-b border-ink-700 px-6 py-5">
        <slot name="header"></slot>
        <div class="flex items-center gap-2">
          <slot name="actions"></slot>
          <button type="button" class="text-slate-500 hover:text-white" aria-label="关闭弹窗" @click="emit('close')">✕</button>
        </div>
      </div>
      <slot name="toolbar"></slot>
      <div class="min-h-0 flex-1 overflow-auto" :class="bodyClass">
        <slot></slot>
      </div>
      <div v-if="$slots.footer" class="flex shrink-0 items-center justify-end gap-2 border-t border-ink-700 px-6 py-4">
        <slot name="footer"></slot>
      </div>
    </div>
  </div>
</template>
