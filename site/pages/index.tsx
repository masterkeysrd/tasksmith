import { useState } from 'react';
import { withBase } from '@rspress/core/runtime';
import { HOME_CSS } from './homeStyles';

export const frontmatter = {
  pageType: 'custom',
  title: 'TaskSmith — Daemon-Backed AI Coding Agent for the Terminal',
};

const INSTALL_METHODS = [
  {
    id: 'brew',
    label: 'Homebrew',
    meta: 'macOS & Linux',
    lines: [
      'brew tap masterkeysrd/tasksmith https://github.com/masterkeysrd/tasksmith',
      'brew install masterkeysrd/tasksmith/tasksmith',
    ],
  },
  {
    id: 'shell',
    label: 'Shell',
    meta: 'POSIX sh · SHA-256 verified',
    lines: ['curl -fsSL https://github.com/masterkeysrd/tasksmith/releases/latest/download/install.sh | sh'],
  },
  {
    id: 'go',
    label: 'Go',
    meta: 'Go 1.25+ toolchain',
    lines: ['go install github.com/masterkeysrd/tasksmith/cmd/tasksmith@latest'],
  },
] as const;

interface ProviderItem {
  name: string;
  detail: string;
  href: string;
}

const PROVIDERS: ProviderItem[] = [
  {
    name: 'Anthropic',
    detail: 'Extended thinking & caching',
    href: '/reference/providers/anthropic',
  },
  {
    name: 'OpenAI',
    detail: 'Responses API · ZDR store: false',
    href: '/reference/providers/openai',
  },
  {
    name: 'Google GenAI',
    detail: '1M+ context & multimodal',
    href: '/reference/providers/google-genai',
  },
  {
    name: 'Ollama',
    detail: '100% offline · zero egress',
    href: '/reference/providers/ollama',
  },
  {
    name: 'TypeSafe',
    detail: 'Sub-50ms System 1 decisions',
    href: '/reference/providers/typesafe',
  },
  {
    name: 'Antigravity',
    detail: 'Managed effort tiers & OAuth',
    href: '/reference/providers/antigravity',
  },
];

interface Feature {
  badge: string;
  title: string;
  description: string;
  href: string;
  accent: string;
}

const FEATURES: Feature[] = [
  {
    badge: 'IPC + SSE',
    title: 'Client-Daemon Architecture',
    description:
      'On-demand headless Brain Daemon over Unix domain sockets with multiplexed SSE token streaming, SQLite turn journaling, and instant session resumption.',
    href: '/architecture/daemon',
    accent: '#2ac3de',
  },
  {
    badge: 'Seatbelt & Bubblewrap',
    title: 'OS Kernel Sandboxing & Shell AST',
    description:
      'Per-session native kernel confinement, host credential masking, capability dropping, and shellguard AST inspection before any command executes.',
    href: '/architecture/sandbox',
    accent: '#1abc9c',
  },
  {
    badge: 'Multi-Vendor',
    title: 'Triple-Role Model Gateway',
    description:
      'Decouple agents from vendors across completion, decision, and embedding roles with dynamic routing, thinking effort controls, and live cost tracking.',
    href: '/architecture/gateway',
    accent: '#ff5f5f',
  },
  {
    badge: 'CAS Snapshots',
    title: 'Transactional Timeline & Revert',
    description:
      'Automatic content-addressable file snapshots before edit, write, or bash runs. Inspect syntax-highlighted diffs with /changes and roll back with /revert.',
    href: '/guides/cli',
    accent: '#2ac3de',
  },
  {
    badge: 'WARP Manifests',
    title: 'Declarative 4-Tier Workspaces',
    description:
      'Version-control agents, rules, skills, custom tools, and MCP servers across system, user, workspace, and local scopes with isolated .env layering.',
    href: '/guides/workspaces',
    accent: '#1abc9c',
  },
  {
    badge: 'Subagents & MCP',
    title: 'Governed Delegation & Deferred Tools',
    description:
      'Delegate tasks to isolated subagents in git worktrees, discover large MCP and custom tool catalogs on demand via tool_search, and enforce strict policies.',
    href: '/guides/tools-and-mcp',
    accent: '#ff5f5f',
  },
];

