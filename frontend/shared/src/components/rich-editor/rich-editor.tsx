import { useEffect, useId, useReducer, useRef, useState } from 'react'
import { EditorContent, Extension, Node, ResizableNodeView, getSchema, useEditor, useEditorState, type ChainedCommands, type Editor } from '@tiptap/react'
import StarterKit from '@tiptap/starter-kit'
import { Placeholder } from '@tiptap/extensions'
import Mention from '@tiptap/extension-mention'
import { TaskItem, TaskList } from '@tiptap/extension-list'
import { TableKit } from '@tiptap/extension-table'
import Collaboration from '@tiptap/extension-collaboration'
import CollaborationCaret from '@tiptap/extension-collaboration-caret'
import type { Node as PMNode } from '@tiptap/pm/model'
import { TextSelection } from '@tiptap/pm/state'
import { prosemirrorJSONToYXmlFragment, yXmlFragmentToProsemirrorJSON } from '@tiptap/y-tiptap'
import type * as Y from 'yjs'
import type { Awareness } from 'y-protocols/awareness'
import { Button } from '../button/button'
import { IconButton } from '../icon-button/icon-button'
import { Input } from '../input/input'
import { Popover } from '../popover/popover'
import { useFieldError } from '../input/field-error'
import { filterOptions, type Option } from '../smart-autocomplete/match'
import { FILE_URL, filePath, imageSrc, isAllowedHref, measure, type Doc } from './doc'
import { RichText } from './rich-text'
import { MoreIcon, PathIcon } from '../icons/icons'
import '../input/input.css'
import '../smart-autocomplete/smart-autocomplete.css'
import { ProgressRing, uploadLabel } from '../progress-ring/progress-ring'
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
  // onProgress: the share sent so far (0..1), shown as a ring beside the file's name.
  upload?: (file: File, onProgress: (sent: number) => void) => Promise<{ url: string; name: string; image: boolean }>
  // Set: attached, pasted and dropped files go here instead of into the document.
  onFiles?: (files: File[]) => void
  // `#` suggests issues (shown as "ENG-12 Fix login"); the mention stores the identifier as both id and label.
  issues?: (q: string) => Promise<{ id: string; label: string; title: string }[]>
  // Set: Enter (outside lists and code blocks, with no suggestion open) calls it instead of starting a new line.
  onSubmit?: () => void
  issueHref?: (identifier: string) => string
  // Adds checklists (nested) and a Checklist toolbar button.
  tasks?: boolean
}

type Suggest = { kind: 'people' | 'issues'; items: Option[]; index: number; rect: DOMRect | null; pick: (o: Option) => void }

type Upload = { id: number; name: string; sent?: number; error?: string }

const Image = Node.create({
  name: 'image',
  group: 'block',
  atom: true,
  draggable: true,
  addAttributes: () => ({
    src: { default: null, parseHTML: (el) => filePath(el.getAttribute('src') ?? '') },
    alt: { default: '' },
    width: { default: null, parseHTML: (el) => Number(el.getAttribute('width')) || null },
  }),
  parseHTML: () => [{ tag: 'img[src]', getAttrs: (el) => FILE_URL.test(filePath(el.getAttribute('src') ?? '')) && null }],
  renderHTML: ({ HTMLAttributes }) => ['img', { ...HTMLAttributes, src: imageSrc(HTMLAttributes.src) }],
  // Dragging a bottom corner sets the width (height follows); it's kept within the editor's width.
  addNodeView:
    () =>
    ({ node, editor, getPos }) => {
      const img = document.createElement('img')
      const show = (n: PMNode) => {
        img.src = imageSrc(n.attrs.src) ?? ''
        img.alt = String(n.attrs.alt ?? '')
        img.style.width = n.attrs.width ? `${n.attrs.width}px` : ''
      }
      show(node)
      return new ResizableNodeView({
        element: img,
        node,
        editor,
        getPos,
        onResize: (width) => (img.style.width = `${width}px`),
        onCommit: (width) => {
          const pos = getPos()
          const max = img.closest('.rich-editor-content')?.clientWidth ?? width
          if (pos !== undefined) editor.view.dispatch(editor.state.tr.setNodeAttribute(pos, 'width', Math.round(Math.min(width, max))))
        },
        onUpdate: (n) => (show(n), true),
        options: { directions: ['bottom-left', 'bottom-right'], min: { width: 48 }, preserveAspectRatio: true, className: { handle: 'rich-editor-resize' } },
      })
    },
})

