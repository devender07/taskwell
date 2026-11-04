import type { ReactNode, SVGProps } from 'react'

// The app's icon set: 24x24 line icons in currentColor, so they take the text color around them. Every new icon goes
// here and is imported from @taskwell/shared/icons; size defaults to 18.
type IconProps = Omit<SVGProps<SVGSVGElement>, 'children'> & { size?: number }

function Icon({ size = 18, children, ...props }: IconProps & { children: ReactNode }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.75}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
      {...props}
    >
      {children}
    </svg>
  )
}

export const SearchIcon = (props: IconProps) => (
  <Icon {...props}>
    <circle cx="11" cy="11" r="7" />
    <path d="m20 20-3.5-3.5" />
  </Icon>
)

export const HomeIcon = (props: IconProps) => (
  <Icon {...props}>
    <path d="m3 10 9-7 9 7v10a1 1 0 0 1-1 1h-5v-7H9v7H4a1 1 0 0 1-1-1z" />
  </Icon>
)

export const InboxIcon = (props: IconProps) => (
  <Icon {...props}>
    <path d="M22 12h-6l-2 3h-4l-2-3H2" />
    <path d="M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z" />
  </Icon>
)

export const UserIcon = (props: IconProps) => (
  <Icon {...props}>
    <circle cx="12" cy="8" r="4" />
    <path d="M4 21a8 8 0 0 1 16 0" />
  </Icon>
)

export const UsersIcon = (props: IconProps) => (
  <Icon {...props}>
    <circle cx="9" cy="8" r="4" />
    <path d="M2 21a7 7 0 0 1 14 0" />
    <path d="M16 4.13a4 4 0 0 1 0 7.75M22 21a7 7 0 0 0-4-6.3" />
  </Icon>
)

export const UserMinusIcon = (props: IconProps) => (
  <Icon {...props}>
    <circle cx="9" cy="8" r="4" />
    <path d="M2 21a7 7 0 0 1 14 0M16 11h6" />
  </Icon>
)

export const LeaveIcon = (props: IconProps) => (
  <Icon {...props}>
    <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4M16 17l5-5-5-5M21 12H9" />
  </Icon>
)

export const FolderIcon = (props: IconProps) => (
  <Icon {...props}>
    <path d="M3 6a1 1 0 0 1 1-1h5l2 2h9a1 1 0 0 1 1 1v11a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1z" />
  </Icon>
)

export const RoadmapIcon = (props: IconProps) => (
  <Icon {...props}>
    <path d="M4 6h9M8 12h12M6 18h8" strokeWidth={3} />
  </Icon>
)

export const SettingsIcon = (props: IconProps) => (
  <Icon {...props}>
    <circle cx="12" cy="12" r="3" />
    <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" />
  </Icon>
)

export const BellIcon = (props: IconProps) => (
  <Icon {...props}>
    <path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9" />
    <path d="M10.3 21a1.94 1.94 0 0 0 3.4 0" />
  </Icon>
)

export const KeyIcon = (props: IconProps) => (
  <Icon {...props}>
    <circle cx="7.5" cy="15.5" r="4.5" />
    <path d="m10.7 12.3 9.8-9.8M17 6l3 3M14.5 8.5l2 2" />
  </Icon>
)

export const GridIcon = (props: IconProps) => (
  <Icon {...props}>
    <rect x="3" y="3" width="7" height="7" rx="1" />
    <rect x="14" y="3" width="7" height="7" rx="1" />
    <rect x="3" y="14" width="7" height="7" rx="1" />
    <rect x="14" y="14" width="7" height="7" rx="1" />
  </Icon>
)

export const ShieldIcon = (props: IconProps) => (
  <Icon {...props}>
    <path d="M12 3 4 6v6c0 5 3.5 8 8 9 4.5-1 8-4 8-9V6z" />
    <path d="m9 12 2 2 4-4" />
  </Icon>
)

export const WorkflowIcon = (props: IconProps) => (
  <Icon {...props}>
    <circle cx="6" cy="6" r="2.5" />
    <circle cx="18" cy="18" r="2.5" />
    <path d="M6 8.5V12a3 3 0 0 0 3 3h6.5" />
  </Icon>
)