const WORKSPACE_TIERS = [
  {
    tier: '4 · Local',
    path: '<subproject>/AGENTS.md · .agents/',
    note: 'Highest precedence — package-specific overrides in monorepos',
  },
  {
    tier: '3 · Workspace',
    path: '<workspace-root>/WORKSPACE.md · .agents/',
    note: 'Repository-committed team agents, skills, rules, and policies',
  },
  {
    tier: '2 · User',
    path: '~/.config/tasksmith/',
    note: 'Personal global providers, credentials, and custom commands',
  },
  {
    tier: '1 · System',
    path: '$XDG_DATA_HOME/tasksmith/builtin/',
    note: 'Embedded built-in tools, @explore subagent, and base defaults',
  },
] as const;

// Palette matching internal/ui/theme/builtin/dark.json & internal/cli/components/logo.go
const TERM = {
  bgCanvas: '#0d1117',
  bgPanel: '#161b22',
  borderDefault: '#30363d',
  borderSubtle: '#21262d',
  fgPrimary: '#e6edf3',
  fgSecondary: '#b1bac4',
  fgMuted: '#9198a1',
  brand: '#9d7cd8',
  iconPrimary: '#56d4dd',
  accent: '#58a6ff',
  permission: '#f0883e',
  success: '#3fb950',
  modeDefault: '#58a6ff',
  diffAddedBg: '#163024',
  diffAdded: '#7ee787',
  diffRemovedBg: '#381a1f',
  diffRemoved: '#ff7b72',
  diffHunkBg: '#1c2430',
  diffHunk: '#79c0ff',
  borderFocus: '#58a6ff',
  logoTop: '#2ac3de',
  logoCenter: '#1abc9c',
  logoBase: '#ff5f5f',
} as const;

const MONO_STACK =
  'var(--rp-font-family-mono, ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace)';

const TOUR_SCENARIOS = [
  {
    id: 'agent',
    label: 'Agent & Subagents',
    windowTitle: 'Refactor rate-limiter middleware',
    caption:
      'Inline @file mentions inject tracked files, matching rules auto-load, and @explore maps callers in parallel before editing and running tests.',
    linkText: 'Explore Interactive CLI Guide →',
    linkHref: '/guides/cli',
  },
  {
    id: 'sandbox',
    label: 'Sandbox & Shell Guard',
    windowTitle: 'Verify production database migration',
    caption:
      'Every shell command passes shellguard AST analysis and workspace policy gates before executing inside macOS Seatbelt or Linux Bubblewrap.',
    linkText: 'Read Permissions & Sandbox Spec →',
    linkHref: '/architecture/permissions',
  },
  {
    id: 'timeline',
    label: 'Timeline & /revert',
    windowTitle: 'Refactor rate-limiter middleware',
    caption:
      'Pre-mutation CAS snapshots track every edit and shell write so you can inspect unified diffs with /changes and roll back instantly with /revert.',
    linkText: 'See CLI Slash Commands →',
    linkHref: '/reference/cli',
  },
] as const;

function TerminalHeader() {
  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'row',
        gap: '1.5rem',
        marginBottom: '1.1rem',
      }}
    >
      <pre
        style={{
          margin: 0,
          padding: 0,
          background: 'transparent',
          fontFamily: 'inherit',
          fontSize: 'inherit',
          lineHeight: 1.35,
          userSelect: 'none',
        }}
      >
        <span style={{ color: TERM.logoTop }}>{'  ▜████████▛ \n'}</span>
        <span style={{ color: TERM.logoCenter }}>{'     █▜▛█    \n'}</span>
        <span style={{ color: TERM.logoBase }}>{'    ▟▛▀▀▜▙   '}</span>
      </pre>
      <div style={{ display: 'flex', flexDirection: 'column', justifyContent: 'center', lineHeight: 1.38 }}>
        <div>
          <span style={{ color: TERM.brand, fontWeight: 700 }}>TaskSmith</span>
        </div>
        <div style={{ color: TERM.fgSecondary }}>anthropic/claude-sonnet-5-5 with high effort</div>
        <div style={{ color: TERM.fgSecondary }}>~/projects/api-service · native sandbox</div>
      </div>
    </div>
  );
}

function TerminalComposerAndStatus({
  modeLabel,
  modeColor,
  rightStats,
}: {
  modeLabel: string;
  modeColor: string;
  rightStats: string;
}) {
  return (
    <>
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          padding: '0.36rem 0',
          borderTop: `1px solid ${TERM.borderFocus}`,
          borderBottom: `1px solid ${TERM.borderFocus}`,
          marginBottom: '0.35rem',
        }}
      >
        <span style={{ color: TERM.iconPrimary, fontWeight: 700, marginRight: '0.55rem' }}>{'>'}</span>
        <span
          style={{
            display: 'inline-block',
            width: '0.52rem',
            height: '1.02rem',
            background: TERM.fgPrimary,
            opacity: 0.85,
          }}
        />
      </div>

      <div
        style={{
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          gap: '1rem',
          fontSize: '0.78rem',
          color: TERM.fgSecondary,
          whiteSpace: 'nowrap',
        }}
      >
        <span>shift+tab cycle mode • /help commands</span>
        <div>
          <span style={{ color: modeColor, fontWeight: 700 }}>{modeLabel}</span>
          <span>{rightStats}</span>
        </div>
      </div>
    </>
  );
}

