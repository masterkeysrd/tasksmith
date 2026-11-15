import { useState } from 'react';
import { withBase } from '@rspress/core/runtime';

export const frontmatter = {
  pageType: 'custom',
  title: 'TaskSmith — Daemon-Backed AI Coding Agent for the Terminal',
};

const INSTALL_CMD = 'go install github.com/masterkeysrd/tasksmith/cmd/tasksmith@latest';

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
      'On-demand headless daemon over Unix domain sockets with multiplexed SSE token streaming, SQLite turn journaling, and seamless session resumption.',
    href: '/architecture/daemon',
    accent: '#2ac3de',
  },
  {
    badge: 'Seatbelt & Bubblewrap',
    title: 'OS Kernel Sandboxing & Shell AST',
    description:
      'Per-session native kernel isolation, host credential masking, capability confinement, and shellguard AST inspection before any command executes.',
    href: '/architecture/sandbox',
    accent: '#1abc9c',
  },
  {
    badge: 'Multi-Vendor',
    title: 'Triple-Role Model Gateway',
    description:
      'Decouple agents from vendors across completion, decision, and embedding roles with OpenAI, Anthropic, Google GenAI, Ollama, TypeSafe, and Antigravity.',
    href: '/reference/providers',
    accent: '#ff5f5f',
  },
  {
    badge: 'Snapshots',
    title: 'Transactional Timeline & Revert',
    description:
      'Automatic pre-mutation file snapshots before edit, write, or bash runs. Inspect unified diffs with /changes and roll back turns with /revert.',
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
      'Delegate work to isolated subagents with git worktrees, discover large MCP and custom tool catalogs on demand via tool_search, and enforce strict policies.',
    href: '/guides/tools-and-mcp',
    accent: '#ff5f5f',
  },
];

// Palette matching internal/ui/theme/builtin/dark.json & internal/cli/components/logo.go
const TERM = {
  bgCanvas: '#0d1117',
  bgPanel: '#161b22',
  fgPrimary: '#e6edf3',
  fgSecondary: '#b1bac4',
  fgMuted: '#9198a1',
  brand: '#9d7cd8',
  iconPrimary: '#56d4dd',
  accent: '#58a6ff',
  success: '#3fb950',
  diffAdded: '#7ee787',
  diffRemoved: '#ff7b72',
  borderFocus: '#58a6ff',
  logoTop: '#2ac3de',
  logoCenter: '#1abc9c',
  logoBase: '#ff5f5f',
} as const;

const MONO_STACK =
  'var(--rp-font-family-mono, ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace)';

