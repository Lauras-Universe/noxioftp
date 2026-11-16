<script setup lang="ts">
import {onBeforeUnmount, onMounted, ref, watch} from 'vue'
import {monaco, monacoTheme} from '../monaco'
import {useEditor} from '../stores/editor'
import Icon from './Icon.vue'

const props = defineProps<{visible: boolean}>()
const editor = useEditor()

const el = ref<HTMLDivElement>()
const pos = ref('')
let ed: monaco.editor.IStandaloneCodeEditor | null = null
const dark = window.matchMedia('(prefers-color-scheme: dark)')
const onTheme = () => monaco.editor.setTheme(monacoTheme())

onMounted(() => {
  ed = monaco.editor.create(el.value!, {
    model: null,
    theme: monacoTheme(),
    automaticLayout: true,
    fontSize: 13,
    minimap: {enabled: true},
    scrollBeyondLastLine: false,
    renderWhitespace: 'selection',
    tabSize: 4,
  })
  // Cmd+S / Ctrl+S saves back to the server.
  ed.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS, () => void editor.save())
  ed.onDidChangeCursorPosition((e) => {
    pos.value = `Zeile ${e.position.lineNumber}, Spalte ${e.position.column}`
  })
  dark.addEventListener('change', onTheme)

  watch(
    () => editor.current,
    (key) => {
      ed?.setModel(key ? editor.modelFor(key) : null)
      if (props.visible) ed?.focus()
    },
    {immediate: true},
  )
})

watch(
  () => props.visible,
  (v) => {
    if (v) {
      ed?.layout()
      ed?.focus()
    }
  },
)

onBeforeUnmount(() => {
  dark.removeEventListener('change', onTheme)
  ed?.dispose()
})

const tab = () => editor.tabs.find((t) => t.key === editor.current)
</script>

<template>
  <div class="editor-view" v-show="visible">
    <div v-if="tab()" class="editor-bar">
      <span class="mono path" :title="tab()!.path">{{ tab()!.profileName }}: {{ tab()!.path }}</span>
      <span class="spacer" />
      <span class="muted">{{ pos }}</span>
      <button class="primary" :disabled="!tab()!.dirty || tab()!.saving" @click="editor.save()">
        <Icon name="save" /> {{ tab()!.saving ? 'Speichert …' : 'Speichern' }}
      </button>
    </div>
    <div ref="el" class="monaco-host" />
    <p v-if="!tab()" class="placeholder empty-editor">Remote-Datei per Doppelklick im Server-Fenster öffnen.</p>
  </div>
</template>
