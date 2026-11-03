import type { Feedback } from '@/api'
import type { Prefill } from '@/components/issues/issue-form'

// The customer's name or email, else the member who added it.
export const feedbackWho = (f: Pick<Feedback, 'name' | 'email' | 'created_by'>) =>
  f.name || f.email || (f.created_by ? `Added by ${f.created_by.name}` : 'Anonymous')

// Title: the body's first non-empty line, cut to 80 characters. Description: the body, a paragraph per line.
export function feedbackPrefill(f: Pick<Feedback, 'id' | 'body'>): Prefill {
  const lines = f.body.trim().split(/\r?\n/)
  const first = lines.find((l) => l.trim())?.trim() ?? ''
  return {
    title: first.length > 80 ? `${first.slice(0, 79).trimEnd()}…` : first,
    description: { type: 'doc', content: lines.map((l) => ({ type: 'paragraph', ...(l ? { content: [{ type: 'text', text: l }] } : {}) })) },
    feedback: f.id,
  }
}
