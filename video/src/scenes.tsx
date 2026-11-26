import { interpolate, useCurrentFrame } from 'remotion'
import { C, MONO } from './theme'
import { Caption, Caret, Key, Logo, Rise, Scene, Tag, typed, useIn, useLayout, Window } from './ui'

// 1. Intro: the logo, the name and the promise.
export function Intro() {
  const logo = useIn(4, 14)
  return (
    <Scene>
      <div style={{ position: 'absolute', inset: 0, display: 'grid', placeContent: 'center', justifyItems: 'center', gap: 36, textAlign: 'center' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 28, opacity: logo, transform: `scale(${interpolate(logo, [0, 1], [0.8, 1])})` }}>
          <Logo size={120} />
          <span style={{ fontSize: 96, fontWeight: 590, letterSpacing: -2 }}>Taskwell</span>
        </div>
        <Rise delay={30}>
          <div style={{ fontSize: 88, fontWeight: 510, letterSpacing: -2, lineHeight: 1.05 }}>Plan it, build it, test it.</div>
        </Rise>
        <Rise delay={48}>
          <div style={{ fontSize: 88, fontWeight: 510, letterSpacing: -2, lineHeight: 1.05, color: C.text2 }}>All in one place.</div>
        </Rise>
      </div>
    </Scene>
  )
}

const COLS = ['Todo', 'In progress', 'Done'] as const
const COL_ICON = [
  { border: C.text2, background: 'transparent' },
  { border: C.yellow, background: `conic-gradient(${C.yellow} 0 50%, transparent 0)` },
  { border: C.link, background: C.link },
]
const STATIC: { col: number; key: string; title: string; tags: [string, 'plain' | 'urgent' | 'pass'][] }[] = [
  { col: 0, key: 'ENG-139', title: 'Export cycle report as CSV', tags: [['Reports', 'plain']] },
  { col: 0, key: 'ENG-131', title: 'Dark mode for emails', tags: [['Design', 'plain']] },
  { col: 1, key: 'ENG-125', title: 'S3 uploads with presigned links', tags: [['Infra', 'plain']] },
  { col: 2, key: 'ENG-120', title: 'Sign in with Google', tags: [['AI test passed', 'pass']] },
]

function Card({ id, title, tags, live }: { id: string; title: string; tags: [string, 'plain' | 'urgent' | 'pass'][]; live?: boolean }) {
  return (
    <div style={{ display: 'grid', gap: 12, padding: 20, background: C.bg, border: `1px solid ${live ? 'rgba(94,106,210,0.6)' : C.border}`, borderRadius: 10, boxShadow: live ? '0 20px 60px rgba(0,0,0,0.5)' : 'none' }}>
      <span style={{ fontFamily: MONO, fontSize: 17, color: C.text2 }}>{id}</span>
      <span style={{ fontSize: 24 }}>{title}</span>
      <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
        {tags.map(([t, tone]) => (
          <Tag key={t} tone={tone}>
            {t}
          </Tag>
        ))}
      </div>
    </div>
  )
}

// 2. Board: a card moves Todo → In progress → Done.
export function Board() {
  const frame = useCurrentFrame()
  const toProgress = useIn(80, 16)
  const toDone = useIn(190, 16)
  const stage = toProgress + toDone // 0..2, eased
  const { W, portrait } = useLayout()
  const colW = ((portrait ? W - 64 : 1500) - 64 - 48) / 3
  const slot = portrait ? 160 : 152 // room for the moving card above each column's cards
  const lifted = Math.sin((stage % 1) * Math.PI) * 0.03 // tilts while it travels
  const count = (c: number) => STATIC.filter((s) => s.col === c).length + (Math.round(stage) === c ? 1 : 0)
  return (
    <Scene>
      <Caption eyebrow="Issues, lists and boards" title="Move work forward in one drag." />
      <Window title="ENG · Board" width={1500} height={620} tall={760}>
        <div style={{ position: 'relative', display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: 24, padding: 32 }}>
          {COLS.map((name, c) => (
            <div key={name} style={{ display: 'grid', alignContent: 'start', gap: 16 }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 12, fontSize: 21, fontWeight: 510 }}>
                <span style={{ width: 16, height: 16, borderRadius: 99, border: `2.5px solid ${COL_ICON[c].border}`, background: COL_ICON[c].background }} />
                {name} <span style={{ color: C.text2, fontWeight: 400 }}>{count(c)}</span>
              </div>
              <div style={{ height: slot }} />
              {STATIC.filter((s) => s.col === c).map((s) => (
                <Card key={s.key} id={s.key} title={s.title} tags={s.tags} />
              ))}
            </div>
          ))}
          <div style={{ position: 'absolute', top: 32 + 37, left: 32 + stage * (colW + 24), width: colW, transform: `scale(${1 + lifted}) rotate(${lifted * 40}deg)` }}>
            <Card id="ENG-128" title="Drag to reorder issues" tags={[['Feature', 'plain'], [stage > 1.5 ? 'Done' : 'Urgent', stage > 1.5 ? 'pass' : 'urgent']]} live={frame > 70 && stage < 1.98} />
          </div>
        </div>
      </Window>
    </Scene>
  )
}