function AgentScenarioBody() {
  return (
    <>
      <TerminalHeader />

      <div
        style={{
          color: TERM.iconPrimary,
          fontWeight: 700,
          marginBottom: '0.8rem',
          whiteSpace: 'pre-wrap',
        }}
      >
        {'> Refactor @ratelimit.go to bound per-IP buckets and run the HTTP test suite'}
      </div>

      <div style={{ display: 'flex', flexDirection: 'column', gap: '0.2rem', marginBottom: '0.75rem' }}>
        <div style={{ color: TERM.fgMuted, whiteSpace: 'nowrap' }}>
          ● Loaded Rule go-error-handling (matched internal/http/ratelimit.go)
        </div>

        <div style={{ color: TERM.fgSecondary, whiteSpace: 'nowrap' }}>
          ● Thought (Checking sliding-window eviction and middleware call sites...)
        </div>

        <div style={{ whiteSpace: 'nowrap' }}>
          <span style={{ color: TERM.success }}>● </span>
          <span style={{ color: TERM.accent, fontWeight: 700 }}>Spawned </span>
          <span style={{ color: TERM.iconPrimary, fontWeight: 700 }}>@explore</span>
          <span style={{ color: TERM.fgPrimary }}> Map rate-limiter middleware callers</span>
          <span style={{ color: TERM.fgSecondary }}> (0196d4a2)</span>
        </div>

        <div style={{ whiteSpace: 'nowrap' }}>
          <span style={{ color: TERM.success }}>● </span>
          <span style={{ color: TERM.accent, fontWeight: 700 }}>Edited </span>
          <span style={{ color: TERM.fgPrimary }}>internal/http/ratelimit.go</span>
          <span style={{ color: TERM.fgSecondary }}> (</span>
          <span style={{ color: TERM.diffAdded }}>+38</span>
          <span style={{ color: TERM.fgSecondary }}> </span>
          <span style={{ color: TERM.diffRemoved }}>-14</span>
          <span style={{ color: TERM.fgSecondary }}>)</span>
        </div>

        <div style={{ display: 'flex', flexDirection: 'column' }}>
          <div style={{ whiteSpace: 'nowrap' }}>
            <span style={{ color: TERM.success }}>● </span>
            <span style={{ color: TERM.accent, fontWeight: 700 }}>Ran </span>
            <span style={{ color: TERM.fgPrimary }}>Run HTTP middleware tests</span>
            <span style={{ color: TERM.fgSecondary }}> (420ms)</span>
          </div>
          <div style={{ color: TERM.fgSecondary, whiteSpace: 'pre' }}>
            {'  ⎿  > go test ./internal/http/...'}
          </div>
        </div>
      </div>

      <div style={{ color: TERM.fgPrimary, marginBottom: '0.7rem', lineHeight: 1.55 }}>
        Bounded the per-IP token bucket table in{' '}
        <code
          style={{
            background: TERM.bgPanel,
            padding: '0.1rem 0.35rem',
            borderRadius: '4px',
            color: '#a5d6ff',
            fontSize: '0.82rem',
          }}
        >
          internal/http/ratelimit.go
        </code>{' '}
        with LRU eviction and verified burst-boundary cases pass.
      </div>

      <div style={{ color: TERM.fgSecondary, marginBottom: '0.8rem' }}>
        Smithed for 14s · ↑18.4k ↓1.2k · $0.04 · done 4:18 PM
      </div>

      <TerminalComposerAndStatus
        modeLabel="accept_edits"
        modeColor={TERM.diffAdded}
        rightStats=" · 42.8k/200.0k (21%) · $0.04 · ↑18.4k ↓1.2k · Claude Sonnet 5.5 · high"
      />
    </>
  );
}

