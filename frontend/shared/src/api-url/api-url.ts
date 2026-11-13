// The API's origin, from the app's .env files; "" serves the API on the app's own host. `?.` keeps it importable
// from the node checks, where import.meta.env doesn't exist.
export const API_URL: string = (import.meta.env?.VITE_API_URL ?? '').replace(/\/$/, '')

// A server path ("/api/...") as the browser must load it, on the API's host. Absolute URLs pass through.
export const apiUrl = (url: string) => (url.startsWith('/') ? API_URL + url : url)
