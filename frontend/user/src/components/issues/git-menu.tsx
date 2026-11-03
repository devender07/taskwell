import { useState } from 'react'
import { IconButton } from '@tether/shared/icon-button'
import { BranchIcon } from '@tether/shared/icons'
import { Popover } from '@tether/shared/popover'
import type { Issue } from '@/api'
import { branchName, commitMessage } from './branch'

export function GitMenu({ issue }: { issue: Issue }) {
  const [copied, setCopied] = useState<string | null>(null)
  const branch = branchName(issue.identifier, issue.title)
  const items = [
    { key: 'branch', label: 'Copy branch name', text: branch },
    { key: 'checkout', label: 'Copy git checkout command', text: `git checkout -b ${branch}` },
    { key: 'commit', label: 'Copy commit message', text: commitMessage(issue.identifier, issue.title) },
  ]

  function copy(key: string, text: string) {
    navigator.clipboard.writeText(text).then(
      () => setCopied(key),
      () => setCopied('error'),
    )
  }

  return (
    <Popover
      aria-label="Git"
      placement="bottom-end"
      trigger={(props) => (
        <IconButton aria-label="Git branch and commit" tooltip="Git branch and commit" {...props}>
          <BranchIcon size={16} />
        </IconButton>
      )}
    >
      <div className="grid p-1" style={{ width: 280 }}>
        <p className="popover-title m-0 ellipsis" title={branch}>{branch}</p>
        {items.map((item) => (
          <button key={item.key} type="button" className="popover-item justify-between gap-2" onClick={() => copy(item.key, item.text)}>
            {item.label}
            {copied === item.key ? <span className="text-caption text-secondary">Copied</span> : null}
          </button>
        ))}
        {copied === 'error' ? <p role="alert" className="m-0 px-3 py-2 text-caption text-accent">Couldn't copy. Try again.</p> : null}
      </div>
    </Popover>
  )
}