export const TagIcon = (props: IconProps) => (
  <Icon {...props}>
    <path d="M3 12V4a1 1 0 0 1 1-1h8l9 9-9 9z" />
    <circle cx="8" cy="8" r="1.5" />
  </Icon>
)

export const BranchIcon = (props: IconProps) => (
  <Icon {...props}>
    <circle cx="6" cy="5" r="2" />
    <circle cx="6" cy="19" r="2" />
    <circle cx="18" cy="7" r="2" />
    <path d="M6 7v10M18 9c0 5-12 3-12 8" />
  </Icon>
)

export const FlaskIcon = (props: IconProps) => (
  <Icon {...props}>
    <path d="M9 3h6M10 3v6l-5 9a2 2 0 0 0 1.8 3h10.4A2 2 0 0 0 19 18l-5-9V3" />
    <path d="M7.5 14h9" />
  </Icon>
)

export const PlugIcon = (props: IconProps) => (
  <Icon {...props}>
    <path d="M9 2v6M15 2v6" />
    <path d="M6 8h12v4a6 6 0 0 1-12 0z" />
    <path d="M12 18v4" />
  </Icon>
)

export const WebhookIcon = (props: IconProps) => (
  <Icon {...props}>
    <path d="M18 16.98h-5.99c-1.1 0-1.95.94-2.48 1.9A4 4 0 0 1 2 17c.01-.7.2-1.4.57-2" />
    <path d="m6 17 3.13-5.78c.53-.97.1-2.18-.5-3.1a4 4 0 1 1 6.89-4.06" />
    <path d="m12 6 3.13 5.73C15.66 12.7 16.9 13 18 13a4 4 0 0 1 0 8" />
  </Icon>
)

export const LockIcon = (props: IconProps) => (
  <Icon {...props}>
    <rect x="5" y="11" width="14" height="10" rx="2" />
    <path d="M8 11V7a4 4 0 0 1 8 0v4" />
  </Icon>
)

export const ArrowLeftIcon = (props: IconProps) => (
  <Icon {...props}>
    <path d="M19 12H5" />
    <path d="m12 19-7-7 7-7" />
  </Icon>
)

const STATUS_SHAPES = {
  circle: <circle cx="12" cy="12" r="9" />,
  'circle-dashed': <circle cx="12" cy="12" r="9" strokeDasharray="3.5 3.5" />,
  'circle-half': (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 6a6 6 0 0 1 0 12z" fill="currentColor" stroke="none" />
    </>
  ),
  'circle-check': (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="m8 12 3 3 5-6" />
    </>
  ),
  'circle-x': (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="m9 9 6 6M15 9l-6 6" />
    </>
  ),
  eye: (
    <>
      <path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z" />
      <circle cx="12" cy="12" r="3" />
    </>
  ),
  refresh: <path d="M20 11a8 8 0 0 0-14.9-4M4 3v4h4M4 13a8 8 0 0 0 14.9 4M20 21v-4h-4" />,
  pause: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M10 9v6M14 9v6" />
    </>
  ),
  clock: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 7v5l3 2" />
    </>
  ),
  flag: <path d="M5 21V4h11l-2 4 2 4H5" />,
  star: <path d="m12 3 2.8 5.8 6.2.8-4.5 4.4 1.1 6.2-5.6-3-5.6 3 1.1-6.2L3 9.6l6.2-.8z" />,
  bug: (
    <>
      <path d="M12 7a4 4 0 0 1 4 4v4a4 4 0 0 1-8 0v-4a4 4 0 0 1 4-4z" />
      <path d="M10 7.5 8.5 4M14 7.5 15.5 4M3 13h5M16 13h5M4 8l4 2M20 8l-4 2M4 19l4-2M20 19l-4-2M12 11v8" />
    </>
  ),
  rocket: (
    <>
      <path d="M4.5 16.5c-1.5 1.3-2 5-2 5s3.7-.5 5-2a2.1 2.1 0 0 0-3-3z" />
      <path d="m12 15-3-3a22 22 0 0 1 2-4A12.9 12.9 0 0 1 22 2c0 2.7-.8 7.5-6 11a22 22 0 0 1-4 2z" />
      <path d="M9 12H4s.6-3 2-4c1.6-1.1 5 0 5 0M12 15v5s3-.6 4-2c1.1-1.6 0-5 0-5" />
    </>
  ),
  alert: (
    <>
      <path d="M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0z" />
      <path d="M12 9v4M12 17h.01" />
    </>
  ),
  lock: (
    <>
      <rect x="5" y="11" width="14" height="10" rx="2" />
      <path d="M8 11V7a4 4 0 0 1 8 0v4" />
    </>
  ),
  message: <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z" />,
}