function SandboxScenarioBody() {
  return (
    <>
      <div
        style={{
          color: TERM.iconPrimary,
          fontWeight: 700,
          marginBottom: '0.75rem',
          whiteSpace: 'pre-wrap',
        }}
      >
        {'> Apply schema migration @004_sessions.sql against staging and verify indexes'}
      </div>

      <div style={{ display: 'flex', flexDirection: 'column', gap: '0.2rem', marginBottom: '0.85rem' }}>
        <div style={{ color: TERM.fgMuted, whiteSpace: 'nowrap' }}>
          ● Loaded Rule bash-safety (tool: bash)
        </div>
        <div style={{ color: TERM.fgSecondary, whiteSpace: 'nowrap' }}>
          ● Thought (Migration alters idx_sessions_updated_at; running psql inside sandbox...)
        </div>
        <div style={{ whiteSpace: 'nowrap' }}>
          <span style={{ color: TERM.permission }}>○ </span>
          <span style={{ color: TERM.accent, fontWeight: 700 }}>Running </span>
          <span style={{ color: TERM.fgPrimary }}>Apply 004_sessions.sql migration</span>
          <span style={{ color: TERM.fgSecondary }}> · awaiting approval</span>
        </div>
      </div>

      {/* Authentic PermissionPrompt (internal/cli/components/permission_prompt.go) */}
      <div
        style={{
          border: `1px solid ${TERM.borderDefault}`,
          borderRadius: '6px',
          padding: '0.55rem 0.85rem',
          marginBottom: '0.85rem',
          display: 'flex',
          flexDirection: 'column',
          gap: '0.18rem',
        }}
      >
        <div style={{ color: TERM.permission, fontWeight: 700 }}>Permission required</div>
        <div style={{ whiteSpace: 'nowrap' }}>
          <span style={{ color: TERM.fgPrimary, fontWeight: 700 }}>bash</span>
          <span style={{ color: TERM.fgSecondary }}> · </span>
          <span style={{ color: TERM.fgSecondary }}>
            psql &quot;$STAGING_DB_URL&quot; -v ON_ERROR_STOP=1 -f migrations/004_sessions.sql
          </span>
        </div>

        <div style={{ marginTop: '0.45rem', display: 'flex', flexDirection: 'column', gap: '0.1rem' }}>
          <div style={{ color: TERM.iconPrimary, fontWeight: 700, whiteSpace: 'pre' }}>
            {'❯ Yes, allow once'}
          </div>
          <div style={{ whiteSpace: 'pre' }}>
            <span style={{ color: TERM.fgSecondary }}>{'  Always allow bash(psql *)      '}</span>
            <span style={{ color: TERM.permission }}>[session]</span>
          </div>
          <div style={{ color: TERM.fgSecondary, whiteSpace: 'pre' }}>{'  No, deny'}</div>
        </div>

        <div
          style={{
            marginTop: '0.45rem',
            fontSize: '0.76rem',
            color: TERM.fgMuted,
            whiteSpace: 'nowrap',
          }}
        >
          <span style={{ color: TERM.fgSecondary, fontWeight: 600 }}>↑/↓</span> Navigate ·{' '}
          <span style={{ color: TERM.fgSecondary, fontWeight: 600 }}>tab</span> Scope ·{' '}
          <span style={{ color: TERM.fgSecondary, fontWeight: 600 }}>enter</span> Confirm ·{' '}
          <span style={{ color: TERM.fgSecondary, fontWeight: 600 }}>r</span> Feedback
        </div>
      </div>

      <TerminalComposerAndStatus
        modeLabel="default"
        modeColor={TERM.modeDefault}
        rightStats=" · 28.1k/200.0k (14%) · $0.02 · ↑11.2k ↓0.6k · Claude Sonnet 5.5 · high"
      />
    </>
  );
}

interface DiffRowData {
  gutter: string;
  code: string;
  kind: 'hunk' | 'ctx' | 'del' | 'add';
}

const SAMPLE_DIFF_ROWS: DiffRowData[] = [
  { gutter: '            ', code: '@@ -41,5 +41,7 @@ func (l *IPLimiter) Allow(ip netip.Addr) bool {', kind: 'hunk' },
  { gutter: '   41   41  ', code: '    l.mu.Lock()', kind: 'ctx' },
  { gutter: '   42   42  ', code: '    defer l.mu.Unlock()', kind: 'ctx' },
  { gutter: '   43      -', code: '    b, ok := l.buckets[ip]', kind: 'del' },
  { gutter: '        43 +', code: '    if l.lru.Len() >= l.maxEntries {', kind: 'add' },
  { gutter: '        44 +', code: '        l.lru.RemoveOldest()', kind: 'add' },
  { gutter: '        45 +', code: '    }', kind: 'add' },
  { gutter: '        46 +', code: '    b, ok := l.lru.Get(ip)', kind: 'add' },
];

