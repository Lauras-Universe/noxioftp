import {defineStore} from 'pinia'
import {reactive, ref} from 'vue'
import {applyRelation, relation} from '../format'
import type {fsinfo} from '../../wailsjs/go/models'

export type Side = 'local' | 'remote'

/**
 * Shared state of the two file panes: which directory each one should show
 * (`req`, bumping `n` forces a reload) and which one it actually shows (`cur`).
 */
export const usePanes = defineStore('panes', () => {
  const req = reactive({local: {path: '', n: 0}, remote: {path: '', n: 0}})
  const cur = reactive({local: '', remote: ''})
  /** Entries currently shown, for the directory comparison. */
  const entries = reactive<{local: fsinfo.Entry[]; remote: fsinfo.Entry[]}>({local: [], remote: []})
  const syncBrowse = ref(false)
  const compare = ref(false)
  /** Which pane a drag started in (the drag data is not readable during dragover). */
  const dragSide = ref<Side | null>(null)

  function go(side: Side, path: string) {
    req[side].path = path
    req[side].n++
  }
  function refresh(side: Side) {
    req[side].n++
  }
  function clearRemote() {
    req.remote.path = ''
    cur.remote = ''
    entries.remote = []
    req.remote.n++
  }

  /** User navigation in a pane: with synchronized browsing the other pane follows. */
  function navigate(side: Side, path: string) {
    const prev = cur[side]
    go(side, path)
    if (!syncBrowse.value || !prev) return
    const rel = relation(prev, path)
    const other: Side = side === 'local' ? 'remote' : 'local'
    if (rel && cur[other]) go(other, applyRelation(cur[other], rel))
  }

  return {req, cur, entries, syncBrowse, compare, dragSide, go, refresh, clearRemote, navigate}
})