export type StatusIconKey = keyof typeof STATUS_SHAPES

// The icons a status type can have (docs/status-types-plan.md); the backend validates against the same keys.
export const STATUS_ICONS = Object.keys(STATUS_SHAPES) as StatusIconKey[]

// A status type's icon in its color (currentColor when none); unknown keys draw a plain circle.
export function StatusTypeIcon({ icon, color, size = 14, title, style, ...props }: Omit<IconProps, 'color'> & { icon: string; color?: string | null; title?: string }) {
  return (
    <Icon size={size} style={{ flexShrink: 0, color: color ?? undefined, ...style }} {...props}>
      {title ? <title>{title}</title> : null}
      {STATUS_SHAPES[icon as StatusIconKey] ?? STATUS_SHAPES.circle}
    </Icon>
  )
}

export const ChevronUpIcon = (props: IconProps) => (
  <Icon {...props}>
    <path d="m6 15 6-6 6 6" />
  </Icon>
)

export const ChevronDownIcon = (props: IconProps) => (
  <Icon {...props}>
    <path d="m6 9 6 6 6-6" />
  </Icon>
)

export const MenuIcon = (props: IconProps) => (
  <Icon {...props}>
    <path d="M4 7h16M4 12h16M4 17h16" />
  </Icon>
)

export const XIcon = (props: IconProps) => (
  <Icon {...props}>
    <path d="M18 6 6 18M6 6l12 12" />
  </Icon>
)

export const CheckIcon = (props: IconProps) => (
  <Icon {...props}>
    <path d="M20 6 9 17l-5-5" />
  </Icon>
)

export const PlusIcon = (props: IconProps) => (
  <Icon {...props}>
    <path d="M12 5v14M5 12h14" />
  </Icon>
)

export const MoreIcon = (props: IconProps) => (
  <Icon fill="currentColor" stroke="none" {...props}>
    <circle cx={5} cy={12} r={1.75} />
    <circle cx={12} cy={12} r={1.75} />
    <circle cx={19} cy={12} r={1.75} />
  </Icon>
)

export const PinIcon = (props: IconProps) => (
  <Icon {...props}>
    <path d="M12 17v5" />
    <path d="M9 10.76a2 2 0 0 1-1.11 1.79l-1.78.9A2 2 0 0 0 5 15.24V16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-.76a2 2 0 0 0-1.11-1.79l-1.78-.9A2 2 0 0 1 15 10.76V7a1 1 0 0 1 1-1 2 2 0 0 0 0-4H8a2 2 0 0 0 0 4 1 1 0 0 1 1 1z" />
  </Icon>
)

export const GripIcon = (props: IconProps) => (
  <Icon fill="currentColor" stroke="none" {...props}>
    {[6, 12, 18].map((y) => (
      <g key={y}>
        <circle cx={9} cy={y} r={1.9} />
        <circle cx={15} cy={y} r={1.9} />
      </g>
    ))}
  </Icon>
)

export const SmileIcon = (props: IconProps) => (
  <Icon {...props}>
    <circle cx="12" cy="12" r="9" />
    <path d="M8 14s1.5 2 4 2 4-2 4-2M9 9h.01M15 9h.01" />
  </Icon>
)

export const FeedbackIcon = (props: IconProps) => (
  <Icon {...props}>
    <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z" />
    <path d="M12 7v4M12 14h.01" />
  </Icon>
)

export const MessageIcon = (props: IconProps) => (
  <Icon {...props}>
    <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z" />
  </Icon>
)

export const SendIcon = (props: IconProps) => (
  <Icon {...props}>
    <path d="m22 2-7 20-4-9-9-4z" />
    <path d="M22 2 11 13" />
  </Icon>
)

// filled: the pupil is solid, for "watching".
export const EyeIcon = ({ filled, ...props }: IconProps & { filled?: boolean }) => (
  <Icon {...props}>
    <path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z" />
    <circle cx="12" cy="12" r="3" fill={filled ? 'currentColor' : 'none'} />
  </Icon>
)

