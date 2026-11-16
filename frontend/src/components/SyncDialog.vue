<script setup lang="ts">
import {computed, ref, watch} from 'vue'
import {App, errMsg} from '../api'
import {formatSize} from '../format'
import {usePanes} from '../stores/panes'
import {useUi} from '../stores/ui'
import type {transfer} from '../../wailsjs/go/models'
import Icon from './Icon.vue'

const emit = defineEmits<{close: []}>()
const panes = usePanes()
const ui = useUi()

// The directories are fixed when the dialog opens.
const localDir = panes.cur.local
const remoteDir = panes.cur.remote

const direction = ref<'upload' | 'download'>('upload')
const keepNewer = ref(true)
const deleteExtra = ref(false)
const plan = ref<transfer.SyncPlan | null>(null)
const busy = ref(false)
const error = ref('')

const MAX_ROWS = 500
const shown = computed(() => plan.value?.actions.slice(0, MAX_ROWS) ?? [])

// any change of the options makes an existing preview stale
watch([direction, keepNewer, deleteExtra], () => {
  plan.value = null
  error.value = ''
})

const kindLabel: Record<string, string> = {copy: 'Kopieren', mkdir: 'Ordner', delete: 'Löschen'}
const reasonLabel: Record<string, string> = {new: 'neu', newer: 'neuer', size: 'Größe anders', extra: 'überzählig'}

async function compare() {
  busy.value = true
  error.value = ''
  try {
    plan.value = await App.PlanSync(localDir, remoteDir, {direction: direction.value, deleteExtra: deleteExtra.value, keepNewer: keepNewer.value})
    plan.value.actions ??= [] // Go sends null for an empty list
  } catch (e) {
    plan.value = null
    error.value = errMsg(e)
  } finally {
    busy.value = false
  }
}

async function run() {
  const p = plan.value
  if (!p) return
  if (p.deleteCount > 0) {
    const where = p.direction === 'upload' ? 'auf dem Server' : 'auf diesem Computer'
    const ok = await ui.confirm('Dateien löschen?', `${p.deleteCount} Element(e) ${where} werden unwiderruflich gelöscht (Ordner samt Inhalt), weil sie in der Quelle nicht vorhanden sind.`, {okLabel: 'Synchronisieren und löschen', danger: true})
    if (!ok) return
  }
  busy.value = true
  try {
    const n = await App.RunSync(p.id)
    ui.toast(n ? `${n} Übertragung(en) gestartet` : 'Synchronisiert', 'success')
    panes.refresh('local')
    panes.refresh('remote')
    emit('close')
  } catch (e) {
    ui.error(e)
    panes.refresh('local')
    panes.refresh('remote')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="overlay" @mousedown.self="emit('close')" @keydown.esc="emit('close')">
    <div class="modal sync" role="dialog" aria-modal="true" aria-label="Verzeichnisse abgleichen">
      <h2>Verzeichnisse abgleichen</h2>
      <div class="dirs">
        <b>Lokal</b><span :title="localDir">{{ localDir }}</span>
        <b>Server</b><span :title="remoteDir">{{ remoteDir }}</span>
      </div>

      <fieldset class="dir-choice">
        <label class="opt"><input v-model="direction" type="radio" value="upload" /> Lokal → Server (Server wird dem lokalen Ordner angeglichen)</label>
        <label class="opt"><input v-model="direction" type="radio" value="download" /> Server → Lokal (lokaler Ordner wird dem Server angeglichen)</label>
      </fieldset>
      <label class="opt"><input v-model="keepNewer" type="checkbox" /> Neuere Dateien im Ziel nicht überschreiben</label>
      <label class="opt danger"><input v-model="deleteExtra" type="checkbox" /> Dateien im Ziel löschen, die in der Quelle fehlen</label>

      <p v-if="error" class="err">{{ error }}</p>

      <template v-if="plan">
        <div class="summary">
          <template v-if="plan.actions.length">
            {{ plan.copyCount }} zu kopieren ({{ formatSize(plan.bytes) }})<template v-if="plan.deleteCount">, <b class="err">{{ plan.deleteCount }} zu löschen</b></template>
          </template>
          <template v-else>Die Verzeichnisse sind bereits identisch.</template>
          <span v-if="plan.note" class="muted"> · {{ plan.note }}</span>
        </div>
        <ul v-if="plan.actions.length" class="actions">
          <li v-for="a in shown" :key="a.kind + a.rel">
            <Icon :name="a.kind === 'delete' ? 'trash' : a.isDir ? 'folderPlus' : 'file'" :size="14" />
            <span class="rel" :title="a.rel">{{ a.rel }}</span>
            <span class="tag" :class="a.kind">{{ kindLabel[a.kind] }} · {{ reasonLabel[a.reason] ?? a.reason }}</span>
            <span class="muted size">{{ a.kind === 'copy' ? formatSize(a.size) : '' }}</span>
          </li>
          <li v-if="plan.actions.length > MAX_ROWS" class="muted">… und {{ plan.actions.length - MAX_ROWS }} weitere</li>
        </ul>
      </template>

      <div class="buttons">
        <button @click="emit('close')">Schließen</button>
        <button :disabled="busy" @click="compare">{{ plan ? 'Neu vergleichen' : 'Vergleichen' }}</button>
        <button class="primary" :disabled="busy || !plan || !plan.actions.length" @click="run">Synchronisieren</button>
      </div>
    </div>
  </div>
</template>
