// Lookups on lists of records with an id (API rows): the one place for them instead of a find or map per file.

type WithId = { id: string }

// The item with the id; undefined when there's none (or no list or id).
export const byId = <T extends WithId>(list: readonly T[] | null | undefined, id: string | null | undefined): T | undefined =>
  id == null ? undefined : list?.find((x) => x.id === id)

// The items' ids, for "is it already here?" checks while merging pages.
export const idSet = (list: readonly WithId[] | null | undefined): Set<string> => new Set(list?.map((x) => x.id))

type Path = string | readonly (string | number)[]

// True for undefined and null (and the text "undefined"), the values a response leaves out.
export const isUndefined = (value: unknown): value is null | undefined => value == null || value === 'undefined'

// True for isUndefined values, blank text, and empty arrays and objects.
export const isEmpty = (value: unknown): boolean =>
  isUndefined(value) ||
  (typeof value === 'string' && value.trim() === '') ||
  (Array.isArray(value) && value.length === 0) ||
  (typeof value === 'object' && !Array.isArray(value) && Object.keys(value as object).length === 0)

// The value at path in data ("a.b.0.c" or ['a', 'b', 0, 'c']), or fallback ('' unless given) when anything on the way
// is missing, so a response without the field renders as empty instead of breaking the page. When fallback is an
// array, a value that isn't one gives fallback too, so .map and .find are always safe:
//   extractDataFromArray(run, 'results', []).find(...)
//   extractDataFromArray(issue, 'assignee.name', 'Unassigned')
export function extractDataFromArray<T, K extends keyof T>(data: T | null | undefined, path: K, fallback: NonNullable<T[K]>): NonNullable<T[K]>
export function extractDataFromArray<T, K extends keyof T, F>(data: T | null | undefined, path: K, fallback: F): NonNullable<T[K]> | F
export function extractDataFromArray<D = string>(data: unknown, path: Path, fallback?: D): D
export function extractDataFromArray(data: unknown, path: Path | PropertyKey, fallback: unknown = ''): unknown {
  if (isUndefined(data)) return fallback
  const keys = typeof path === 'string' ? path.split('.') : Array.isArray(path) ? path : [path as PropertyKey]
  let value: unknown = data
  for (const key of keys) {
    if (isUndefined(value)) return fallback
    value = (value as Record<PropertyKey, unknown>)[key as PropertyKey]
  }
  if (isUndefined(value)) return fallback
  return Array.isArray(fallback) && !Array.isArray(value) ? fallback : value
}
