import { useId, useReducer, useRef } from 'react'
import { EditorContent, getSchema, useEditor, useEditorState, type ChainedCommands, type Editor, type JSONContent } from '@tiptap/react'
import StarterKit from '@tiptap/starter-kit'
import { Placeholder } from '@tiptap/extensions'
import Mention from '@tiptap/extension-mention'
import Collaboration from '@tiptap/extension-collaboration'
import CollaborationCaret from '@tiptap/extension-collaboration-caret'
import { prosemirrorJSONToYXmlFragment, yXmlFragmentToProsemirrorJSON } from '@tiptap/y-tiptap'
import type * as Y from 'yjs'
import type { Awareness } from 'y-protocols/awareness'
import { IconButton } from '../icon-button/icon-button'
import { useFieldError } from '../input/field-error'
import { filterOptions, type Option } from '../smart-autocomplete/match'
import { measure } from './doc'
import '../input/input.css'
import '../smart-autocomplete/smart-autocomplete.css'
import './rich-editor.css'

export type Doc = JSONContent

type RichEditorProps = {
  label?: string
  'aria-label'?: string
  content?: Doc | null
  onChange?: (doc: Doc | null, textLength: number) => void
  editable?: boolean
  placeholder?: string
  mentions?: { id: string; name: string }[]
  error?: string | null
  collab?: { doc: Y.Doc; awareness: Awareness; user: { name: string; color: string } }
  minHeight?: number
}

type Suggest = { items: Option[]; index: number; rect: DOMRect | null; pick: (o: Option) => void }

const SAFE_HREF = /^(https?:\/\/|mailto:)/i
const starterKit = (editable: boolean, collab: boolean) =>
  StarterKit.configure({
    heading: { levels: [1, 2, 3] },
    link: { openOnClick: !editable, autolink: true, isAllowedUri: (url) => SAFE_HREF.test(url) },
    undoRedo: collab ? false : undefined,
  })

// Builds a Y.Doc's content from a saved document, for the first client of a live session (the "seed").
export function seedYDoc(doc: Y.Doc, json: Doc) {
  prosemirrorJSONToYXmlFragment(getSchema([starterKit(true, true), Mention]), json, doc.getXmlFragment('default'))
}

// The live document as editor.getJSON() would return it, read straight from the Y.Doc.
export const yDocToJSON = (doc: Y.Doc): Doc => yXmlFragmentToProsemirrorJSON(doc.getXmlFragment('default'))

export function RichEditor({
  label,
  'aria-label': ariaLabel,
  content,
  onChange,
  editable = true,
  placeholder,
  mentions,
  error: rawError,
  collab,
  minHeight = 96,
}: RichEditorProps) {
  const [error, onEdit] = useFieldError(rawError)
  const id = useId()
  const errorId = `${id}-error`
  const latest = useRef({ onChange, onEdit, mentions })
  latest.current = { onChange, onEdit, mentions }
  const suggest = useRef<Suggest | null>(null)
  const [, rerender] = useReducer((n: number) => n + 1, 0)
  const show = (s: Suggest | null) => {
    suggest.current = s
    rerender()
  }

  const editor = useEditor(
    {
      editable,
      content: collab ? undefined : (content ?? ''),
      extensions: [
        starterKit(editable, !!collab),
        Placeholder.configure({ placeholder: placeholder ?? '' }),
        Mention.configure({
          suggestion: {
            char: '@',
            allow: () => !!latest.current.mentions?.length,
            items: ({ query }) =>
              filterOptions(
                (latest.current.mentions ?? []).map((m) => ({ value: m.id, label: m.name })),
                query,
                8,
              ),
            render: () => {
              const open = (p: { items: Option[]; clientRect?: (() => DOMRect | null) | null; command: (a: { id: string; label: string }) => void }) =>
                show({ items: p.items, index: 0, rect: p.clientRect?.() ?? null, pick: (o) => p.command({ id: o.value, label: o.label }) })
              return {
                onStart: open,
                onUpdate: open,
                onExit: () => show(null),
                onKeyDown: ({ event }) => {
                  const s = suggest.current
                  if (!s || !s.items.length) return false
                  const n = s.items.length
                  if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
                    show({ ...s, index: (s.index + (event.key === 'ArrowDown' ? 1 : n - 1)) % n })
                    return true
                  }
                  if (event.key === 'Enter' || event.key === 'Tab') {
                    s.pick(s.items[s.index]!)
                    return true
                  }
                  return false
                },
              }
            },
          },
        }),
        ...(collab
          ? [Collaboration.configure({ document: collab.doc }), CollaborationCaret.configure({ provider: { awareness: collab.awareness }, user: collab.user })]
          : []),
      ],
      editorProps: {
        attributes: {
          class: 'rich-editor-content',
          role: 'textbox',
          'aria-multiline': 'true',
          ...(label ? { 'aria-labelledby': `${id}-label` } : ariaLabel ? { 'aria-label': ariaLabel } : {}),
          'aria-describedby': errorId,
          ...(editable ? { style: `min-height: ${minHeight}px` } : {}),
        },
      },
      onCreate: ({ editor }) => queueMicrotask(() => report(editor, false)),
      onUpdate: ({ editor }) => queueMicrotask(() => report(editor, true)),
    },
    [collab?.doc, editable],
  )

  function report(editor: Editor, edited: boolean) {
    const doc = editor.getJSON()
    const { length, empty } = measure(doc)
    if (edited) latest.current.onEdit()
    latest.current.onChange?.(empty ? null : doc, length)
  }

  const s = suggest.current
  return (
    <div className="grid gap-2">
      {label ? (
        <span id={`${id}-label`} className="text-label text-secondary" onClick={() => editor?.commands.focus()}>
          {label}
        </span>
      ) : null}
      <div className={editable ? 'input rich-editor' : 'rich-editor rich-editor-readonly'} aria-invalid={error ? true : undefined}>
        {editable && editor ? <Toolbar editor={editor} /> : null}
        <EditorContent editor={editor} />
        {s && s.rect ? (
          <div className="autocomplete-panel rich-editor-mentions" style={{ position: 'fixed', top: s.rect.bottom + 4, left: s.rect.left, right: 'auto' }}>
            {s.items.length ? (
              <ul className="autocomplete-list" role="listbox" aria-label="People">
                {s.items.map((o, i) => (
                  <li
                    key={o.value}
                    role="option"
                    aria-selected={i === s.index}
                    data-active={i === s.index ? '' : undefined}
                    className="autocomplete-option"
                    onMouseDown={(e) => e.preventDefault()}
                    onClick={() => s.pick(o)}
                  >
                    {o.label}
                  </li>
                ))}
              </ul>
            ) : (
              <p className="autocomplete-note">No people found</p>
            )}
          </div>
        ) : null}
      </div>
      {error ? <span id={errorId} className="text-caption text-accent">{error}</span> : null}
    </div>
  )
}

