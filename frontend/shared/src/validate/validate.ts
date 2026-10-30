// Field validators: return an error message, or null when valid. Messages are shown as-is under the input.
type Check = (value: string) => string | null

const EMAIL = /^[^\s@]+@[^\s@]+\.[^\s@]+$/
const USERNAME = /^[a-z0-9][a-z0-9_.-]*$/i

export const required = (label: string): Check => (v) => (v.trim() ? null : `${label} is required.`)

export const email: Check = (v) => {
  const s = v.trim()
  if (!s) return 'Email is required.'
  if (s.length > 254 || !EMAIL.test(s)) return 'Enter a valid email address.'
  return null
}

export const username: Check = (v) => {
  const s = v.trim()
  if (!s) return 'Username is required.'
  if (s.length < 3 || s.length > 32) return 'Username must be 3 to 32 characters.'
  if (!USERNAME.test(s)) return 'Use letters, numbers, dots, dashes or underscores, starting with a letter or number.'
  return null
}

export const name: Check = (v) => {
  const s = v.trim()
  if (!s) return 'Name is required.'
  if (s.length > 100) return 'Name must be 100 characters or fewer.'
  return null
}

// Length only (NIST 800-63B); 72 bytes is bcrypt's input limit.
export const password: Check = (v) => {
  if (!v) return 'Password is required.'
  if (v.length < 8) return 'Password must be at least 8 characters.'
  if (new TextEncoder().encode(v).length > 72) return 'Password is too long.'
  return null
}

export const usernameOrEmail: Check = (v) => (v.includes('@') ? email(v) : username(v))
