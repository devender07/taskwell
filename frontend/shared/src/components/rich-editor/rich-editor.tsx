import { useId, useReducer, useRef, useState } from 'react'
import { EditorContent, Extension, Node, getSchema, useEditor, useEditorState, type ChainedCommands, type Editor } from '@tiptap/react'
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
import { FILE_URL, imageSrc, isAllowedHref, measure, type Doc } from './doc'
import { RichText } from './rich-text'
import { PathIcon } from '../icons/icons'
import '../input/input.css'
import '../smart-autocomplete/smart-autocomplete.css'
import './rich-editor.css'

export type { Doc }
export { RichText }

type RichEditorProps = {
  label?: string
  'aria-label'?: string
  content?: Doc | null
  onChange?: (doc: Doc | null, textLength: number) => void
  editable?: boolean
  placeholder?: string
  // `@` suggests these people, or the first 8 this search returns for what's typed.
  mentions?: { id: string; name: string }[] | ((q: string) => Promise<{ id: string; name: string }[]>)
  error?: string | null
  collab?: { doc: Y.Doc; awareness: Awareness; user: { name: string; color: string } }
  minHeight?: number
  upload?: (file: File) => Promise<{ url: string; name: string; image: boolean }>
  // `#` suggests issues (shown as "ENG-12 Fix login"); the mention stores the identifier as both id and label.
  issues?: (q: string) => Promise<{ id: string; label: string; title: string }[]>
  // Set: Enter (outside lists and code blocks, with no suggestion open) calls it instead of starting a new line.
  onSubmit?: () => void
  issueHref?: (identifier: string) => string
}

type Suggest = { kind: 'people' | 'issues'; items: Option[]; index: number; rect: DOMRect | null; pick: (o: Option) => void }

type Upload = { id: number; name: string; error?: string }

const Image = Node.create({
  name: 'image',
  group: 'block',
  atom: true,
  draggable: true,
  addAttributes: () => ({ src: { default: null }, alt: { default: '' } }),
  parseHTML: () => [{ tag: 'img[src]', getAttrs: (el) => FILE_URL.test(el.getAttribute('src') ?? '') && null }],
  renderHTML: ({ HTMLAttributes }) => ['img', { ...HTMLAttributes, src: imageSrc(HTMLAttributes.src) }],
})

// Enter runs `submit` (which returns whether it handled it) unless the caret is in a list or code block.
const SubmitOnEnter = Extension.create<{ submit: () => boolean }>({
  name: 'submitOnEnter',
  addOptions: () => ({ submit: () => false }),
  addKeyboardShortcuts() {
    return { Enter: ({ editor }) => !editor.isActive('listItem') && !editor.isActive('codeBlock') && this.options.submit() }
  },
})

const starterKit = (editable: boolean, collab: boolean) =>
  StarterKit.configure({
    heading: { levels: [1, 2, 3] },
    link: { openOnClick: !editable, autolink: true, isAllowedUri: isAllowedHref },
    undoRedo: collab ? false : undefined,
  })

// Builds a Y.Doc's content from a saved document, for the first client of a live session (the "seed").
export function seedYDoc(doc: Y.Doc, json: Doc) {
  prosemirrorJSONToYXmlFragment(getSchema([starterKit(true, true), Mention, Image]), json, doc.getXmlFragment('default'))
}

// The live document as editor.getJSON() would return it, read straight from the Y.Doc.
export const yDocToJSON = (doc: Y.Doc): Doc => yXmlFragmentToProsemirrorJSON(doc.getXmlFragment('default'))

// Read-only and not live: rendered straight from the JSON, so a page of comments doesn't mount an editor each.
export function RichEditor(props: RichEditorProps) {
  if (props.editable === false && !props.collab && props.content)
    return <RichText doc={props.content} aria-label={props['aria-label']} issueHref={props.issueHref} />
  return <Editable {...props} />
}

