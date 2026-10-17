<script setup>
import { computed, ref } from 'vue'
import { renderSanitizedMarkdown } from '../markdown'
import { readReaderFontSize, writeReaderFontSize } from '../reader-preferences'
const props = defineProps({ report: { type: Object, required: true } })
defineEmits(['back', 'share', 'delete'])
const fontSize = ref(readReaderFontSize())
const documentContent = computed(() => {
  const body = document.createElement('div')
  body.innerHTML = renderSanitizedMarkdown(props.report.content || '')
  const headings = [...body.querySelectorAll('h1,h2,h3')].map((heading, index) => {
    heading.id = `report-${props.report.id}-section-${index}`
    return { id: heading.id, text: heading.textContent }
  })
  return { html: body.innerHTML, headings }
})
function resize(value) { fontSize.value = Math.min(24, Math.max(13, value)); writeReaderFontSize(fontSize.value) }
</script>

<template>
  <section class="topic-reader" aria-label="报告阅读">
    <div class="topic-reader-tools">
      <button class="ts-text" @click="$emit('back')">← 返回主题内容</button>
      <div class="topic-reader-actions">
        <button class="ts-button" aria-label="缩小正文字号" :disabled="fontSize <= 13" @click="resize(fontSize - 1)">A−</button>
        <span>{{ fontSize }}px</span>
        <button class="ts-button" aria-label="放大正文字号" :disabled="fontSize >= 24" @click="resize(fontSize + 1)">A＋</button>
        <button class="ts-button" @click="$emit('share', report)">分享</button>
        <details><summary>更多</summary><button class="ts-text" @click="$emit('delete', report)">删除报告…</button></details>
      </div>
    </div>
    <div class="topic-reader-layout">
      <article :style="{ '--reader-font-size': `${fontSize}px` }">
        <div class="ts-meta">{{ report.source }} · {{ report.display_name || report.bot_name }} · {{ new Date(report.executed_at).toLocaleString() }}</div>
        <h1>{{ report.title || '正式报告' }}</h1>
        <p v-if="report.summary" class="ts-reader-summary">{{ report.summary }}</p>
        <div v-if="report.tags?.length" class="ts-meta">{{ report.tags.map(tag => `#${tag}`).join('　') }}</div>
        <div class="markdown-body" v-html="documentContent.html"></div>
      </article>
      <nav v-if="documentContent.headings.length" class="topic-reader-toc" aria-label="本文目录">
        <strong>本文目录</strong>
        <a v-for="heading in documentContent.headings" :key="heading.id" :href="`#${heading.id}`">{{ heading.text }}</a>
      </nav>
    </div>
  </section>
</template>
