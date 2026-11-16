export function formatSize(n: number): string {
  if (n < 1024) return `${n} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let i = -1
  do {
    n /= 1024
    i++
  } while (n >= 1024 && i < units.length - 1)
  return `${n.toFixed(n < 10 ? 1 : 0)} ${units[i]}`
}

export function formatDate(sec: number): string {
  if (!sec) return ''
  return new Date(sec * 1000).toLocaleString('de-DE', {dateStyle: 'short', timeStyle: 'short'})
}

/** Joins a directory and a name using the separator the directory already uses. */
export function joinPath(base: string, name: string): string {
  const sep = base.includes('\\') && !base.includes('/') ? '\\' : '/'
  return base.endsWith(sep) ? base + name : base + sep + name
}

export function baseName(p: string): string {
  const parts = p.split(/[\\/]/).filter(Boolean)
  return parts[parts.length - 1] ?? p
}

/** Returns an error message for an invalid file name, or null. */
export function validateName(name: string): string | null {
  if (!name.trim()) return 'Der Name darf nicht leer sein'
  if (/[\\/]/.test(name)) return 'Der Name darf kein „/“ oder „\\“ enthalten'
  if (name === '.' || name === '..') return 'Ungültiger Name'
  return null
}

export function octal(perm: number): string {
  return (perm & 0o7777).toString(8)
}

function segments(p: string): string[] {
  return p.split(/[\\/]/).filter(Boolean)
}

/** Parent of a local or remote path ("/" or "C:\\" at the top). */
export function parentPath(p: string): string {
  const win = p.includes('\\') && !p.includes('/')
  const sep = win ? '\\' : '/'
  const trimmed = p.length > 1 && p.endsWith(sep) ? p.slice(0, -1) : p
  const i = trimmed.lastIndexOf(sep)
  if (i < 0) return p
  const head = trimmed.slice(0, i)
  if (win) return /^[A-Za-z]:$/.test(head) ? head + sep : head || sep
  return head || sep
}

/**
 * How `next` relates to `prev`: `{down: [...]}` if it is a sub-folder,
 * `{up: n}` if it is an ancestor n levels up, otherwise null.
 */
export function relation(prev: string, next: string): {down: string[]} | {up: number} | null {
  const a = segments(prev)
  const b = segments(next)
  if (b.length > a.length && a.every((s, i) => s === b[i])) return {down: b.slice(a.length)}
  if (a.length > b.length && b.every((s, i) => s === a[i])) return {up: a.length - b.length}
  return null
}

/** Applies a relation (from another pane) to this path. */
export function applyRelation(base: string, rel: {down: string[]} | {up: number}): string {
  if ('down' in rel) return rel.down.reduce((acc, seg) => joinPath(acc, seg), base)
  let out = base
  for (let i = 0; i < rel.up; i++) out = parentPath(out)
  return out
}

export const PROTOCOLS: Array<{value: string; label: string}> = [
  {value: 'sftp', label: 'SFTP (SSH)'},
  {value: 'ftps', label: 'FTPS – explizit (TLS)'},
  {value: 'ftps-implicit', label: 'FTPS – implizit (TLS)'},
  {value: 'ftp', label: 'FTP – unverschlüsselt'},
]

export function defaultPort(protocol: string): number {
  return protocol === 'sftp' ? 22 : protocol === 'ftps-implicit' ? 990 : 21
}