function Editable({
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
  upload,
  issues,
  onSubmit,
}: RichEditorProps) {
  const [error, onEdit] = useFieldError(rawError)
  const id = useId()
  const errorId = `${id}-error`
  const [uploads, setUploads] = useState<Upload[]>([])
  const uploadSeq = useRef(0)
  const latest = useRef({ onChange, onEdit, mentions, upload, attach, issues, onSubmit })
  latest.current = { onChange, onEdit, mentions, upload, attach, issues, onSubmit }
  const suggest = useRef<Suggest | null>(null)
  const [, rerender] = useReducer((n: number) => n + 1, 0)
  const show = (s: Suggest | null) => {
    suggest.current = s
    rerender()
  }

  const suggestion = (kind: Suggest['kind']) => {
    // `@` allows spaces ("@Dummy 100"), so once a query with a space matches no one it's just prose: hide the list.
    const open = (p: { query: string; items: Option[]; clientRect?: (() => DOMRect | null) | null; command: (a: { id: string; label: string }) => void }) =>
      show(
        !p.items.length && p.query.includes(' ')
          ? null
          : { kind, items: p.items, index: 0, rect: p.clientRect?.() ?? null, pick: (o) => p.command({ id: o.value, label: o.hint ?? o.label }) },
      )
    return {
      onStart: open,
      onUpdate: open,
      onExit: () => show(null),
      onKeyDown: ({ event }: { event: KeyboardEvent }) => {
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
  }

  const editor = useEditor(
    {
      editable,
      content: collab ? undefined : (content ?? ''),
      extensions: [
        starterKit(editable, !!collab),
        Image,
        Placeholder.configure({ placeholder: placeholder ?? '' }),
        Mention.configure({
          suggestions: [
            {
              char: '@',
              allowSpaces: true,
              allow: () => typeof latest.current.mentions === 'function' || !!latest.current.mentions?.length,
              items: async ({ query }) => {
                const m = latest.current.mentions
                if (typeof m === 'function') return ((await m(query).catch(() => [])) ?? []).slice(0, 8).map((p) => ({ value: p.id, label: p.name }))
                return filterOptions((m ?? []).map((p) => ({ value: p.id, label: p.name })), query, 8)
              },
              render: () => suggestion('people'),
            },
            {
              char: '#',
              allow: () => !!latest.current.issues,
              // ponytail: one search per keystroke, no debounce; Tiptap shows whichever answer lands last.
              items: async ({ query }) =>
                ((await latest.current.issues?.(query).catch(() => [])) ?? []).slice(0, 8).map((i) => ({ value: i.id, label: `${i.label} ${i.title}`, hint: i.label })),
              render: () => suggestion('issues'),
            },
          ],
        }),
        SubmitOnEnter.configure({ submit: () => (suggest.current || !latest.current.onSubmit ? false : (latest.current.onSubmit(), true)) }),
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
        handlePaste: (_view, event) => {
          const files = Array.from(event.clipboardData?.files ?? [])
          if (!files.length || !latest.current.upload) return false
          latest.current.attach(files)
          return true
        },
        handleDrop: (view, event, _slice, moved) => {
          const files = Array.from(event.dataTransfer?.files ?? [])
          if (moved || !files.length || !latest.current.upload) return false
          event.preventDefault()
          latest.current.attach(files, view.posAtCoords({ left: event.clientX, top: event.clientY })?.pos)
          return true
        },
      },
      onCreate: ({ editor }) => queueMicrotask(() => report(editor, false)),
      onUpdate: ({ editor }) => queueMicrotask(() => report(editor, true)),
    },
    [collab?.doc, editable],
  )

  function attach(files: File[], at?: number) {
    if (!upload || !editor) return
    if (at != null) editor.commands.setTextSelection(at)
    setUploads((list) => list.filter((u) => !u.error))
    for (const file of files) {
      const id = ++uploadSeq.current
      setUploads((list) => [...list, { id, name: file.name }])
      upload(file).then(
        (f) => {
          setUploads((list) => list.filter((u) => u.id !== id))
          if (editor.isDestroyed) return
          editor.commands.insertContent(
            f.image
              ? [{ type: 'image', attrs: { src: f.url, alt: f.name } }, { type: 'paragraph' }]
              : [{ type: 'text', text: f.name, marks: [{ type: 'link', attrs: { href: f.url } }] }, { type: 'text', text: ' ' }],
          )
        },
        (e: unknown) => {
          const error = e instanceof Error ? e.message : 'Something went wrong. Try again.'
          setUploads((list) => list.map((u) => (u.id === id ? { ...u, error } : u)))
        },
      )
    }
  }

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
        {editable && editor ? <Toolbar editor={editor} attach={upload ? attach : undefined} /> : null}
        <EditorContent editor={editor} />
        {s && s.rect ? (
          <div className="autocomplete-panel rich-editor-mentions" style={{ position: 'fixed', ...suggestionPlace(s.rect), left: s.rect.left, right: 'auto' }}>
            {s.items.length ? (
              <ul className="autocomplete-list" role="listbox" aria-label={s.kind === 'people' ? 'People' : 'Issues'}>
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
              <p className="autocomplete-note">{s.kind === 'people' ? 'No people found' : 'No issues found'}</p>
            )}
          </div>
        ) : null}
      </div>
      {uploads.map((u) => (
        <span key={u.id} role={u.error ? 'alert' : 'status'} className={`text-caption ${u.error ? 'text-accent' : 'text-secondary'}`}>
          {u.error ? `Couldn't upload ${u.name}: ${u.error}` : `Uploading ${u.name}…`}
        </span>
      ))}
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

const PAPERCLIP = 'm21.44 11.05-9.19 9.19a6 6 0 0 1-8.49-8.49l8.57-8.57A4 4 0 1 1 18 8.84l-8.59 8.57a2 2 0 0 1-2.83-2.83l8.49-8.48'

function Toolbar({ editor, attach }: { editor: Editor; attach?: (files: File[]) => void }) {
  const active = useEditorState({ editor, selector: ({ editor }) => TOOLS.map((t) => editor.isActive(t.name)) })
  const picker = useRef<HTMLInputElement>(null)
  return (
    <div role="toolbar" aria-label="Formatting" className="rich-editor-toolbar flex flex-wrap">
      {TOOLS.map((t, i) => (
        <IconButton
          key={t.name}
          className="rich-editor-tool"
          aria-label={t.label}
          tooltip={t.label}
          aria-pressed={active[i]}
          onMouseDown={(e) => e.preventDefault()}
          onClick={() => t.run(editor.chain().focus()).run()}
        >
          <PathIcon d={t.d} />
        </IconButton>
      ))}
      {attach ? (
        <>
          <IconButton
            className="rich-editor-tool"
            aria-label="Attach file"
            tooltip="Attach file"
            onMouseDown={(e) => e.preventDefault()}
            onClick={() => picker.current?.click()}
          >
            <PathIcon d={PAPERCLIP} />
          </IconButton>
          <input
            ref={picker}
            type="file"
            multiple
            hidden
            onChange={(e) => {
              attach(Array.from(e.currentTarget.files ?? []))
              e.currentTarget.value = ''
            }}
          />
        </>
      ) : null}
    </div>
  )
}

// Below the caret, or above it when there's less room below than the list's 240px max and more above (the chat composer).
function suggestionPlace(r: DOMRect) {
  const below = innerHeight - r.bottom - 8
  return below < 240 && r.top - 8 > below ? { bottom: innerHeight - r.top + 4 } : { top: r.bottom + 4 }
}
