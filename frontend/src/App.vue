<script setup lang="ts">
import {computed, onBeforeUnmount, onMounted, ref} from 'vue'
import {EventsOn} from '../wailsjs/runtime/runtime'
import ConnectionManager from './components/ConnectionManager.vue'
import DialogHost from './components/DialogHost.vue'
import EditorView from './components/EditorView.vue'
import FilePane from './components/FilePane.vue'
import Icon from './components/Icon.vue'
import ToastHost from './components/ToastHost.vue'
import TransferQueue from './components/TransferQueue.vue'
import {useConnection} from './stores/connection'
import {useEditor} from './stores/editor'
import {useTransfers} from './stores/transfers'
import {useUi} from './stores/ui'

const conn = useConnection()
const editor = useEditor()
const transfers = useTransfers()
const ui = useUi()

const showManager = ref(false)
const selectedId = ref('')
const offs: Array<() => void> = []

const status = computed(() => {
  if (conn.busy) return {text: 'Verbinde …', cls: 'busy'}
  if (conn.connected && conn.lost) return {text: 'Verbindung verloren', cls: 'lost'}
  if (conn.connected) return {text: `Verbunden mit ${conn.active?.name}`, cls: 'ok'}
  return {text: 'Nicht verbunden', cls: ''}
})

onMounted(async () => {
  offs.push(EventsOn('transfer:update', (j) => transfers.onUpdate(j)))
  offs.push(EventsOn('connection:lost', () => conn.markLost()))
  try {
    await Promise.all([conn.load(), transfers.load()])
    selectedId.value = conn.profiles[0]?.id ?? ''
  } catch (e) {
    ui.error(e)
  }
  window.addEventListener('keydown', onKey)
})
onBeforeUnmount(() => {
  offs.forEach((f) => f())
  window.removeEventListener('keydown', onKey)
})

function onKey(e: KeyboardEvent) {
  // Cmd/Ctrl+W closes the active editor tab
  if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'w' && editor.current) {
    e.preventDefault()
    void editor.close(editor.current)
  }
}

async function toggleConnection() {
  if (conn.connected) await conn.disconnect()
  else if (selectedId.value) await conn.connect(selectedId.value)
  else showManager.value = true
}

function closeManager() {
  showManager.value = false
  if (!conn.profiles.some((p) => p.id === selectedId.value)) selectedId.value = conn.profiles[0]?.id ?? ''
}
</script>

<template>
  <div class="app">
    <header class="toolbar">
      <button @click="showManager = true"><Icon name="plug" /> Verbindungen …</button>
      <select v-model="selectedId" :disabled="conn.connected || conn.busy" aria-label="Profil">
        <option v-if="!conn.profiles.length" value="">Kein Profil</option>
        <option v-for="p in conn.profiles" :key="p.id" :value="p.id">{{ p.name }} ({{ p.user }}@{{ p.host }})</option>
      </select>
      <button class="primary" :disabled="conn.busy" @click="toggleConnection">{{ conn.connected ? 'Trennen' : 'Verbinden' }}</button>
      <span class="spacer" />
      <span class="status" :class="status.cls"><i class="dot" />{{ status.text }}</span>
    </header>

    <nav class="tabs" role="tablist">
      <button role="tab" class="tab" :class="{active: editor.current === 0}" :aria-selected="editor.current === 0" @click="editor.current = 0">Dateien</button>
      <div v-for="t in editor.tabs" :key="t.key" role="tab" class="tab" :class="{active: editor.current === t.key}" :aria-selected="editor.current === t.key" :title="`${t.profileName}: ${t.path}`" @click="editor.current = t.key" @auxclick.middle="editor.close(t.key)">
        <Icon name="file" :size="13" />
        <span>{{ t.name }}</span>
        <i v-if="t.dirty" class="dirty" title="Ungespeichert" />
        <button class="icon-btn small" :aria-label="`${t.name} schließen`" @click.stop="editor.close(t.key)"><Icon name="close" :size="12" /></button>
      </div>
    </nav>

    <main>
      <div v-show="editor.current === 0" class="files-view">
        <div class="panes">
          <FilePane side="local" />
          <FilePane side="remote" />
        </div>
        <TransferQueue />
      </div>
      <EditorView :visible="editor.current !== 0" />
    </main>

    <ConnectionManager v-if="showManager" @close="closeManager" />
    <DialogHost />
    <ToastHost />
  </div>
</template>