function TerminalShowcase() {
  return (
    <div
      style={{
        borderRadius: '12px',
        background: TERM.bgCanvas,
        border: '1px solid #30363d',
        boxShadow: '0 22px 50px rgba(0, 0, 0, 0.32)',
        overflow: 'hidden',
        fontFamily: MONO_STACK,
        fontSize: '0.84rem',
        lineHeight: 1.5,
        color: TERM.fgPrimary,
        textAlign: 'left',
      }}
    >
      {/* Native Terminal Window Bar (title reflects generated session title per terminal_status.go) */}
      <div
        style={{
          display: 'grid',
          gridTemplateColumns: '64px 1fr 64px',
          alignItems: 'center',
          padding: '0.6rem 0.95rem',
          background: TERM.bgPanel,
          borderBottom: '1px solid #21262d',
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
          Refactor rate-limiter middleware
        </span>
        <span />
      </div>

      {/* Inline Reactive REPL Canvas */}
      <div
        style={{
          padding: '1.15rem 1.35rem 0.95rem',
          overflowX: 'auto',
        }}
      >
        <div style={{ minWidth: '640px', display: 'flex', flexDirection: 'column' }}>
          {/* Header Banner (Header + Logo in internal/cli/components/header.go & logo.go) */}
          <div
            style={{
              display: 'flex',
              flexDirection: 'row',
              gap: '1.5rem',
              marginBottom: '1.15rem',
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
              <span style={{ color: TERM.logoTop }}>{' ◥██████████◣\n'}</span>
              <span style={{ color: TERM.logoCenter }}>{'     ▐██▌    \n'}</span>
              <span style={{ color: TERM.logoBase }}>{'    ◢████◣   '}</span>
            </pre>
            <div style={{ display: 'flex', flexDirection: 'column', justifyContent: 'center', lineHeight: 1.38 }}>
              <div>
                <span style={{ color: TERM.brand, fontWeight: 700 }}>TaskSmith</span>
                <span style={{ color: TERM.fgSecondary }}> v0.0.3</span>
              </div>
              <div style={{ color: TERM.fgSecondary }}>
                anthropic/claude-sonnet-5-5 with high effort
              </div>
              <div style={{ color: TERM.fgSecondary }}>
                ~/projects/api-service · native sandbox
              </div>
            </div>
          </div>

          {/* User Message (UserMessage in internal/cli/components/message.go) */}
          <div
            style={{
              color: TERM.iconPrimary,
              fontWeight: 700,
              marginBottom: '0.85rem',
              whiteSpace: 'pre-wrap',
            }}
          >
            {'&gt; '.replace('&gt;', '>')}Refactor @ratelimit.go to bound per-IP buckets and run the HTTP test suite
          </div>

          {/* Assistant Turn Parts (MessageThought + InvokeAgent + Edit + Bash + Markdown) */}
          <div style={{ display: 'flex', flexDirection: 'column', gap: '0.2rem', marginBottom: '0.75rem' }}>
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

          {/* Assistant Final Text */}
          <div style={{ color: TERM.fgPrimary, marginBottom: '0.75rem', lineHeight: 1.55 }}>
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

          {/* Turn Summary Notice (FormatTurnSummary in internal/cli/components/turn_summary.go) */}
          <div style={{ color: TERM.fgSecondary, marginBottom: '0.85rem' }}>
            Smithed for 14s · ↑18.4k ↓1.2k · $0.04 · done 4:18 PM
          </div>

          {/* Composer (top & bottom horizontal focus rules only, per composer.go) */}
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              padding: '0.38rem 0',
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
                height: '1.05rem',
                background: TERM.fgPrimary,
                opacity: 0.85,
              }}
            />
          </div>

          {/* DefaultStatusLine (hint on left; mode · context · usage · model on right) */}
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
              <span style={{ color: TERM.diffAdded, fontWeight: 700 }}>accept_edits</span>
              <span> · 42.8k/200.0k (21%) · $0.04 · ↑18.4k ↓1.2k · Claude Sonnet 5.5 · high</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

export default function HomePage() {
  const [copied, setCopied] = useState(false);

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(INSTALL_CMD);
      setCopied(true);
      setTimeout(() => setCopied(false), 1800);
    } catch {
      // Ignore clipboard failures on non-HTTPS contexts
    }
  };

  return (
    <main
      style={{
        maxWidth: '1120px',
        margin: '0 auto',
        padding: '3.5rem 1.5rem 5rem',
      }}
    >
      {/* Hero Section */}
      <section
        style={{
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
          textAlign: 'center',
          gap: '1.25rem',
          paddingBottom: '3.5rem',
        }}
      >
        <img
          src={withBase('/tasksmith-logo.svg')}
          alt="TaskSmith Anvil Logo"
          width={116}
          height={78}
          style={{
            borderRadius: '16px',
            boxShadow: '0 12px 32px rgba(0, 0, 0, 0.22)',
          }}
        />

        <h1
          style={{
            fontSize: 'clamp(2.4rem, 5vw, 3.6rem)',
            fontWeight: 800,
            letterSpacing: '-0.03em',
            lineHeight: 1.1,
            margin: 0,
            color: 'var(--rp-c-text-1)',
          }}
        >
          TaskSmith
        </h1>

        <p
          style={{
            maxWidth: '720px',
            fontSize: 'clamp(1.05rem, 2vw, 1.25rem)',
            lineHeight: 1.6,
            margin: 0,
            color: 'var(--rp-c-text-2)',
          }}
        >
          An extensible, daemon-backed AI coding agent for the terminal with{' '}
          <strong style={{ color: 'var(--rp-c-text-1)' }}>OS-level sandboxing</strong>,{' '}
          <strong style={{ color: 'var(--rp-c-text-1)' }}>multi-provider routing</strong>, and{' '}
          <strong style={{ color: 'var(--rp-c-text-1)' }}>declarative workspaces</strong>.
        </p>

        {/* Install Command Pill */}
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: '0.75rem',
            maxWidth: '100%',
            padding: '0.65rem 0.9rem 0.65rem 1.1rem',
            borderRadius: '10px',
            background: '#16161e',
            border: '1px solid rgba(42, 195, 222, 0.28)',
            color: '#e6edf3',
            fontFamily: MONO_STACK,
            fontSize: '0.88rem',
            overflowX: 'auto',
          }}
        >
          <span style={{ color: '#1abc9c', userSelect: 'none' }}>$</span>
          <code style={{ whiteSpace: 'nowrap', color: '#e6edf3' }}>{INSTALL_CMD}</code>
          <button
            type="button"
            onClick={handleCopy}
            style={{
              cursor: 'pointer',
              border: '1px solid rgba(255, 255, 255, 0.16)',
              background: copied ? 'rgba(26, 188, 156, 0.2)' : 'rgba(255, 255, 255, 0.06)',
              color: copied ? '#1abc9c' : '#cbd5e1',
              borderRadius: '6px',
              padding: '0.28rem 0.6rem',
              fontSize: '0.78rem',
              fontWeight: 600,
              flexShrink: 0,
            }}
          >
            {copied ? 'Copied' : 'Copy'}
          </button>
        </div>

        {/* Action Buttons */}
        <div
          style={{
            display: 'flex',
            flexWrap: 'wrap',
            justifyContent: 'center',
            gap: '0.85rem',
            marginTop: '0.5rem',
          }}
        >
          <a
            href={withBase('/guides/cli')}
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              padding: '0.7rem 1.4rem',
              borderRadius: '8px',
              background: '#2ac3de',
              color: '#0f1419',
              fontWeight: 700,
              fontSize: '0.95rem',
              textDecoration: 'none',
            }}
          >
            Get Started
          </a>
          <a
            href={withBase('/architecture/daemon')}
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              padding: '0.7rem 1.4rem',
              borderRadius: '8px',
              border: '1px solid var(--rp-c-divider)',
              background: 'var(--rp-c-bg-soft)',
              color: 'var(--rp-c-text-1)',
              fontWeight: 600,
              fontSize: '0.95rem',
              textDecoration: 'none',
            }}
          >
            Architecture
          </a>
          <a
            href={withBase('/roadmap')}
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              padding: '0.7rem 1.4rem',
              borderRadius: '8px',
              border: '1px solid var(--rp-c-divider)',
              background: 'var(--rp-c-bg-soft)',
              color: 'var(--rp-c-text-1)',
              fontWeight: 600,
              fontSize: '0.95rem',
              textDecoration: 'none',
            }}
          >
            Roadmap
          </a>
        </div>
      </section>

      {/* Authentic TaskSmith REPL Preview */}
      <section style={{ marginBottom: '4rem' }}>
        <TerminalShowcase />
      </section>

      {/* Feature Grid */}
      <section
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fit, minmax(300px, 1fr))',
          gap: '1.25rem',
        }}
      >
        {FEATURES.map((item) => (
          <a
            key={item.title}
            href={withBase(item.href)}
            style={{
              display: 'flex',
              flexDirection: 'column',
              gap: '0.6rem',
              padding: '1.4rem',
              borderRadius: '12px',
              border: '1px solid var(--rp-c-divider)',
              background: 'var(--rp-c-bg-soft)',
              textDecoration: 'none',
              color: 'inherit',
            }}
          >
            <span
              style={{
                alignSelf: 'flex-start',
                fontSize: '0.74rem',
                fontWeight: 700,
                letterSpacing: '0.04em',
                textTransform: 'uppercase',
                color: item.accent,
              }}
            >
              {item.badge}
            </span>
            <h2
              style={{
                margin: 0,
                fontSize: '1.12rem',
                fontWeight: 700,
                color: 'var(--rp-c-text-1)',
              }}
            >
              {item.title}
            </h2>
            <p
              style={{
                margin: 0,
                fontSize: '0.92rem',
                lineHeight: 1.55,
                color: 'var(--rp-c-text-2)',
              }}
            >
              {item.description}
            </p>
          </a>
        ))}
      </section>
    </main>
  );
}
