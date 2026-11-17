<script setup lang="ts">
import {computed, onBeforeUnmount, onMounted, ref, watch} from 'vue'
import {App} from './api'
import {EventsOn} from '../wailsjs/runtime/runtime'
import logo from './assets/logo.png'
import AboutDialog from './components/AboutDialog.vue'
import ConnectionManager from './components/ConnectionManager.vue'
import DialogHost from './components/DialogHost.vue'
import EditorView from './components/EditorView.vue'
import FilePane from './components/FilePane.vue'
import Icon from './components/Icon.vue'
import SyncDialog from './components/SyncDialog.vue'
import ToastHost from './components/ToastHost.vue'
import TransferQueue from './components/TransferQueue.vue'
import UpdateDialog from './components/UpdateDialog.vue'
import {useConnection} from './stores/connection'
import {useEditor} from './stores/editor'
import {usePanes} from './stores/panes'
import {useTransfers} from './stores/transfers'
import {useUpdate} from './stores/update'
import {useUi} from './stores/ui'

const conn = useConnection()
const editor = useEditor()
const transfers = useTransfers()
const ui = useUi()
const panes = usePanes()
const update = useUpdate()

const showManager = ref(false)
const showSync = ref(false)
const showAbout = ref(false)
const selectedId = ref('')
const offs: Array<() => void> = []

// the backend asks before the window closes while there is unsaved work
watch(
  () => editor.tabs.some((t) => t.dirty),
  (dirty) => void App.SetDirty(dirty),
)

const status = computed(() => {
  if (conn.busy) return {text: 'Verbinde …', cls: 'busy'}
  if (conn.connected && conn.lost) return {text: 'Verbindung verloren', cls: 'lost'}
  if (conn.connected) return {text: conn.active?.name ?? 'Verbunden', cls: 'ok'}
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
  void update.init()
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
      <button class="brand" title="Über Noxio FTP" aria-label="Über Noxio FTP" @click="showAbout = true"><img :src="logo" alt="" width="26" height="26" /><span>Noxio FTP</span></button>
      <button @click="showManager = true"><Icon name="plug" /> Verbindungen …</button>
      <select v-model="selectedId" :disabled="conn.connected || conn.busy" aria-label="Profil">
        <option v-if="!conn.profiles.length" value="">Kein Profil</option>
        <option v-for="p in conn.profiles" :key="p.id" :value="p.id">{{ p.name }} ({{ p.user }}@{{ p.host }})</option>
      </select>
      <button class="primary" :disabled="conn.busy" @click="toggleConnection">{{ conn.connected ? 'Trennen' : 'Verbinden' }}</button>
      <span class="sep-v" />
      <label class="check" title="Beim Navigieren in einem Fenster folgt das andere Fenster"><input v-model="panes.syncBrowse" type="checkbox" /> Synchron browsen</label>
      <button class="toggle" :aria-pressed="panes.compare" title="Unterschiede zwischen den beiden Ordnern farbig markieren" @click="panes.compare = !panes.compare">Vergleichen</button>
      <button :disabled="!conn.connected" title="Lokalen und Server-Ordner abgleichen" @click="showSync = true">Abgleichen …</button>
      <span class="spacer" />
      <span class="status" :class="status.cls" :title="conn.connected ? `Verbunden mit ${conn.active?.name}` : undefined"><i class="dot" />{{ status.text }}</span>
      <button class="ghost version" :title="update.info?.available ? 'Update verfügbar – klicken' : 'Nach Updates suchen'" :disabled="update.checking" @click="update.info?.available ? (update.open = true) : update.check()">
        {{ update.version }}<i v-if="update.info?.available" class="dot new" />
      </button>
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
    <SyncDialog v-if="showSync" @close="showSync = false" />
    <UpdateDialog />
    <AboutDialog v-if="showAbout" @close="showAbout = false" />
    <DialogHost />
    <ToastHost />
  </div>
</template>
