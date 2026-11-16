import {defineStore} from 'pinia'
import {reactive} from 'vue'

export type Side = 'local' | 'remote'

/**
 * Shared state of the two file panes: which directory each one should show
 * (`req`, bumping `n` forces a reload) and which one it actually shows (`cur`).
 */
export const usePanes = defineStore('panes', () => {
  const req = reactive({local: {path: '', n: 0}, remote: {path: '', n: 0}})
  const cur = reactive({local: '', remote: ''})

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
    req.remote.n++
  }

  return {req, cur, go, refresh, clearRemote}
})
