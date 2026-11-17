<script setup lang="ts">
import {computed, onBeforeUnmount, onMounted, ref, watch} from 'vue'
import {App, errMsg} from '../api'
import {baseName, formatDate, formatSize, joinPath, octal, validateName} from '../format'
import {useConnection} from '../stores/connection'
import {useEditor} from '../stores/editor'
import {type Side, usePanes} from '../stores/panes'
import {useTransfers} from '../stores/transfers'
import {useUi} from '../stores/ui'
import type {fsinfo} from '../../wailsjs/go/models'
import Icon from './Icon.vue'

const props = defineProps<{side: Side}>()

const ui = useUi()
const panes = usePanes()
const conn = useConnection()
const transfers = useTransfers()
const editor = useEditor()

const isRemote = computed(() => props.side === 'remote')
const title = computed(() => (isRemote.value ? 'Server' : 'Lokal'))

const listing = ref<fsinfo.Listing | null>(null)
const loading = ref(false)
const error = ref('')
const selected = ref<Set<string>>(new Set())
const anchor = ref(-1)
const sortKey = ref<'name' | 'size' | 'modTime' | 'perm'>('name')
const sortAsc = ref(true)
const editingPath = ref(false)
const pathInput = ref('')
const pathEl = ref<HTMLInputElement>()
const roots = ref<string[]>([])
const dragOver = ref<string | null>(null) // '' = whole pane, otherwise dir path
const menu = ref<{x: number; y: number} | null>(null)
const root = ref<HTMLElement>()

const available = computed(() => !isRemote.value || conn.connected)
/** Show the mode column on SFTP, and on FTP when the server's listing contained permissions (not the case for DOS/IIS style servers). */
const hasPerms = computed(() => isRemote.value && (conn.active?.protocol === 'sftp' || (listing.value?.entries ?? []).some((e) => e.mode)))
/** chmod works on every remote protocol (FTP via SITE CHMOD, if the server allows it). */
const canChmod = computed(() => isRemote.value)

// ------------------------------------------------------------- loading

async function load() {
  const path = panes.req[props.side].path
  if (isRemote.value && (!conn.connected || !path)) {
    listing.value = null
    panes.entries[props.side] = []
    error.value = ''
    return
  }
  if (!path) return
  loading.value = true
  try {
    const l = isRemote.value ? await App.RemoteList(path) : await App.LocalList(path)
    listing.value = l
    panes.entries[props.side] = l.entries
    error.value = ''
    panes.cur[props.side] = l.path
    panes.req[props.side].path = l.path
    const names = new Set(l.entries.map((e) => e.path))
    selected.value = new Set([...selected.value].filter((p) => names.has(p)))
  } catch (e) {
    if (listing.value) {
      ui.error(e) // stay where we were
      panes.req[props.side].path = panes.cur[props.side]
    } else {
      error.value = errMsg(e)
    }
  } finally {
    loading.value = false
  }
}

watch(() => panes.req[props.side].n, load)
watch(
  () => conn.connected,
  (c) => {
    if (isRemote.value && !c) {
      listing.value = null
      panes.entries.remote = []
      selected.value = new Set()
    }
  },
)

onMounted(async () => {
  if (!isRemote.value) {
    roots.value = (await App.LocalRoots()) ?? []
    try {
      panes.go('local', await App.LocalHome())
    } catch (e) {
      ui.error(e)
      panes.go('local', roots.value[0] ?? '/')
    }
  }
  window.addEventListener('click', closeMenu)
  window.addEventListener('blur', closeMenu)
})
onBeforeUnmount(() => {
  window.removeEventListener('click', closeMenu)
  window.removeEventListener('blur', closeMenu)
})

// ------------------------------------------------------------- display

const rows = computed(() => {
  const list = [...(listing.value?.entries ?? [])]
  const dir = sortAsc.value ? 1 : -1
  list.sort((a, b) => {
    if (a.isDir !== b.isDir) return a.isDir ? -1 : 1
    let r: number
    switch (sortKey.value) {
      case 'size':
        r = a.size - b.size
        break
      case 'modTime':
        r = a.modTime - b.modTime
        break
      case 'perm':
        r = a.perm - b.perm
        break
      default:
        r = a.name.toLowerCase().localeCompare(b.name.toLowerCase(), 'de', {numeric: true})
    }
    return r * dir || a.name.localeCompare(b.name)
  })
  return list
})

const selectedEntries = computed(() => rows.value.filter((r) => selected.value.has(r.path)))