const RESULTS = [
  ['Create issue', 'C'],
  ['Create project', ''],
  ['Go to my issues', ''],
  ['Search issues and projects', ''],
] as const

// 3. Command palette: Ctrl+K, type, pick.
export function Palette() {
  const frame = useCurrentFrame()
  const query = typed('create iss', frame, 50, 4)
  const press = frame > 130 && frame < 150
  return (
    <Scene>
      <Caption eyebrow="Keyboard first" title="Ctrl+K for everything. C to create." />
      <Window title="Command palette" width={1100} height={480} tall={500}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 16, padding: '26px 32px', borderBottom: `1px solid ${C.border}`, fontSize: 30 }}>
          <span style={{ color: C.text2 }}>⌕</span>
          <span>{query || <span style={{ color: C.text2 }}>Type a command or search</span>}</span>
          <Caret />
          <span style={{ marginLeft: 'auto' }}>
            <Key>Ctrl K</Key>
          </span>
        </div>
        <div style={{ padding: 16, display: 'grid', gap: 6 }}>
          {RESULTS.map(([label, keys], i) => (
            <Rise key={label} delay={20 + i * 4} distance={10}>
              <div
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  padding: '18px 20px',
                  borderRadius: 10,
                  fontSize: 26,
                  background: i === 0 ? (press ? 'rgba(94,106,210,0.35)' : 'rgba(255,255,255,0.06)') : 'transparent',
                  color: i === 0 || query.length < 3 ? C.text : C.text2,
                }}
              >
                {label}
                {keys ? (
                  <span style={{ marginLeft: 'auto' }}>
                    <Key>{keys}</Key>
                  </span>
                ) : null}
              </div>
            </Rise>
          ))}
        </div>
      </Window>
    </Scene>
  )
}

const DOC = 'Ship the beta on Monday. Turn on beta mode, set the bug report link, and invite the first ten teams.'

function Cursor({ name, color }: { name: string; color: string }) {
  return (
    <span style={{ position: 'relative', display: 'inline-block', width: 0, height: 0 }}>
      <span style={{ position: 'absolute', left: 4, top: -30 }}>
        <span style={{ display: 'block', width: 3, height: 38, background: color }} />
        <span style={{ position: 'absolute', top: -28, left: 0, padding: '2px 10px', fontSize: 17, fontWeight: 590, color: '#fff', background: color, borderRadius: '6px 6px 6px 0', whiteSpace: 'nowrap' }}>{name}</span>
      </span>
    </span>
  )
}