function TimelineScenarioBody() {
  return (
    <>
      {/* Authentic changeDiffViewer + DiffView (internal/cli/command/changes.go & components/changes.go) */}
      <div
        style={{
          borderTop: `1px solid ${TERM.borderDefault}`,
          paddingTop: '0.55rem',
          marginBottom: '0.85rem',
        }}
      >
        <div style={{ color: TERM.fgPrimary, fontWeight: 700, marginBottom: '0.45rem' }}>
          Diff for internal/http/ratelimit.go (modified):
        </div>

        <div
          style={{
            display: 'flex',
            flexDirection: 'column',
            borderRadius: '4px',
            overflow: 'hidden',
            marginBottom: '0.45rem',
          }}
        >
          {SAMPLE_DIFF_ROWS.map((row, idx) => {
            const bg =
              row.kind === 'hunk'
                ? TERM.diffHunkBg
                : row.kind === 'del'
                  ? TERM.diffRemovedBg
                  : row.kind === 'add'
                    ? TERM.diffAddedBg
                    : 'transparent';
            const fg =
              row.kind === 'hunk'
                ? TERM.diffHunk
                : row.kind === 'del'
                  ? TERM.diffRemoved
                  : row.kind === 'add'
                    ? TERM.diffAdded
                    : TERM.fgPrimary;
            return (
              <div
                key={idx}
                style={{
                  display: 'flex',
                  background: bg,
                  whiteSpace: 'pre',
                  lineHeight: 1.45,
                }}
              >
                <span style={{ color: TERM.fgMuted, userSelect: 'none' }}>{row.gutter}</span>
                <span style={{ color: TERM.borderDefault, userSelect: 'none' }}>│</span>
                <span style={{ color: fg, paddingLeft: '0.4rem' }}>{row.code}</span>
              </div>
            );
          })}
        </div>

        <div style={{ fontSize: '0.76rem', color: TERM.fgMuted }}>
          <span style={{ color: TERM.fgSecondary, fontWeight: 600 }}>esc</span> Return to changes
        </div>
      </div>

      {/* Authentic CommandOutput for /revert (internal/cli/components/notice.go) */}
      <div style={{ marginBottom: '0.85rem', lineHeight: 1.48 }}>
        <div>
          <span style={{ color: TERM.fgPrimary, fontWeight: 700 }}>{'> '}</span>
          <span style={{ color: TERM.fgPrimary }}>/revert internal/http/ratelimit.go</span>
        </div>
        <div style={{ color: TERM.fgSecondary, whiteSpace: 'pre' }}>
          {'  ⎿  Changes reverted.'}
        </div>
      </div>

      <TerminalComposerAndStatus
        modeLabel="accept_edits"
        modeColor={TERM.diffAdded}
        rightStats=" · 42.8k/200.0k (21%) · $0.04 · ↑18.4k ↓1.2k · Claude Sonnet 5.5 · high"
      />
    </>
  );
}

