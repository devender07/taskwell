import { Suspense, lazy, type ComponentProps } from 'react'
import type { RichEditor as Editor } from '@tether/shared/rich-editor'

type Props = ComponentProps<typeof Editor>

const LazyEditor = lazy(() => import('@tether/shared/rich-editor').then((m) => ({ default: m.RichEditor })))

// The editor (ProseMirror and Tiptap) loads on first render. Until then a box of its size, which takes no input, holds
// its place.
export function RichEditor(props: Props) {
  return (
    <Suspense fallback={<Placeholder {...props} />}>
      <LazyEditor {...props} />
    </Suspense>
  )
}

function Placeholder({ label, placeholder, minHeight = 96 }: Props) {
  return (
    <div className="grid gap-2" aria-busy="true">
      {label ? <span className="text-label text-secondary">{label}</span> : null}
      <div className="input rich-editor">
        <div className="rich-editor-toolbar" style={{ height: 45 }} />
        <p className="rich-editor-content text-secondary m-0" style={{ minHeight }}>
          {placeholder}
        </p>
      </div>
    </div>
  )
}