// 4. Live docs: two people in one description.
export function Docs() {
  const frame = useCurrentFrame()
  const text = typed(DOC, frame, 40, 2)
  const ben = useIn(120) // Ben's cursor slides in to the end of the item he ticks
  const ticked = frame > 190
  return (
    <Scene>
      <Caption eyebrow="Live docs" title="Write it together, in real time." />
      <Window title="Launch plan · ENG-142" width={1400} height={600} tall={520}>
        <div style={{ position: 'relative', padding: '40px 56px', display: 'grid', gap: 26 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 14 }}>
            <div style={{ fontSize: 44, fontWeight: 590 }}>Launch plan</div>
            <div style={{ marginLeft: 'auto', display: 'flex' }}>
              {[
                ['AS', C.accent],
                ['BN', C.link],
              ].map(([i, c], n) => (
                <span key={i} style={{ display: 'grid', placeItems: 'center', width: 48, height: 48, marginLeft: n ? -10 : 0, borderRadius: 99, background: c, border: `3px solid ${C.surface}`, fontSize: 18, fontWeight: 590 }}>
                  {i}
                </span>
              ))}
            </div>
          </div>
          <div style={{ fontSize: 30, lineHeight: 1.6, maxWidth: 1180 }}>
            {text}
            <span style={{ position: 'relative', display: 'inline-block', width: 0, height: 0 }}>
              <span style={{ position: 'absolute', left: 4, top: -34 }}>
                <span style={{ display: 'block', width: 3, height: 40, background: C.accent }} />
                <span style={{ position: 'absolute', top: 40, left: 0, padding: '2px 10px', fontSize: 17, fontWeight: 590, background: C.accent, borderRadius: '0 6px 6px 6px', whiteSpace: 'nowrap' }}>Asha</span>
              </span>
            </span>
          </div>
          <div style={{ display: 'grid', gap: 14, fontSize: 26, color: C.text2 }}>
            {['Beta notice live', 'Report a bug link set', 'Backups to R2 checked'].map((l, i) => (
              <Rise key={l} delay={100 + i * 12} distance={10}>
                <span style={{ color: i === 1 && ticked ? C.success : undefined }}>{i === 1 && ticked ? '☑' : '☐'}  {l}</span>
                {i === 1 ? (
                  <span style={{ display: 'inline-block', opacity: ben, transform: `translateX(${(1 - ben) * 400}px)` }}>
                    <Cursor name="Ben" color={C.link} />
                  </span>
                ) : null}
              </Rise>
            ))}
          </div>
        </div>
      </Window>
    </Scene>
  )
}

const STEPS = ['Open the sign-in page', 'Click "Continue with Google"', 'Choose the test account', 'Check the workspace home loads']

// 5. AI testing: plain-words steps run in a real browser.
export function AiTest() {
  const frame = useCurrentFrame()
  const done = frame > 40 + STEPS.length * 40
  const { portrait } = useLayout()
  return (
    <Scene>
      <Caption eyebrow="AI testing" title="Describe a test. AI runs it in a real browser." />
      <Window title="Test run · ENG-120" width={1500} height={600} tall={640}>
        <div style={{ display: 'grid', gridTemplateColumns: portrait ? '1fr' : '1.1fr 1fr', gap: 32, padding: 36 }}>
          <div style={{ display: 'grid', alignContent: 'start', gap: 18, fontFamily: MONO, fontSize: 25 }}>
            <div style={{ color: C.text2 }}>Running test: "Sign in with Google"</div>
            {STEPS.map((s, i) => {
              const at = 40 + i * 40
              const on = frame >= at
              return (
                <div key={s} style={{ opacity: interpolate(frame, [at, at + 12], [0, 1], { extrapolateLeft: 'clamp', extrapolateRight: 'clamp' }), color: on ? C.text : C.text2 }}>
                  <span style={{ color: frame > at + 30 ? C.success : C.link }}>{frame > at + 30 ? '✓' : '→'}</span> {s}
                </div>
              )
            })}
            {done ? (
              <Rise delay={40 + STEPS.length * 40} distance={8}>
                <span style={{ color: C.success }}>✓ Passed in 18s · 4 screenshots saved</span>
              </Rise>
            ) : null}
          </div>
          <div style={{ display: 'grid', gridTemplateColumns: portrait ? 'repeat(4, 1fr)' : '1fr 1fr', gap: 16 }}>
            {STEPS.map((s, i) => {
              const at = 40 + i * 40 + 20
              const p = interpolate(frame, [at, at + 14], [0, 1], { extrapolateLeft: 'clamp', extrapolateRight: 'clamp' })
              return (
                <div key={s} style={{ opacity: p, transform: `scale(${0.9 + p * 0.1})`, display: 'grid', alignContent: 'space-between', padding: 16, height: portrait ? 200 : 220, background: C.bg, border: `1px solid ${C.border}`, borderRadius: 10 }}>
                  <div style={{ display: 'grid', gap: 10 }}>
                    <div style={{ height: 14, width: '60%', borderRadius: 4, background: 'rgba(255,255,255,0.12)' }} />
                    <div style={{ height: 10, width: '85%', borderRadius: 4, background: 'rgba(255,255,255,0.06)' }} />
                    <div style={{ height: 10, width: '70%', borderRadius: 4, background: 'rgba(255,255,255,0.06)' }} />
                    <div style={{ height: 34, width: '55%', marginTop: 8, borderRadius: 99, background: i === 1 ? C.text : 'rgba(94,106,210,0.4)' }} />
                  </div>
                  <span style={{ fontSize: 16, color: C.text2 }}>Step {i + 1}</span>
                </div>
              )
            })}
          </div>
        </div>
      </Window>
    </Scene>
  )
}