function sortBy(k: typeof sortKey.value) {
  if (sortKey.value === k) sortAsc.value = !sortAsc.value
  else {
    sortKey.value = k
    sortAsc.value = true
  }
}
function ariaSort(k: string) {
  return sortKey.value === k ? (sortAsc.value ? 'ascending' : 'descending') : 'none'
}

// ----------------------------------------------------------- comparison

const TOLERANCE = 2 // seconds: servers round timestamps

/** Colour hint for a row when the directory comparison is on. */
function cmpClass(r: fsinfo.Entry): string {
  if (!panes.compare) return ''
  const otherList = panes.entries[isRemote.value ? 'local' : 'remote']
  if (!otherList.length && !(isRemote.value ? panes.cur.local : panes.cur.remote)) return ''
  const o = otherList.find((e) => e.name === r.name)
  if (!o) return 'cmp-only'
  if (r.isDir !== o.isDir) return 'cmp-diff'
  if (r.isDir) return ''
  if (r.size !== o.size) return 'cmp-diff'
  if (r.modTime > o.modTime + TOLERANCE) return 'cmp-newer'
  if (r.modTime + TOLERANCE < o.modTime) return 'cmp-older'
  return ''
}

// ------------------------------------------------------------ selection

function onRowClick(e: MouseEvent, row: fsinfo.Entry, i: number) {
  root.value?.focus()
  if (e.shiftKey && anchor.value >= 0) {
    const [a, b] = [Math.min(anchor.value, i), Math.max(anchor.value, i)]
    selected.value = new Set(rows.value.slice(a, b + 1).map((r) => r.path))
  } else if (e.metaKey || e.ctrlKey) {
    const s = new Set(selected.value)
    if (!s.delete(row.path)) s.add(row.path)
    selected.value = s
    anchor.value = i
  } else {
    selected.value = new Set([row.path])
    anchor.value = i
  }
}

function onKey(e: KeyboardEvent) {
  if (editingPath.value) return
  const mod = e.metaKey || e.ctrlKey
  const idx = rows.value.findIndex((r) => selected.value.has(r.path))
  switch (e.key) {
    case 'ArrowDown':
    case 'ArrowUp': {
      e.preventDefault()
      if (!rows.value.length) return
      const next = Math.max(0, Math.min(rows.value.length - 1, (idx < 0 ? (e.key === 'ArrowDown' ? -1 : rows.value.length) : idx) + (e.key === 'ArrowDown' ? 1 : -1)))
      if (e.shiftKey && anchor.value >= 0) {
        const [a, b] = [Math.min(anchor.value, next), Math.max(anchor.value, next)]
        selected.value = new Set(rows.value.slice(a, b + 1).map((r) => r.path))
      } else {
        selected.value = new Set([rows.value[next].path])
        anchor.value = next
      }
      root.value?.querySelector<HTMLElement>(`[data-i="${next}"]`)?.scrollIntoView({block: 'nearest'})
      break
    }
    case 'Enter':
      if (selectedEntries.value.length === 1) openEntry(selectedEntries.value[0])
      break
    case 'Backspace':
      if (mod) void doDelete()
      else void goUp()
      break
    case 'Delete':
      void doDelete()
      break
    case 'F2':
      void doRename()
      break
    case 'F5':
      panes.refresh(props.side)
      break
    case 'a':
      if (mod) {
        e.preventDefault()
        selected.value = new Set(rows.value.map((r) => r.path))
      }
      break
  }
}

// ----------------------------------------------------------- navigation

function goUp() {
  if (listing.value && listing.value.parent !== listing.value.path) panes.navigate(props.side, listing.value.parent)
}
async function goHome() {
  if (isRemote.value) {
    const home = conn.active?.remoteDir || '.'
    panes.go('remote', home === '.' ? '/' : home)
  } else panes.go('local', await App.LocalHome())
}
function startPathEdit() {
  if (!listing.value) return
  pathInput.value = listing.value.path
  editingPath.value = true
  setTimeout(() => pathEl.value?.select(), 0)
}
function commitPathEdit() {
  editingPath.value = false
  const p = pathInput.value.trim()
  if (p && p !== listing.value?.path) panes.navigate(props.side, p)
}

function openEntry(row: fsinfo.Entry) {
  if (row.isDir) panes.navigate(props.side, row.path)
  else if (isRemote.value) void editor.open(row.path)
  else if (conn.connected) void transfers.send('upload', [row.path])
  else ui.toast('Nicht verbunden', 'error')
}

// -------------------------------------------------------------- actions

const dirPath = () => listing.value?.path ?? ''