const TOOLS: { name: string; label: string; d: string; run: (c: ChainedCommands) => ChainedCommands }[] = [
  { name: 'heading', label: 'Heading', d: 'M6 12h12M6 20V4M18 20V4', run: (c) => c.toggleHeading({ level: 1 }) },
  { name: 'bold', label: 'Bold', d: 'M6 12h9a4 4 0 0 1 0 8H6V4h8a4 4 0 0 1 0 8', run: (c) => c.toggleBold() },
  { name: 'italic', label: 'Italic', d: 'M19 4h-9M14 20H5M15 4 9 20', run: (c) => c.toggleItalic() },
  { name: 'strike', label: 'Strikethrough', d: 'M16 4H9a3 3 0 0 0-2.83 4M14 12a4 4 0 0 1 0 8H6M4 12h16', run: (c) => c.toggleStrike() },
  { name: 'code', label: 'Code', d: 'm16 18 6-6-6-6M8 6l-6 6 6 6', run: (c) => c.toggleCode() },
  { name: 'bulletList', label: 'Bullet list', d: 'M3 6h.01M3 12h.01M3 18h.01M8 6h13M8 12h13M8 18h13', run: (c) => c.toggleBulletList() },
  { name: 'orderedList', label: 'Numbered list', d: 'M10 6h11M10 12h11M10 18h11M4 6h1v4M4 10h2M6 18H4c0-1 2-2 2-3s-1-1.5-2-1', run: (c) => c.toggleOrderedList() },
  { name: 'blockquote', label: 'Quote', d: 'M17 6H3M21 12H8M21 18H8M3 12v6', run: (c) => c.toggleBlockquote() },
  {
    name: 'codeBlock',
    label: 'Code block',
    d: 'M10 9l-3 3 3 3M14 15l3-3-3-3M5 3h14a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2z',
    run: (c) => c.toggleCodeBlock(),
  },
]

function Toolbar({ editor }: { editor: Editor }) {
  const active = useEditorState({ editor, selector: ({ editor }) => TOOLS.map((t) => editor.isActive(t.name)) })
  return (
    <div role="toolbar" aria-label="Formatting" className="rich-editor-toolbar flex flex-wrap">
      {TOOLS.map((t, i) => (
        <IconButton
          key={t.name}
          className="rich-editor-tool"
          aria-label={t.label}
          title={t.label}
          aria-pressed={active[i]}
          onMouseDown={(e) => e.preventDefault()}
          onClick={() => t.run(editor.chain().focus()).run()}
        >
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
            <path d={t.d} />
          </svg>
        </IconButton>
      ))}
    </div>
  )
}
