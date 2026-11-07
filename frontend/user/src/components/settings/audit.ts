export const AUDIT_ACTIONS: Record<string, string> = {
  'workspace.renamed': 'Renamed the workspace',
  'workspace.url_changed': "Changed the workspace's URL",
  'workspace.exported': 'Exported the workspace',
  'member.invited': 'Invited a member',
  'invite.resent': 'Resent an invite',
  'invite.canceled': 'Canceled an invite',
  'member.joined': 'Joined the workspace',
  'member.role_changed': "Changed a member's role",
  'member.removed': 'Removed a member',
  'member.left': 'Left the workspace',
  'role.created': 'Created a role',
  'role.updated': 'Updated a role',
  'role.deleted': 'Deleted a role',
  'project.created': 'Created a project',
  'project.deleted': 'Deleted a project',
  'project.archived': 'Archived a project',
  'project.unarchived': 'Unarchived a project',
  'project.made_private': 'Made a project private',
  'project.made_public': 'Made a project public',
  'project.member_added': 'Added a project member',
  'project.member_removed': 'Removed a project member',
  'webhook.created': 'Created a webhook',
  'webhook.deleted': 'Deleted a webhook',
  'webhook.secret_rolled': "Rolled a webhook's secret",
  'integration.connected': 'Connected an integration',
  'integration.disconnected': 'Disconnected an integration',
  'trash.emptied': 'Emptied the trash',
  'issue.purged': 'Deleted an issue permanently',
}

const text = (v: unknown) => (typeof v === 'string' ? v : '')
const count = (v: unknown) => (Array.isArray(v) ? v.length : typeof v === 'number' ? v : 0)
const plural = (n: number, word: string) => `${n} ${word}${n === 1 ? '' : 's'}`
const capital = (s: string) => s.charAt(0).toUpperCase() + s.slice(1)
const change = (d: Record<string, unknown>) => (d.from || d.to ? `${text(d.from)} → ${text(d.to)}` : '')

export function auditDetail(action: string, d: Record<string, unknown>): string {
  switch (action) {
    case 'workspace.renamed':
    case 'workspace.url_changed':
    case 'member.role_changed':
      return change(d)
    case 'member.invited':
      return d.guest ? 'Guest' : capital(text(d.role))
    case 'member.joined':
      return capital(text(d.role))
    case 'role.created':
      return plural(count(d.permissions), 'permission')
    case 'role.updated': {
      const added = count(d.added)
      const removed = count(d.removed)
      const parts = [
        d.renamed_from ? `renamed from ${text(d.renamed_from)}` : '',
        added ? `added ${plural(added, 'permission')}` : '',
        removed ? `removed ${added ? removed : plural(removed, 'permission')}` : '',
      ]
      return capital(parts.filter(Boolean).join(', '))
    }
    case 'project.created':
    case 'project.deleted':
      return text(d.name)
    case 'project.member_added':
    case 'project.member_removed':
      return text(d.email)
    case 'trash.emptied':
      return plural(count(d.count), 'issue')
    case 'issue.purged':
      return text(d.title)
    default:
      return ''
  }
}