async function run(fn: () => Promise<unknown>) {
  try {
    await fn()
  } catch (e) {
    ui.error(e)
  } finally {
    panes.refresh(props.side)
  }
}

async function doMkdir() {
  const name = await ui.prompt('Neuer Ordner', '', {okLabel: 'Anlegen'})
  if (name === null) return
  const bad = validateName(name)
  if (bad) return ui.toast(bad, 'error')
  const p = joinPath(dirPath(), name.trim())
  await run(() => (isRemote.value ? App.RemoteMkdir(p) : App.LocalMkdir(p)))
}

async function doNewFile() {
  const name = await ui.prompt('Neue Datei', '', {okLabel: 'Anlegen'})
  if (name === null) return
  const bad = validateName(name)
  if (bad) return ui.toast(bad, 'error')
  const p = joinPath(dirPath(), name.trim())
  if (rows.value.some((r) => r.name === name.trim())) return ui.toast('Existiert bereits', 'error')
  await run(() => App.RemoteWriteText(p, '', 0, true))
  void editor.open(p)
}

async function doRename() {
  if (selectedEntries.value.length !== 1) return
  const e = selectedEntries.value[0]
  const name = await ui.prompt('Umbenennen', e.name, {okLabel: 'Umbenennen'})
  if (name === null || name === e.name) return
  const bad = validateName(name)
  if (bad) return ui.toast(bad, 'error')
  const to = joinPath(dirPath(), name.trim())
  await run(async () => {
    if (isRemote.value) {
      await App.RemoteRename(e.path, to)
      editor.renamed(e.path, to)
    } else await App.LocalRename(e.path, to)
  })
}

async function doDelete() {
  const items = selectedEntries.value
  if (!items.length) return
  const names = items.slice(0, 5).map((i) => i.name).join('\n') + (items.length > 5 ? `\n… und ${items.length - 5} weitere` : '')
  const where = isRemote.value ? 'auf dem Server' : 'auf diesem Computer'
  const ok = await ui.confirm('Endgültig löschen?', `${items.length === 1 ? 'Dieses Element' : `Diese ${items.length} Elemente`} ${where} unwiderruflich löschen (Ordner samt Inhalt)?\n\n${names}`, {
    okLabel: 'Löschen',
    danger: true,
  })
  if (!ok) return
  const paths = items.map((i) => i.path)
  await run(() => (isRemote.value ? App.RemoteDelete(paths) : App.LocalDelete(paths)))
}

async function doChmod() {
  const items = selectedEntries.value
  if (!items.length) return
  const known = !!items[0].mode // some FTP servers do not list permissions
  const v = await ui.prompt('Zugriffsrechte (oktal)', known ? octal(items[0].perm) : '644', {
    message: 'Z. B. 644 für Dateien, 755 für Ordner und Skripte.' + (conn.active?.protocol === 'sftp' ? '' : '\nBei FTP muss der Server SITE CHMOD unterstützen.'),
  })
  if (v === null) return
  await run(async () => {
    for (const i of items) await App.RemoteChmod(i.path, v.trim())
  })
}

function doTransfer() {
  const paths = selectedEntries.value.map((r) => r.path)
  void transfers.send(isRemote.value ? 'download' : 'upload', paths)
}

async function copyPath() {
  const p = selectedEntries.value[0]?.path ?? dirPath()
  try {
    await navigator.clipboard.writeText(p)
    ui.toast('Pfad kopiert', 'success', 2000)
  } catch {
    ui.toast('Zwischenablage nicht verfügbar', 'error')
  }
}

// ---------------------------------------------------------- context menu

function onContext(e: MouseEvent, row?: fsinfo.Entry) {
  e.preventDefault()
  root.value?.focus()
  if (row && !selected.value.has(row.path)) {
    selected.value = new Set([row.path])
    anchor.value = rows.value.indexOf(row)
  } else if (!row) selected.value = new Set()
  const w = 220
  menu.value = {x: Math.min(e.clientX, window.innerWidth - w - 8), y: Math.min(e.clientY, window.innerHeight - 330)}
}
function closeMenu() {
  menu.value = null
}

const canTransfer = computed(() => selectedEntries.value.length > 0 && (!isRemote.value ? conn.connected : true))

// ------------------------------------------------------------ drag & drop

const MIME = 'application/x-sftp-items'