// Enter runs `submit` (which returns whether it handled it) unless the caret is in a list, code block or table.
const SubmitOnEnter = Extension.create<{ submit: () => boolean }>({
  name: 'submitOnEnter',
  addOptions: () => ({ submit: () => false }),
  addKeyboardShortcuts() {
    return { Enter: ({ editor }) => !editor.isActive('listItem') && !editor.isActive('taskItem') && !editor.isActive('codeBlock') && !editor.isActive('table') && this.options.submit() }
  },
})

const starterKit = (editable: boolean, collab: boolean) =>
  StarterKit.configure({
    heading: { levels: [1, 2, 3] },
    link: { openOnClick: !editable, autolink: true, isAllowedUri: isAllowedHref },
    undoRedo: collab ? false : undefined,
  })

// The node view doesn't put data-type on its <li> (only renderHTML does); the checklist CSS keys on it.
// Tables in every editor; no column resizing (widths would need a drag UI and collide in live editing).
const table = TableKit.configure({ table: { resizable: false } })

const taskList = [TaskList, TaskItem.configure({ nested: true, HTMLAttributes: { 'data-type': 'taskItem' } })]

// Builds a Y.Doc's content from a saved document, for the first client of a live session (the "seed").
// `tasks` must match the editor's, so checklists survive the seed.
export function seedYDoc(doc: Y.Doc, json: Doc, opts?: { tasks?: boolean }) {
  prosemirrorJSONToYXmlFragment(getSchema([starterKit(true, true), Mention, Image, table, ...(opts?.tasks ? taskList : [])]), json, doc.getXmlFragment('default'))
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
  onFiles,
  issues,
  onSubmit,
  tasks,
}: RichEditorProps) {
  const [error, onEdit] = useFieldError(rawError)
  const id = useId()
  const errorId = `${id}-error`
  const [uploads, setUploads] = useState<Upload[]>([])
  const uploadSeq = useRef(0)
  const latest = useRef({ onChange, onEdit, mentions, upload, onFiles, attach, issues, onSubmit })
  latest.current = { onChange, onEdit, mentions, upload, onFiles, attach, issues, onSubmit }
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
        table,
        ...(tasks ? taskList : []),
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
          if (!files.length || !(latest.current.upload || latest.current.onFiles)) return false
          latest.current.attach(files)
          return true
        },
        handleDrop: (view, event, _slice, moved) => {
          const files = Array.from(event.dataTransfer?.files ?? [])
          if (moved || !files.length || !(latest.current.upload || latest.current.onFiles)) return false
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
    if (onFiles) return onFiles(files)
    if (!upload || !editor) return
    if (at != null) editor.commands.setTextSelection(at)
    setUploads((list) => list.filter((u) => !u.error))
    for (const file of files) {
      const id = ++uploadSeq.current
      setUploads((list) => [...list, { id, name: file.name }])
      const progress = (sent: number) => setUploads((list) => list.map((u) => (u.id === id ? { ...u, sent } : u)))
      upload(file, progress).then(
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
    <div className="grid gap-2" style={{ gridTemplateColumns: 'minmax(0, 1fr)' }}>
      {label ? (
        <span id={`${id}-label`} className="text-label text-secondary" onClick={() => editor?.commands.focus()}>
          {label}
        </span>
      ) : null}
      <div className={editable ? 'input rich-editor' : 'rich-editor rich-editor-readonly'} aria-invalid={error ? true : undefined}>
        {editable && editor ? <Toolbar editor={editor} attach={upload || onFiles ? attach : undefined} tasks={tasks} /> : null}
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
      {uploads.map((u) =>
        u.error ? (
          <span key={u.id} role="alert" className="text-caption text-accent">
            Couldn't upload {u.name}: {u.error}
          </span>
        ) : (
          <span key={u.id} className="flex items-center gap-2 text-caption text-secondary">
            <ProgressRing value={u.sent} label={`Uploading ${u.name}`} />
            <span className="ellipsis">{uploadLabel(u.name, u.sent)}</span>
          </span>
        ),
      )}
      {error ? <span id={errorId} className="text-caption text-accent">{error}</span> : null}
    </div>
  )
}

// The list item each list kind holds.
const LISTS = { bulletList: 'listItem', orderedList: 'listItem', taskList: 'taskItem' } as const

// A list button, like a word processor's: in a list of its kind it unwraps the items; in another kind of list it
// converts that list (and its items) in place; anywhere else it wraps the selected blocks. The selection is first
// narrowed to the text it covers, without empty blocks at its end: Ctrl+A or a drag past the last line takes in the
// trailing empty paragraph, which would otherwise become a new item each click.
const toggleList = (name: keyof typeof LISTS) => (c: ChainedCommands) =>
  c.command(({ state, tr, dispatch, commands }) => {
    const $start = TextSelection.near(tr.doc.resolve(tr.selection.from), 1).$from
    let $end = TextSelection.near(tr.doc.resolve(tr.selection.to), -1).$to
    while ($end.parent.content.size === 0 && $end.depth > 0 && $end.before() > $start.pos) {
      const prev = TextSelection.near(tr.doc.resolve($end.before()), -1).$to
      if (prev.pos >= $end.pos) break
      $end = prev
    }
    tr.setSelection(TextSelection.between($start, $end))
    const $from = tr.selection.$from
    for (let d = $from.depth; d > 0; d--) {
      const list = $from.node(d)
      if (!(list.type.name in LISTS)) continue
      if (list.type.name === name) {
        const end = $from.after(d) - 1
        if (tr.selection.to > end) tr.setSelection(TextSelection.between($from, TextSelection.near(tr.doc.resolve(end), -1).$head))
        return commands.liftListItem(LISTS[name])
      }
      if (!dispatch) return true
      const item = state.schema.nodes[LISTS[name]]!
      const items: PMNode[] = []
      list.forEach((child) => items.push(child.type === item ? child : item.create(item.name === 'taskItem' ? { checked: false } : null, child.content)))
      const pos = $from.before(d)
      const mapped = tr.selection
      tr.replaceWith(pos, pos + list.nodeSize, state.schema.nodes[name]!.create(null, items))
      tr.setSelection(TextSelection.between(tr.doc.resolve(Math.min(mapped.from, tr.doc.content.size)), tr.doc.resolve(Math.min(mapped.to, tr.doc.content.size))))
      return true
    }
    return name === 'bulletList' ? commands.toggleBulletList() : name === 'orderedList' ? commands.toggleOrderedList() : commands.toggleTaskList()
  })

const TOOLS: { name: string; label: string; d: string; run: (c: ChainedCommands) => ChainedCommands }[] = [
  { name: 'heading', label: 'Heading', d: 'M6 12h12M6 20V4M18 20V4', run: (c) => c.toggleHeading({ level: 1 }) },
  { name: 'bold', label: 'Bold', d: 'M6 12h9a4 4 0 0 1 0 8H6V4h8a4 4 0 0 1 0 8', run: (c) => c.toggleBold() },
  { name: 'italic', label: 'Italic', d: 'M19 4h-9M14 20H5M15 4 9 20', run: (c) => c.toggleItalic() },
  { name: 'strike', label: 'Strikethrough', d: 'M16 4H9a3 3 0 0 0-2.83 4M14 12a4 4 0 0 1 0 8H6M4 12h16', run: (c) => c.toggleStrike() },
  { name: 'code', label: 'Code', d: 'm16 18 6-6-6-6M8 6l-6 6 6 6', run: (c) => c.toggleCode() },
  { name: 'bulletList', label: 'Bullet list', d: 'M3 6h.01M3 12h.01M3 18h.01M8 6h13M8 12h13M8 18h13', run: toggleList('bulletList') },
  { name: 'orderedList', label: 'Numbered list', d: 'M10 6h11M10 12h11M10 18h11M4 6h1v4M4 10h2M6 18H4c0-1 2-2 2-3s-1-1.5-2-1', run: toggleList('orderedList') },
  { name: 'blockquote', label: 'Quote', d: 'M17 6H3M21 12H8M21 18H8M3 12v6', run: (c) => c.toggleBlockquote() },
  {
    name: 'codeBlock',
    label: 'Code block',
    d: 'M10 9l-3 3 3 3M14 15l3-3-3-3M5 3h14a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2z',
    run: (c) => c.toggleCodeBlock(),
  },
]

const CHECKLIST: (typeof TOOLS)[number] = {
  name: 'taskList',
  label: 'Checklist',
  d: 'M3 5.5 4.5 7 7 4.5M3 12.5 4.5 14 7 11.5M3 19.5 4.5 21 7 18.5M11 6h10M11 13h10M11 20h10',
  run: toggleList('taskList'),
}

const PAPERCLIP = 'm21.44 11.05-9.19 9.19a6 6 0 0 1-8.49-8.49l8.57-8.57A4 4 0 1 1 18 8.84l-8.59 8.57a2 2 0 0 1-2.83-2.83l8.49-8.48'

const TABLE = 'M3 3h18v18H3zM3 9h18M3 15h18M9 3v18M15 3v18'
const LINK = 'M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71'
const TABLE_TOOLS: { label: string; d: string; run: (c: ChainedCommands) => ChainedCommands }[] = [
  { label: 'Add row', d: 'M3 3h18v10H3zM3 8h18M12 16v6M9 19h6', run: (c) => c.addRowAfter() },
  { label: 'Add column', d: 'M3 3h10v18H3zM8 3v18M19 9v6M16 12h6', run: (c) => c.addColumnAfter() },
  { label: 'Delete row', d: 'M3 3h18v10H3zM3 8h18M9 19h6', run: (c) => c.deleteRow() },
  { label: 'Delete column', d: 'M3 3h10v18H3zM8 3v18M16 12h6', run: (c) => c.deleteColumn() },
  { label: 'Delete table', d: 'M3 6h18M8 6V4h8v2M19 6l-1 14H6L5 6M10 11v6M14 11v6', run: (c) => c.deleteTable() },
]
const TOOL_WIDTH = 44

type Item = { label: string; d: string; active?: boolean; select: () => void }

// One row of tools; those that don't fit go in a "More formatting" menu at its end. Inside a table, its row and
// column tools take the Table button's place. Link opens an address field under the row.
function Toolbar({ editor, attach, tasks }: { editor: Editor; attach?: (files: File[]) => void; tasks?: boolean }) {
  const tools = tasks ? [...TOOLS, CHECKLIST] : TOOLS
  const state = useEditorState({
    editor,
    selector: ({ editor }) => ({ active: tools.map((t) => editor.isActive(t.name)), link: editor.isActive('link'), table: editor.isActive('table') }),
  })
  const picker = useRef<HTMLInputElement>(null)
  const row = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(Infinity)
  const [linking, setLinking] = useState(false)

  useEffect(() => {
    const el = row.current
    if (!el) return
    const measure = () => {
      const cs = getComputedStyle(el)
      setWidth(el.clientWidth - parseFloat(cs.paddingLeft) - parseFloat(cs.paddingRight))
    }
    measure()
    const ro = new ResizeObserver(measure)
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  const run = (fn: (c: ChainedCommands) => ChainedCommands) => () => fn(editor.chain().focus()).run()
  const items: Item[] = [
    ...tools.map((t, i) => ({ label: t.label, d: t.d, active: state.active[i], select: run(t.run) })),
    { label: 'Link', d: LINK, active: state.link, select: () => setLinking(true) },
    ...(state.table
      ? TABLE_TOOLS.map((t) => ({ label: t.label, d: t.d, select: run(t.run) }))
      : [{ label: 'Table', d: TABLE, select: run((c) => c.insertTable({ rows: 3, cols: 3, withHeaderRow: true })) }]),
    ...(attach ? [{ label: 'Attach file', d: PAPERCLIP, select: () => picker.current?.click() }] : []),
  ]
  const fit = Math.max(1, Math.floor(width / TOOL_WIDTH))
  const shown = items.length > fit ? items.slice(0, fit - 1) : items
  const more = items.slice(shown.length)

  return (
    <div className="rich-editor-toolbar">
      <div ref={row} role="toolbar" aria-label="Formatting" className="flex" style={{ minWidth: 0 }}>
        {shown.map((t) => (
          <IconButton
            key={t.label}
            className="rich-editor-tool"
            aria-label={t.label}
            tooltip={t.label}
            aria-pressed={t.active}
            onMouseDown={(e) => e.preventDefault()}
            onClick={t.select}
          >
            <PathIcon d={t.d} />
          </IconButton>
        ))}
        {more.length ? (
          <Popover
            aria-label="More formatting"
            placement="bottom-end"
            trigger={(props) => (
              <IconButton {...props} className="rich-editor-tool" aria-label="More formatting" tooltip="More formatting" onMouseDown={(e) => e.preventDefault()}>
                <MoreIcon />
              </IconButton>
            )}
          >
            {(close) => (
              <div className="grid" style={{ minWidth: 200 }}>
                {more.map((t) => (
                  <button
                    key={t.label}
                    type="button"
                    className="popover-item flex items-center gap-3"
                    aria-pressed={t.active}
                    onMouseDown={(e) => e.preventDefault()}
                    onClick={() => (close(), t.select())}
                  >
                    <PathIcon d={t.d} />
                    {t.label}
                  </button>
                ))}
              </div>
            )}
          </Popover>
        ) : null}
        {attach ? (
          <input
            ref={picker}
            type="file"
            multiple
            hidden
            accept="image/png,image/jpeg,image/gif,image/webp,video/mp4,video/webm"
            onChange={(e) => {
              attach(Array.from(e.currentTarget.files ?? []))
              e.currentTarget.value = ''
            }}
          />
        ) : null}
      </div>
      {linking ? <LinkField editor={editor} active={state.link} onDone={() => setLinking(false)} /> : null}
    </div>
  )
}

// The link's address under the toolbar: Save sets it on the selection (the whole link when the caret is in one), or
// inserts the address as linked text when nothing is selected; a bare domain gets https://. Esc cancels.
function LinkField({ editor, active, onDone }: { editor: Editor; active: boolean; onDone: () => void }) {
  const [error, setError] = useState<string | null>(null)
  const done = () => (onDone(), editor.commands.focus())
  return (
    <form
      noValidate
      className="rich-editor-link flex flex-wrap items-start gap-2"
      onKeyDown={(e) => e.key === 'Escape' && (e.preventDefault(), e.stopPropagation(), done())}
      onSubmit={(e) => {
        e.preventDefault()
        e.stopPropagation() // the editor may sit in a form of its own
        const raw = String(new FormData(e.currentTarget).get('href') ?? '').trim()
        const href = raw && !/^[a-z][a-z0-9+.-]*:/i.test(raw) ? `https://${raw}` : raw
        if (!isAllowedHref(href)) return setError('Enter a web address, like https://example.com.')
        const c = editor.chain().focus()
        if (editor.state.selection.empty && !active) c.insertContent({ type: 'text', text: raw, marks: [{ type: 'link', attrs: { href } }] }).run()
        else c.extendMarkRange('link').setLink({ href }).run()
        onDone()
      }}
    >
      <div className="flex-grow" style={{ minWidth: 180 }}>
        <Input aria-label="Link address" name="href" placeholder="https://example.com" defaultValue={String(editor.getAttributes('link').href ?? '')} autoFocus error={error} />
      </div>
      <Button type="submit">Save link</Button>
      {active ? (
        <Button type="button" variant="ghost" onClick={() => (editor.chain().focus().extendMarkRange('link').unsetLink().run(), onDone())}>
          Remove link
        </Button>
      ) : null}
      <Button type="button" variant="ghost" onClick={done}>
        Cancel
      </Button>
    </form>
  )
}

// Below the caret, or above it when there's less room below than the list's 240px max and more above (the chat composer).
function suggestionPlace(r: DOMRect) {
  const below = innerHeight - r.bottom - 8
  return below < 240 && r.top - 8 > below ? { bottom: innerHeight - r.top + 4 } : { top: r.bottom + 4 }
}
