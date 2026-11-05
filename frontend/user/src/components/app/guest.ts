const HIDDEN = ['inbox', 'notes', 'feedback', 'roadmap', 'team', 'members']
export const GUEST_SETTINGS = ['profile', 'notifications', 'api-tokens']

// The workspace pages a guest can't use, by pathname (`/<workspace>/<section>/…`): their links are hidden and the
// pages redirect to the workspace home. The API refuses their data either way.
export function guestBlocked(pathname: string) {
  const [section = '', sub] = pathname.split('/').slice(2)
  return HIDDEN.includes(section) || (section === 'settings' && sub !== undefined && sub !== '' && !GUEST_SETTINGS.includes(sub))
}
