<script setup lang="ts">
import {computed, ref} from 'vue'
import {formatSize} from '../format'
import {useTransfers} from '../stores/transfers'
import Icon from './Icon.vue'

const tr = useTransfers()
const open = ref(true)

const labels: Record<string, string> = {
  queued: 'Wartet',
  running: 'Läuft',
  done: 'Fertig',
  failed: 'Fehler',
  cancelled: 'Abgebrochen',
  skipped: 'Übersprungen',
}

const finished = computed(() => tr.jobs.some((j) => !['queued', 'running'].includes(j.status)))

function percent(done: number, total: number) {
  return total > 0 ? Math.min(100, Math.round((done / total) * 100)) : 0
}
</script>

<template>
  <section class="queue" :class="{collapsed: !open}">
    <header>
      <button class="ghost" :aria-expanded="open" @click="open = !open">
        <Icon name="chevron" :class="{rot: !open}" />
        Übertragungen
        <span v-if="tr.active" class="badge">{{ tr.active }} aktiv</span>
        <span v-else-if="tr.jobs.length" class="muted">({{ tr.jobs.length }})</span>
      </button>
      <span class="spacer" />
      <button class="ghost" :disabled="!finished" @click="tr.clearFinished()">Beendete entfernen</button>
    </header>

    <div v-if="open" class="jobs">
      <p v-if="!tr.jobs.length" class="empty">Keine Übertragungen. Dateien per Drag &amp; Drop zwischen die Fenster ziehen oder per Rechtsklick hoch-/herunterladen.</p>
      <div v-for="j in tr.jobs" :key="j.id" class="job" :class="j.status">
        <Icon :name="j.direction === 'upload' ? 'upload' : 'download'" />
        <div class="info">
          <div class="name" :title="`${j.src} → ${j.dst}`">
            <Icon :name="j.isDir ? 'folder' : 'file'" :size="13" /> {{ j.name }}
          </div>
          <div v-if="j.status === 'failed'" class="err">{{ j.error }}</div>
          <div v-else-if="j.note && j.status !== 'running'" class="muted note">{{ j.note }}</div>
          <div v-else class="bar" :class="{indeterminate: j.status === 'running' && !j.total}">
            <div :style="{width: (j.status === 'done' ? 100 : percent(j.done, j.total)) + '%'}" />
          </div>
        </div>
        <span class="size muted">{{ formatSize(j.done) }}<template v-if="j.total"> / {{ formatSize(j.total) }}</template></span>
        <span class="chip" :class="j.status">{{ labels[j.status] }}</span>
        <button v-if="j.status === 'queued' || j.status === 'running'" class="icon-btn" title="Abbrechen" @click="tr.cancel(j.id)"><Icon name="close" /></button>
        <button v-else-if="j.status !== 'done'" class="icon-btn" title="Wiederholen" @click="tr.retry(j.id)"><Icon name="refresh" /></button>
      </div>
    </div>
  </section>
</template>
