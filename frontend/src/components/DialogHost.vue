<script setup lang="ts">
import {nextTick, ref, watch} from 'vue'
import {useUi} from '../stores/ui'

const ui = useUi()
const input = ref<HTMLInputElement>()
const value = ref('')

watch(
  () => ui.dialog,
  async (d) => {
    if (d?.kind === 'prompt') {
      value.value = d.value
      await nextTick()
      input.value?.focus()
      input.value?.select()
    }
  },
)

function finish(result: string | boolean | null) {
  const d = ui.dialog
  if (!d) return
  ui.dialog = null
  if (d.kind === 'confirm') d.resolve(result === true)
  else if (d.kind === 'prompt') d.resolve(typeof result === 'string' ? result : null)
  else d.resolve(typeof result === 'string' && result ? result : null)
}

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') finish(ui.dialog?.kind === 'confirm' ? false : null)
}
</script>

<template>
  <div v-if="ui.dialog" class="overlay" @keydown="onKey" @mousedown.self="finish(ui.dialog.kind === 'confirm' ? false : null)">
    <div class="modal small" role="dialog" aria-modal="true" :aria-label="ui.dialog.title">
      <h2>{{ ui.dialog.title }}</h2>

      <template v-if="ui.dialog.kind === 'prompt'">
        <p v-if="ui.dialog.message" class="msg">{{ ui.dialog.message }}</p>
        <form @submit.prevent="finish(value)">
          <input ref="input" v-model="value" :type="ui.dialog.secret ? 'password' : 'text'" autocomplete="off" spellcheck="false" />
          <div class="buttons">
            <button type="button" @click="finish(null)">Abbrechen</button>
            <button type="submit" class="primary">{{ ui.dialog.okLabel }}</button>
          </div>
        </form>
      </template>

      <template v-else-if="ui.dialog.kind === 'confirm'">
        <p class="msg">{{ ui.dialog.message }}</p>
        <div class="buttons">
          <button @click="finish(false)">Abbrechen</button>
          <button :class="ui.dialog.danger ? 'danger' : 'primary'" autofocus @click="finish(true)">{{ ui.dialog.okLabel }}</button>
        </div>
      </template>

      <template v-else>
        <p class="msg">{{ ui.dialog.message }}</p>
        <div class="buttons">
          <button v-for="o in ui.dialog.options" :key="o.label" :class="{primary: o.primary}" @click="finish(o.value)">{{ o.label }}</button>
        </div>
      </template>
    </div>
  </div>
</template>
