// Run: node --experimental-strip-types shared/src/components/color-picker/color.check.ts
import { parseHex, toHex } from './color.ts'

const cases = ['#5e6ad2', '#f91880', '#00ba7c', '#000000', '#ffffff', '#808080', '#5e6ad280', '#ff000000', '#12345678']
for (const hex of cases) {
  const back = toHex(parseHex(hex)!)
  if (back !== hex) throw new Error(`${hex} came back as ${back}`)
}
if (parseHex('#fff') || parseHex('5e6ad2') || parseHex('#5e6ad2f')) throw new Error('accepted a malformed hex')
if (toHex({ h: 0, s: 1, v: 1, a: 1 }) !== '#ff0000') throw new Error('pure red')
console.log('color: ok')