function onDragStart(e: DragEvent, row: fsinfo.Entry) {
  if (!selected.value.has(row.path)) selected.value = new Set([row.path])
  const paths = selectedEntries.value.map((r) => r.path)
  e.dataTransfer?.setData(MIME, JSON.stringify({side: props.side, paths}))
  e.dataTransfer?.setData('text/plain', paths.join('\n'))
  if (e.dataTransfer) e.dataTransfer.effectAllowed = 'copyMove'
  panes.dragSide = props.side
}
function onDragEnd() {
  panes.dragSide = null
  dragOver.value = null
}
/** Dragging inside this pane moves; dragging from the other pane transfers. */
function acceptsDrag(e: DragEvent, target: string) {
  if (!e.dataTransfer?.types.includes(MIME) || !available.value) return false
  if (panes.dragSide === props.side) {
    // moving: only onto a folder row / path segment that is not itself being dragged
    return !!target && !selected.value.has(target)
  }
  return true
}
function onDragOver(e: DragEvent, target: string) {
  if (!acceptsDrag(e, target)) return
  e.preventDefault()
  e.stopPropagation()
  if (e.dataTransfer) e.dataTransfer.dropEffect = panes.dragSide === props.side ? 'move' : 'copy'
  dragOver.value = target
}

async function moveItems(paths: string[], dstDir: string) {
  await run(async () => {
    if (isRemote.value) {
      await App.RemoteMove(paths, dstDir)
      for (const p of paths) editor.renamed(p, joinPath(dstDir, baseName(p)))
    } else await App.LocalMove(paths, dstDir)
  })
}

function onDrop(e: DragEvent, dstDir: string) {
  dragOver.value = null
  const raw = e.dataTransfer?.getData(MIME)
  if (!raw || !available.value) return
  e.preventDefault()
  e.stopPropagation()
  const data = JSON.parse(raw) as {side: Side; paths: string[]}
  if (data.side === props.side) {
    if (dstDir && !data.paths.includes(dstDir)) void moveItems(data.paths, dstDir)
    return
  }
  void transfers.send(data.side === 'local' ? 'upload' : 'download', data.paths, dstDir || undefined)
}
</script>

