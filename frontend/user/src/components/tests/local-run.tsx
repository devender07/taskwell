import { Link } from '@tanstack/react-router'
import { API_URL } from '@taskwell/shared/api-url'
import { Dialog } from '@taskwell/shared/dialog'
import { CopyButton } from '@/components/settings/integrations'

const API = API_URL || location.origin
const SETUP = `claude mcp add taskwell-tester -e TASKWELL_TOKEN=tth_… ${API === 'https://api.taskwell.dev' ? '' : `-e TASKWELL_URL=${API} `}-- npx -y taskwell-tester`
const VIDEO = 'npx -y playwright-core@1.65.0 install ffmpeg'

function Snippet({ label, text, copyLabel }: { label: string; text: string; copyLabel: string }) {
  return (
    <div className="grid gap-2">
      <div className="flex items-center gap-3">
        <span className="text-label flex-grow">{label}</span>
        <CopyButton text={text} label={copyLabel} />
      </div>
      <pre className="m-0 p-4 bg-surface border rounded-lg" style={{ fontFamily: 'var(--mono)', fontSize: 14, lineHeight: '21px', whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}>
        {text}
      </pre>
    </div>
  )
}

export function RunnerSetup({ slug, label }: { slug: string; label: string }) {
  return (
    <div className="grid gap-2">
      <Snippet label={label} text={SETUP} copyLabel="Copy setup command" />
      <p className="text-caption text-secondary m-0">
        Replace tth_… with your own token.{' '}
        <Link to="/$workspace/settings/api-tokens" params={{ workspace: slug }} style={{ color: 'var(--link)' }}>
          Create an API token
        </Link>
      </p>
      <Snippet label="Then install the video recorder, once" text={VIDEO} copyLabel="Copy install command" />
    </div>
  )
}

export function LocalRunDialog({ open, onClose, slug, identifier, hasCases }: { open: boolean; onClose: () => void; slug: string; identifier: string; hasCases: boolean }) {
  return (
    <Dialog
      open={open}
      onClose={onClose}
      title="Run with your Claude"
      description="Your coding agent tests each case in Chrome on your machine. Every step, screenshot and a video of each case show up here."
    >
      <div className="grid gap-6">
        <RunnerSetup slug={slug} label="1. Set it up once" />
        <Snippet label="2. Ask your agent" text={`Test ${identifier} with taskwell-tester`} copyLabel="Copy prompt" />
        {hasCases ? null : <p className="text-caption text-secondary m-0">Add a test case first. The runner tests the cases on this issue.</p>}
      </div>
    </Dialog>
  )
}
