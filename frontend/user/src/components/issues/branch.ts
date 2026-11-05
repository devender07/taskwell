// The git branch for an issue: "eng-142-fix-login-redirect", lowercase ASCII, at most 50 characters.
export function branchName(identifier: string, title: string) {
  const slug = title
    .normalize('NFKD')
    .replace(/[̀-ͯ]/g, '')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-|-$/g, '')
  return `${identifier.toLowerCase()}-${slug}`.slice(0, 50).replace(/-+$/, '')
}

// The commit message for an issue, led by its identifier so a PR titled from it links back: "ENG-142 Fix login redirect".
export const commitMessage = (identifier: string, title: string) => `${identifier} ${title.trim()}`.trim()
