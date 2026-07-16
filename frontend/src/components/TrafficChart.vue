<script setup lang="ts">
defineProps<{ sourceKey: string; range: '24h' | '7d' | '30d' }>()
</script>

<template>
  <div class="flex h-40 items-center justify-center rounded-xl bg-ink-950/40 text-center ring-1 ring-ink-700">
    <div>
      <p class="text-xs text-slate-400">暂无历史流量数据</p>
      <p class="mt-1 text-[11px] text-slate-600">WireMesh 后端当前未提供流量时序查询 API，图表不会生成虚构曲线。</p>
    </div>
  </div>
</template>