type GlyphProps = { color: string; label?: string; size?: number }
const glyphA11y = (label?: string) => (label ? { role: 'img', 'aria-label': label } : { 'aria-hidden': true as const })

// A built-in status category's mark: dashed (backlog), empty (unstarted), half (started), check (completed), cross
// (canceled).
export function CategoryGlyph({ category, color, label, size = 14 }: GlyphProps & { category: 'backlog' | 'unstarted' | 'started' | 'completed' | 'canceled' }) {
  return (
    <svg width={size} height={size} viewBox="0 0 14 14" fill="none" stroke={color} strokeWidth={1.5} className="flex-noshrink" {...glyphA11y(label)}>
      {label ? <title>{label}</title> : null}
      {category === 'backlog' ? <circle cx={7} cy={7} r={6} strokeDasharray="2 2" /> : null}
      {category === 'unstarted' ? <circle cx={7} cy={7} r={6} /> : null}
      {category === 'started' ? (
        <>
          <circle cx={7} cy={7} r={6} />
          <path d="M7 3a4 4 0 0 1 0 8z" fill={color} stroke="none" />
        </>
      ) : null}
      {category === 'completed' || category === 'canceled' ? (
        <>
          <circle cx={7} cy={7} r={6.5} fill={color} stroke="none" />
          <path d={category === 'completed' ? 'm4.5 7 1.75 1.75L9.5 5.5' : 'm5 5 4 4M9 5 5 9'} stroke="var(--bg)" strokeLinecap="round" strokeLinejoin="round" />
        </>
      ) : null}
    </svg>
  )
}

// A test result's mark: dashed (never run), empty (not run), check, cross, dash (skipped), alert (error), half
// (running).
export function ResultGlyph({ status, color, label, size = 16 }: GlyphProps & { status: 'pending' | 'running' | 'passed' | 'failed' | 'skipped' | 'error' | null }) {
  return (
    <svg width={size} height={size} viewBox="0 0 16 16" fill="none" stroke={color} strokeWidth={1.5} strokeLinecap="round" className="flex-noshrink" {...glyphA11y(label)}>
      {label ? <title>{label}</title> : null}
      <circle cx={8} cy={8} r={7} strokeDasharray={status ? undefined : '2 2'} />
      {status === 'passed' ? <path d="m5 8 2 2 4-4" /> : null}
      {status === 'failed' ? <path d="m5.5 5.5 5 5m0-5-5 5" /> : null}
      {status === 'skipped' ? <path d="M5 8h6" /> : null}
      {status === 'error' ? <path d="M8 4.5v4m0 3h.01" /> : null}
      {status === 'running' ? <path d="M8 1a7 7 0 0 1 0 14z" fill={color} stroke="none" /> : null}
    </svg>
  )
}

export const CalendarIcon = (props: IconProps) => (
  <Icon {...props}>
    <rect x="3" y="5" width="18" height="16" rx="2" />
    <path d="M16 3v4M8 3v4M3 10h18" />
  </Icon>
)

export const ChevronLeftIcon = (props: IconProps) => (
  <Icon {...props}>
    <path d="m15 6-6 6 6 6" />
  </Icon>
)

export const ChevronRightIcon = (props: IconProps) => (
  <Icon {...props}>
    <path d="m9 6 6 6-6 6" />
  </Icon>
)

// One path drawn as an icon, for sets kept as path data (the editor toolbar).
export const PathIcon = ({ d, ...props }: IconProps & { d: string }) => (
  <Icon {...props}>
    <path d={d} />
  </Icon>
)

export const LayersIcon = (props: IconProps) => (
  <Icon {...props}>
    <path d="m12 3 9 5-9 5-9-5z" />
    <path d="m3 13 9 5 9-5" />
  </Icon>
)

export const TrashIcon = (props: IconProps) => (
  <Icon {...props}>
    <path d="M4 7h16M10 11v6M14 11v6" />
    <path d="M5 7l1 12a2 2 0 0 0 2 2h8a2 2 0 0 0 2-2l1-12M9 7V4h6v3" />
  </Icon>
)

export const FileIcon = (props: IconProps) => (
  <Icon {...props}>
    <path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z" />
    <path d="M14 3v5h5M9 13h6M9 17h4" />
  </Icon>
)
