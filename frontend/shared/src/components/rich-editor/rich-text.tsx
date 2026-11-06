import { Fragment, type ReactNode } from 'react'
import { imageSrc, linkHref, toggleTask, type Doc } from './doc'
import './rich-editor.css'

export type { Doc }

type Mark = NonNullable<Doc['marks']>[number]

// A saved document as the read-only editor would show it (same elements and classes), without a ProseMirror view.
// `issueHref` turns `#ENG-12` issue mentions into links; without it they are plain text.
// `onToggleTask` makes checklist items clickable: it gets the doc with that item flipped. Without it they're read-only.
export function RichText({
  doc,
  'aria-label': ariaLabel,
  issueHref,
  onToggleTask,
}: {
  doc: Doc
  'aria-label'?: string
  issueHref?: (identifier: string) => string
  onToggleTask?: (doc: Doc) => void
}) {
  const ctx: Ctx = { issueHref, task: 0, toggle: onToggleTask && ((n) => onToggleTask(toggleTask(doc, n))) }
  return (
    <div className="rich-editor rich-editor-readonly">
      <div className="rich-editor-content rich-text" role="textbox" aria-readonly="true" aria-multiline="true" aria-label={ariaLabel}>
        {blocks(doc.content, ctx)}
      </div>
    </div>
  )
}

type Ctx = { issueHref?: (identifier: string) => string; task: number; toggle?: (n: number) => void }

const blocks = (nodes: Doc[] | undefined, ctx: Ctx) => nodes?.map((n, i) => block(n, i, ctx))

function block(n: Doc, key: number, ctx: Ctx): ReactNode {
  const a = n.attrs ?? {}
  switch (n.type) {
    case 'paragraph':
      return <p key={key}>{n.content?.length ? inline(n.content, 0, ctx) : <br />}</p>
    case 'heading': {
      const H = (`h${Math.min(Math.max(Number(a.level) || 1, 1), 3)}`) as 'h1' | 'h2' | 'h3'
      return <H key={key}>{inline(n.content ?? [], 0, ctx)}</H>
    }
    case 'bulletList':
      return <ul key={key}>{blocks(n.content, ctx)}</ul>
    case 'orderedList':
      return (
        <ol key={key} start={a.start != null && a.start !== 1 ? Number(a.start) : undefined} type={/^[aAiI]$/.test(String(a.type)) ? (a.type as 'a') : undefined}>
          {blocks(n.content, ctx)}
        </ol>
      )
    case 'listItem':
      return <li key={key}>{blocks(n.content, ctx)}</li>
    case 'taskList':
      return (
        <ul key={key} data-type="taskList">
          {blocks(n.content, ctx)}
        </ul>
      )
    case 'taskItem': {
      const i = ctx.task++
      const toggle = ctx.toggle
      const checked = a.checked === true
      return (
        <li key={key} data-type="taskItem" data-checked={checked}>
          <label>
            <input type="checkbox" checked={checked} disabled={!toggle} onChange={toggle && (() => toggle(i))} />
            <span />
          </label>
          <div>{blocks(n.content, ctx)}</div>
        </li>
      )
    }
    case 'blockquote':
      return <blockquote key={key}>{blocks(n.content, ctx)}</blockquote>
    case 'codeBlock':
      return (
        <pre key={key}>
          <code className={a.language ? `language-${a.language}` : undefined}>{n.content?.map((t) => t.text ?? '').join('')}</code>
        </pre>
      )
    case 'table':
      return (
        <div key={key} className="tableWrapper">
          <table>
            <tbody>{blocks(n.content, ctx)}</tbody>
          </table>
        </div>
      )
    case 'tableRow':
      return <tr key={key}>{blocks(n.content, ctx)}</tr>
    case 'tableHeader':
    case 'tableCell': {
      const Cell = n.type === 'tableHeader' ? 'th' : 'td'
      const span = (v: unknown) => (typeof v === 'number' && v > 1 ? v : undefined)
      return (
        <Cell key={key} colSpan={span(a.colspan)} rowSpan={span(a.rowspan)}>
          {blocks(n.content, ctx)}
        </Cell>
      )
    }
    case 'horizontalRule':
      return <hr key={key} />
    case 'image':
      return <img key={key} src={imageSrc(a.src) ?? undefined} alt={String(a.alt ?? '')} style={typeof a.width === 'number' ? { width: a.width } : undefined} draggable />
    default:
      return null
  }
}

// Consecutive nodes sharing a mark share one element, as ProseMirror's serializer does: <strong>a<em>b</em></strong>.
function inline(nodes: Doc[], depth: number, ctx: Ctx): ReactNode[] {
  const out: ReactNode[] = []
  for (let i = 0; i < nodes.length; ) {
    const mark = nodes[i]!.marks?.[depth]
    if (!mark) {
      out.push(leaf(nodes[i]!, i, ctx))
      i++
      continue
    }
    let j = i + 1
    while (j < nodes.length && same(nodes[j]!.marks?.[depth], mark)) j++
    out.push(wrap(mark, inline(nodes.slice(i, j), depth + 1, ctx), i))
    i = j
  }
  return out
}

const same = (a: Mark | undefined, b: Mark) => !!a && a.type === b.type && JSON.stringify(a.attrs ?? {}) === JSON.stringify(b.attrs ?? {})

function leaf(n: Doc, key: number, ctx: Ctx): ReactNode {
  if (n.type === 'text') return n.text ?? ''
  if (n.type === 'hardBreak') return <br key={key} />
  if (n.type === 'mention') {
    const { id, label, mentionSuggestionChar } = n.attrs ?? {}
    if (mentionSuggestionChar === '#') {
      const text = `#${label ?? id}`
      return ctx.issueHref && id ? (
        <a key={key} href={ctx.issueHref(String(id))} data-type="issue-mention" data-id={id}>
          {text}
        </a>
      ) : (
        text
      )
    }
    return (
      <span key={key} data-type="mention" data-id={id || undefined} data-label={label || undefined} data-mention-suggestion-char="@">
        {`@${label ?? id}`}
      </span>
    )
  }
  return null
}

function wrap(mark: Mark, children: ReactNode[], key: number): ReactNode {
  switch (mark.type) {
    case 'bold':
      return <strong key={key}>{children}</strong>
    case 'italic':
      return <em key={key}>{children}</em>
    case 'strike':
      return <s key={key}>{children}</s>
    case 'underline':
      return <u key={key}>{children}</u>
    case 'code':
      return <code key={key}>{children}</code>
    case 'link':
      return (
        <a key={key} href={linkHref(mark.attrs?.href)} target="_blank" rel="noopener noreferrer nofollow">
          {children}
        </a>
      )
    default:
      return <Fragment key={key}>{children}</Fragment>
  }
}
