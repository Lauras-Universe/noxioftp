import * as App from '../wailsjs/go/main/App'

export {App}

function raw(e: unknown): string {
  if (typeof e === 'string') return e
  if (e instanceof Error) return e.message
  return String(e)
}

/** Error message without the machine-readable "E_CODE: " prefix. */
export function errMsg(e: unknown): string {
  return raw(e).replace(/^E_[A-Z_]+:\s*/, '')
}

/** The "E_CODE" prefix of a backend error, or "". */
export function errCode(e: unknown): string {
  return /^(E_[A-Z_]+):/.exec(raw(e))?.[1] ?? ''
}