function TerminalShowcase() {
  const [tabIdx, setTabIdx] = useState(0);
  const active = TOUR_SCENARIOS[tabIdx];

  return (
    <div>
      {/* Scenario Selector Tabs */}
      <div className="ts-tour-tabs" role="tablist" aria-label="TaskSmith Interactive Terminal Scenarios">
        {TOUR_SCENARIOS.map((item, idx) => (
          <button
            key={item.id}
            type="button"
            role="tab"
            aria-selected={idx === tabIdx}
            data-active={idx === tabIdx}
            className="ts-tour-tab"
            onClick={() => setTabIdx(idx)}
          >
            <span className="ts-tour-num">{idx + 1}</span>
            <span>{item.label}</span>
          </button>
        ))}
      </div>

      {/* Terminal Window */}
      <div
        style={{
          borderRadius: '12px',
          background: TERM.bgCanvas,
          border: `1px solid ${TERM.borderDefault}`,
          boxShadow: '0 24px 54px rgba(0, 0, 0, 0.34)',
          overflow: 'hidden',
          fontFamily: MONO_STACK,
          fontSize: '0.84rem',
          lineHeight: 1.5,
          color: TERM.fgPrimary,
          textAlign: 'left',
        }}
      >
        {/* Native Terminal Window Bar */}
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: '64px 1fr 64px',
            alignItems: 'center',
            padding: '0.6rem 0.95rem',
            background: TERM.bgPanel,
            borderBottom: `1px solid ${TERM.borderSubtle}`,
            fontSize: '0.76rem',
            color: TERM.fgMuted,
          }}
        >
          <div style={{ display: 'flex', gap: '0.42rem' }}>
            <span style={{ width: 11, height: 11, borderRadius: '50%', background: '#ff5f5f' }} />
            <span style={{ width: 11, height: 11, borderRadius: '50%', background: '#e3b341' }} />
            <span style={{ width: 11, height: 11, borderRadius: '50%', background: '#3fb950' }} />
          </div>
          <span style={{ textAlign: 'center', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
            {active.windowTitle}
          </span>
          <span style={{ textAlign: 'right', fontSize: '0.7rem', color: '#6e7681' }}>zsh</span>
        </div>

        {/* Inline Reactive REPL Canvas */}
        <div
          style={{
            padding: '1.15rem 1.35rem 0.95rem',
            overflowX: 'auto',
          }}
        >
          <div style={{ minWidth: '640px', display: 'flex', flexDirection: 'column' }}>
            {tabIdx === 0 && <AgentScenarioBody />}
            {tabIdx === 1 && <SandboxScenarioBody />}
            {tabIdx === 2 && <TimelineScenarioBody />}
          </div>
        </div>
      </div>

      {/* Caption & Deep Link */}
      <div className="ts-tour-caption">
        <span>{active.caption}</span>
        <a href={withBase(active.linkHref)} className="ts-inline-link">
          {active.linkText}
        </a>
      </div>
    </div>
  );
}

function WorkspaceManifestCard() {
  const k = { color: '#79c0ff' };
  const v = { color: '#a5d6ff' };
  const c = { color: '#8b949e' };
  const p = { color: '#e6edf3' };
  const h = { color: '#56d4dd', fontWeight: 700 };

  return (
    <div className="ts-code-card">
      <div className="ts-code-header" style={{ fontFamily: MONO_STACK }}>
        <span>&lt;workspace-root&gt;/WORKSPACE.md</span>
        <span style={{ color: '#56d4dd', fontWeight: 600 }}>warp/v1alpha1</span>
      </div>
      <pre
        style={{
          margin: 0,
          padding: '1.05rem 1.25rem',
          overflowX: 'auto',
          background: 'transparent',
          fontFamily: MONO_STACK,
          fontSize: '0.81rem',
          lineHeight: 1.56,
          color: TERM.fgPrimary,
        }}
      >
        <code>
          <span style={c}>{'---\n'}</span>
          <span style={k}>apiVersion</span>
          <span style={p}>{': '}</span>
          <span style={v}>{'warp/v1alpha1\n'}</span>
          <span style={k}>kind</span>
          <span style={p}>{': '}</span>
          <span style={v}>{'Workspace\n'}</span>
          <span style={k}>metadata</span>
          <span style={p}>{':\n  '}</span>
          <span style={k}>name</span>
          <span style={p}>{': '}</span>
          <span style={v}>{'api-platform\n'}</span>
          <span style={k}>spec</span>
          <span style={p}>{':\n  '}</span>
          <span style={c}>{'# Route specialized models per cognitive role\n  '}</span>
          <span style={k}>defaultModels</span>
          <span style={p}>{':\n    '}</span>
          <span style={k}>completion</span>
          <span style={p}>{': '}</span>
          <span style={v}>{'anthropic/claude-sonnet-5-5\n    '}</span>
          <span style={k}>decision</span>
          <span style={p}>{':   '}</span>
          <span style={v}>{'typesafe/jev-latest\n    '}</span>
          <span style={k}>embedding</span>
          <span style={p}>{':  '}</span>
          <span style={v}>{'openai/text-embedding-3-small\n  '}</span>
          <span style={k}>policies</span>
          <span style={p}>{':\n    '}</span>
          <span style={k}>tools</span>
          <span style={p}>{':\n      '}</span>
          <span style={k}>dangerous</span>
          <span style={p}>{': '}</span>
          <span style={v}>{'ask\n      '}</span>
          <span style={k}>exclude</span>
          <span style={p}>{':\n        - '}</span>
          <span style={v}>{'"bash(rm -rf /*)"\n'}</span>
          <span style={c}>{'---\n\n'}</span>
          <span style={h}>{'# Workspace Guidelines\n'}</span>
          <span style={p}>
            {'Always run package tests before completing refactors.\nWrap sentinel errors with %w.'}
          </span>
        </code>
      </pre>
    </div>
  );
}

