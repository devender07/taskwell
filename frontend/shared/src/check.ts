// Tiny assert for the *.check.ts scripts, so they type-check without Node's types.
export const assert = {
  ok(value: unknown, message = 'expected a truthy value') {
    if (!value) throw new Error(message)
  },
  equal(actual: unknown, expected: unknown) {
    if (actual !== expected) throw new Error(`expected ${String(expected)}, got ${String(actual)}`)
  },
  // ponytail: JSON comparison, so key order matters; fine for the plain objects these checks build.
  deepEqual(actual: unknown, expected: unknown) {
    const [a, e] = [JSON.stringify(actual), JSON.stringify(expected)]
    if (a !== e) throw new Error(`expected ${e}, got ${a}`)
  },
}