<template>
  <section
    ref="root"
    class="pane"
    :class="{'drop-target': dragOver === ''}"
    tabindex="0"
    :aria-label="title"
    @keydown="onKey"
    @dragover="onDragOver($event, '')"
    @dragleave.self="dragOver = null"
    @drop="onDrop($event, '')"
  >
    <header class="pane-head">
      <strong class="pane-title">{{ title }}</strong>
      <select v-if="!isRemote && roots.length > 1" class="roots" aria-label="Laufwerk" @change="panes.go('local', ($event.target as HTMLSelectElement).value)">
        <option v-for="r in roots" :key="r" :value="r">{{ r }}</option>
      </select>
      <span class="spacer" />
      <button class="icon-btn" title="Übergeordneter Ordner" :disabled="!listing || listing.parent === listing.path" @click="goUp"><Icon name="up" /></button>
      <button class="icon-btn" title="Startverzeichnis" :disabled="!available" @click="goHome"><Icon name="home" /></button>
      <button class="icon-btn" title="Aktualisieren (F5)" :disabled="!available" @click="panes.refresh(side)"><Icon name="refresh" /></button>
      <button class="icon-btn" title="Neuer Ordner" :disabled="!listing" @click="doMkdir"><Icon name="folderPlus" /></button>
    </header>

    <div v-if="listing" class="pathbar">
      <input v-if="editingPath" ref="pathEl" v-model="pathInput" class="path-input" spellcheck="false" aria-label="Pfad" @keydown.enter="commitPathEdit" @keydown.esc="editingPath = false" @blur="editingPath = false" />
      <nav v-else class="crumbs" aria-label="Pfad" @dblclick.self="startPathEdit">
        <button
          v-for="c in listing.crumbs"
          :key="c.path"
          class="crumb"
          :class="{drop: dragOver === c.path}"
          @click="panes.navigate(side, c.path)"
          @dragover="onDragOver($event, c.path)"
          @dragleave="dragOver = null"
          @drop="onDrop($event, c.path)"
        >{{ c.name }}</button>
        <span class="crumb-fill" @click="startPathEdit" />
      </nav>
    </div>

    <div v-if="!available" class="placeholder">
      <Icon name="plug" :size="32" />
      <p>Nicht verbunden</p>
      <p class="muted">Oben ein Profil wählen und „Verbinden“ klicken.</p>
    </div>
    <div v-else-if="error" class="placeholder">
      <p class="err">{{ error }}</p>
      <button @click="panes.refresh(side)">Erneut versuchen</button>
    </div>
    <div v-else class="table-wrap" :class="{loading}" @contextmenu="onContext($event)" @click.self="selected = new Set()">
      <table class="files" role="grid" :aria-label="`Dateien (${title})`">
        <thead>
          <tr>
            <th :aria-sort="ariaSort('name')" @click="sortBy('name')">Name</th>
            <th class="num" :aria-sort="ariaSort('size')" @click="sortBy('size')">Größe</th>
            <th :aria-sort="ariaSort('modTime')" @click="sortBy('modTime')">Geändert</th>
            <th v-if="hasPerms" :aria-sort="ariaSort('perm')" @click="sortBy('perm')">Rechte</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="(r, i) in rows"
            :key="r.path"
            :data-i="i"
            :class="[cmpClass(r), {selected: selected.has(r.path), 'drop-dir': dragOver === r.path}]"
            :aria-selected="selected.has(r.path)"
            draggable="true"
            @click="onRowClick($event, r, i)"
            @dblclick="openEntry(r)"
            @contextmenu.stop="onContext($event, r)"
            @dragstart="onDragStart($event, r)"
            @dragover="r.isDir ? onDragOver($event, r.path) : undefined"
            @dragleave="dragOver = null"
            @dragend="onDragEnd"
            @drop="r.isDir ? onDrop($event, r.path) : undefined"
          >
            <td class="name" :title="r.name">
              <Icon :name="r.isDir ? 'folder' : 'file'" :class="r.isDir ? 'dir' : ''" />
              <span class="label">{{ r.name }}</span>
              <Icon v-if="r.isLink" name="link" :size="12" class="muted" />
            </td>
            <td class="num">{{ r.isDir ? '' : formatSize(r.size) }}</td>
            <td>{{ formatDate(r.modTime) }}</td>
            <td v-if="hasPerms" class="mono">{{ r.mode }}</td>
          </tr>
          <tr v-if="!rows.length && !loading" class="empty-row"><td :colspan="hasPerms ? 4 : 3">Ordner ist leer</td></tr>
        </tbody>
      </table>
    </div>

    <footer class="pane-foot muted">
      <template v-if="listing">{{ rows.length }} Einträge<template v-if="selected.size"> · {{ selected.size }} ausgewählt</template></template>
      <span class="spacer" />
      <span v-if="panes.compare" class="legend" title="Farbmarkierung des Vergleichs">
        <span class="only"><i />nur hier</span><span class="diff"><i />Größe anders</span><span class="newer"><i />neuer</span><span class="older"><i />älter</span>
      </span>
    </footer>

    <Teleport to="body">
      <ul v-if="menu" class="menu" role="menu" :style="{left: menu.x + 'px', top: menu.y + 'px'}" @click.stop @contextmenu.prevent>
        <template v-if="selectedEntries.length">
          <li v-if="selectedEntries.length === 1 && selectedEntries[0].isDir" role="menuitem" @click="openEntry(selectedEntries[0]); closeMenu()"><Icon name="folder" />Öffnen</li>
          <li v-if="isRemote && selectedEntries.length === 1 && !selectedEntries[0].isDir" role="menuitem" @click="openEntry(selectedEntries[0]); closeMenu()"><Icon name="edit" />Bearbeiten</li>
          <li v-if="canTransfer" role="menuitem" @click="doTransfer(); closeMenu()">
            <Icon :name="isRemote ? 'download' : 'upload'" />{{ isRemote ? 'Herunterladen' : 'Hochladen' }}
          </li>
          <li class="sep" />
          <li v-if="selectedEntries.length === 1" role="menuitem" @click="doRename(); closeMenu()"><Icon name="pencil" />Umbenennen<kbd>F2</kbd></li>
          <li v-if="canChmod" role="menuitem" @click="doChmod(); closeMenu()"><Icon name="lock" />Rechte ändern …</li>
          <li role="menuitem" @click="copyPath(); closeMenu()"><Icon name="link" />Pfad kopieren</li>
          <li role="menuitem" class="danger" @click="doDelete(); closeMenu()"><Icon name="trash" />Löschen<kbd>Entf</kbd></li>
          <li class="sep" />
        </template>
        <li role="menuitem" @click="doMkdir(); closeMenu()"><Icon name="folderPlus" />Neuer Ordner</li>
        <li v-if="isRemote" role="menuitem" @click="doNewFile(); closeMenu()"><Icon name="filePlus" />Neue Datei</li>
        <li role="menuitem" @click="panes.refresh(side); closeMenu()"><Icon name="refresh" />Aktualisieren<kbd>F5</kbd></li>
      </ul>
    </Teleport>
  </section>
</template>