export default function HomePage() {
  const [methodIdx, setMethodIdx] = useState(0);
  const [copied, setCopied] = useState(false);

  const activeInstall = INSTALL_METHODS[methodIdx];

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(activeInstall.lines.join('\n'));
      setCopied(true);
      setTimeout(() => setCopied(false), 1800);
    } catch {
      // Ignore clipboard failures on non-HTTPS contexts
    }
  };

  return (
    <main className="ts-home">
      <style>{HOME_CSS}</style>

      {/* Hero Section */}
      <section className="ts-hero">
        <img
          src={withBase('/tasksmith-logo.svg')}
          alt="TaskSmith Anvil Logo"
          width={112}
          height={75}
          className="ts-logo"
        />

        <h1 className="ts-hero-title">
          Autonomous coding in your terminal,{' '}
          <span className="ts-hero-gradient">sandboxed at the kernel.</span>
        </h1>

        <p className="ts-hero-subtitle">
          TaskSmith pairs a persistent Unix-socket <strong style={{ color: 'var(--rp-c-text-1)' }}>Brain Daemon</strong>{' '}
          with <strong style={{ color: 'var(--rp-c-text-1)' }}>OS-level Seatbelt &amp; Bubblewrap isolation</strong>,{' '}
          <strong style={{ color: 'var(--rp-c-text-1)' }}>triple-role model routing</strong>, and{' '}
          <strong style={{ color: 'var(--rp-c-text-1)' }}>declarative WARP workspaces</strong>.
        </p>

        {/* Fixed-Width Install Switcher */}
        <div className="ts-install-card">
          <div className="ts-install-header">
            <div className="ts-install-tabs" role="tablist" aria-label="Installation method">
              {INSTALL_METHODS.map((item, idx) => (
                <button
                  key={item.id}
                  type="button"
                  role="tab"
                  aria-selected={idx === methodIdx}
                  data-active={idx === methodIdx}
                  className="ts-install-tab"
                  onClick={() => {
                    setMethodIdx(idx);
                    setCopied(false);
                  }}
                >
                  {item.label}
                </button>
              ))}
            </div>
            <span className="ts-install-meta" style={{ fontFamily: MONO_STACK }}>
              {activeInstall.meta}
            </span>
          </div>

          <div className="ts-install-body" style={{ fontFamily: MONO_STACK }}>
            <div className="ts-install-lines">
              {activeInstall.lines.map((line) => (
                <div key={line} style={{ display: 'flex', gap: '0.55rem', whiteSpace: 'nowrap' }}>
                  <span style={{ color: '#1abc9c', userSelect: 'none' }}>$</span>
                  <code style={{ color: '#e6edf3' }}>{line}</code>
                </div>
              ))}
            </div>
            <button
              type="button"
              onClick={handleCopy}
              data-copied={copied}
              className="ts-copy-btn"
            >
              {copied ? '✓ Copied' : 'Copy'}
            </button>
          </div>
        </div>

        {/* Action Buttons */}
        <div className="ts-cta-row">
          <a href={withBase('/guides/cli')} className="ts-btn-primary">
            <span>Get Started</span>
            <span aria-hidden="true">→</span>
          </a>
          <a href={withBase('/architecture/daemon')} className="ts-btn-secondary">
            Architecture
          </a>
          <a href={withBase('/reference/manifests')} className="ts-btn-secondary">
            Manifest Reference
          </a>
          <a
            href="https://github.com/masterkeysrd/tasksmith"
            target="_blank"
            rel="noreferrer"
            className="ts-btn-secondary"
          >
            GitHub
          </a>
        </div>
      </section>

      {/* Interactive 3-Tab REPL Tour */}
      <section className="ts-tour-section">
        <TerminalShowcase />
      </section>

      {/* Multi-Provider Gateway Strip */}
      <section className="ts-provider-strip" aria-label="Supported Model Providers">
        <div className="ts-provider-header">
          <span className="ts-eyebrow">
            Vendor-Agnostic Gateway · Completion · Decision · Embedding
          </span>
          <a href={withBase('/reference/providers')} className="ts-inline-link" style={{ fontSize: '0.82rem' }}>
            Compare all providers →
          </a>
        </div>
        <div className="ts-provider-grid">
          {PROVIDERS.map((prov) => (
            <a key={prov.name} href={withBase(prov.href)} className="ts-provider-chip">
              <span className="ts-provider-name">{prov.name}</span>
              <span className="ts-provider-role">{prov.detail}</span>
            </a>
          ))}
        </div>
      </section>

      {/* Declarative by Design Split Showcase */}
      <section className="ts-split-section">
        <div>
          <span className="ts-eyebrow">Declarative by Design</span>
          <h2 className="ts-section-title">
            Check your AI workflow into Git—not into a proprietary dashboard.
          </h2>
          <p className="ts-section-desc">
            Anchor any repository with <code style={{ fontFamily: MONO_STACK }}>WORKSPACE.md</code> and{' '}
            <code style={{ fontFamily: MONO_STACK }}>.agents/</code> manifests. Route frontier reasoning, sub-50ms
            System 1 decisions, and vector embeddings independently while keeping{' '}
            <code style={{ fontFamily: MONO_STACK }}>.env</code> secrets masked from child shells.
          </p>

          <div className="ts-tier-list">
            {WORKSPACE_TIERS.map((item) => (
              <div key={item.tier} className="ts-tier-item">
                <span className="ts-tier-badge">{item.tier}</span>
                <div>
                  <div
                    style={{
                      fontFamily: MONO_STACK,
                      fontSize: '0.78rem',
                      color: 'var(--rp-c-text-1)',
                      marginBottom: '0.1rem',
                    }}
                  >
                    {item.path}
                  </div>
                  <div style={{ fontSize: '0.78rem', color: 'var(--rp-c-text-2)' }}>{item.note}</div>
                </div>
              </div>
            ))}
          </div>

          <div style={{ display: 'flex', flexWrap: 'wrap', gap: '1.15rem', fontSize: '0.88rem' }}>
            <a href={withBase('/guides/workspaces')} className="ts-inline-link">
              Workspaces &amp; Onboarding Guide →
            </a>
            <a href={withBase('/reference/manifests')} className="ts-inline-link">
              WARP Manifest Schemas →
            </a>
          </div>
        </div>

        <WorkspaceManifestCard />
      </section>

      {/* Feature Grid */}
      <section>
        <div className="ts-features-header">
          <span className="ts-eyebrow">Architecture &amp; Capabilities</span>
          <h2 className="ts-section-title" style={{ marginBottom: '0.5rem' }}>
            Engineered from the socket up
          </h2>
          <p style={{ margin: 0, fontSize: '0.96rem', color: 'var(--rp-c-text-2)', lineHeight: 1.6 }}>
            Every subsystem is built for deterministic control, defense-in-depth isolation, and zero vendor lock-in.
          </p>
        </div>

        <div className="ts-feature-grid">
          {FEATURES.map((item) => (
            <a key={item.title} href={withBase(item.href)} className="ts-feature-card">
              <span
                style={{
                  alignSelf: 'flex-start',
                  fontSize: '0.73rem',
                  fontWeight: 700,
                  letterSpacing: '0.05em',
                  textTransform: 'uppercase',
                  color: item.accent,
                }}
              >
                {item.badge}
              </span>
              <h3
                style={{
                  margin: 0,
                  fontSize: '1.1rem',
                  fontWeight: 700,
                  color: 'var(--rp-c-text-1)',
                }}
              >
                {item.title}
              </h3>
              <p
                style={{
                  margin: 0,
                  fontSize: '0.9rem',
                  lineHeight: 1.56,
                  color: 'var(--rp-c-text-2)',
                }}
              >
                {item.description}
              </p>
              <span className="ts-feature-more">
                <span>Explore</span>
                <span aria-hidden="true">→</span>
              </span>
            </a>
          ))}
        </div>
      </section>

      {/* Bottom Quick-Start Banner */}
      <section className="ts-bottom-banner">
        <div>
          <h2
            style={{
              margin: '0 0 0.35rem',
              fontSize: '1.3rem',
              fontWeight: 800,
              color: 'var(--rp-c-text-1)',
            }}
          >
            Ready to smith in your repository?
          </h2>
          <p style={{ margin: 0, fontSize: '0.92rem', color: 'var(--rp-c-text-2)' }}>
            Run <code style={{ fontFamily: MONO_STACK }}>tasksmith</code> inside any project directory and launch{' '}
            <code style={{ fontFamily: MONO_STACK }}>/setup</code> to configure providers and sandbox rules.
          </p>
        </div>
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: '0.65rem' }}>
          <a href={withBase('/guides/cli')} className="ts-btn-primary">
            <span>Quickstart Guide</span>
            <span aria-hidden="true">→</span>
          </a>
          <a href={withBase('/roadmap')} className="ts-btn-secondary">
            View Roadmap
          </a>
        </div>
      </section>
    </main>
  );
}