const CMD = 'claude mcp add --transport http taskwell https://mcp.taskwell.dev/mcp'

// 6. MCP: one command, then Claude files the issue.
export function Mcp() {
  const frame = useCurrentFrame()
  const cmd = typed(CMD, frame, 20, 1)
  const ask = useIn(110)
  const reply = useIn(160)
  return (
    <Scene>
      <Caption eyebrow="Built for AI agents" title="Claude works in your workspace, as you." />
      <Window title="Terminal" width={1500} height={600} tall={480}>
        <div style={{ display: 'grid', gap: 26, padding: '34px 40px', fontSize: 26 }}>
          <div style={{ fontFamily: MONO, wordBreak: 'break-all' }}>
            <span style={{ color: C.success }}>$ </span>
            {cmd}
            {cmd.length < CMD.length ? <Caret color={C.text} /> : null}
          </div>
          {frame > 20 + CMD.length + 10 ? <div style={{ fontFamily: MONO, color: C.text2 }}>Added HTTP MCP server taskwell</div> : null}
          <div style={{ opacity: ask, transform: `translateY(${(1 - ask) * 16}px)`, justifySelf: 'end', maxWidth: 1100, padding: '18px 24px', borderRadius: 16, background: 'rgba(255,255,255,0.07)' }}>
            File a bug: the login page redirects in a loop on Safari. Make it urgent.
          </div>
          <div style={{ opacity: reply, transform: `translateY(${(1 - reply) * 16}px)`, display: 'flex', alignItems: 'center', gap: 18, maxWidth: 1300, padding: '18px 24px', borderRadius: 16, border: `1px solid ${C.border}`, background: C.bg }}>
            <span style={{ fontSize: 30, color: C.accent }}>✳</span>
            <span>
              Created <span style={{ fontFamily: MONO, color: C.link }}>ENG-143</span> "Login redirect loop on Safari" in Engineering, priority urgent.
            </span>
          </div>
        </div>
      </Window>
    </Scene>
  )
}

// 7. Call to action.
export function Cta() {
  const underline = useIn(50, 22)
  return (
    <Scene>
      <div style={{ position: 'absolute', inset: 0, background: 'radial-gradient(ellipse 50% 60% at 50% 100%, rgba(249, 24, 128, 0.14), transparent 70%)' }} />
      <div style={{ position: 'absolute', inset: 0, display: 'grid', placeContent: 'center', justifyItems: 'center', gap: 40, textAlign: 'center' }}>
        <Rise>
          <Logo size={110} />
        </Rise>
        <Rise delay={10}>
          <div style={{ fontSize: 92, fontWeight: 590, letterSpacing: -2 }}>Free during the beta.</div>
        </Rise>
        <Rise delay={30}>
          <div style={{ position: 'relative', fontSize: 72, fontWeight: 510, color: C.text }}>
            taskwell.dev
            <div style={{ position: 'absolute', left: 0, right: 0, bottom: -10, height: 6, borderRadius: 3, background: C.accent, transform: `scaleX(${underline})`, transformOrigin: 'left' }} />
          </div>
        </Rise>
        <Rise delay={60}>
          <div style={{ fontSize: 32, color: C.text2 }}>Issues · cycles · live docs · chat · AI testing</div>
        </Rise>
      </div>
    </Scene>
  )
}
