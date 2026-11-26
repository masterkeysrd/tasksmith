import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { defineConfig } from '@rspress/core';

const __dirname = path.dirname(fileURLToPath(import.meta.url));

function normalizeBase(raw?: string): string {
  if (!raw) return '/';
  const trimmed = raw.trim();
  if (!trimmed || trimmed === '/') return '/';
  const withLeading = trimmed.startsWith('/') ? trimmed : `/${trimmed}`;
  return withLeading.endsWith('/') ? withLeading : `${withLeading}/`;
}

const GUIDES_SIDEBAR = [
  {
    text: 'Get Started',
    items: [
      { text: 'Overview', link: '/overview' },
      { text: 'Quickstart', link: '/guides/quickstart' },
      { text: 'Workspaces & Projects', link: '/guides/workspaces' },
    ],
  },
  {
    text: 'Core Workflows',
    items: [
      { text: 'Interactive Terminal (TUI)', link: '/guides/cli' },
      { text: 'Sessions, Diffs & Usage', link: '/guides/sessions' },
      { text: 'Modes & Planning', link: '/guides/modes-and-planning' },
      { text: 'Connect an Editor (ACP)', link: '/guides/acp' },
      { text: 'Headless & CI Automation', link: '/guides/headless-exec' },
    ],
  },
  {
    text: 'Security & Governance',
    items: [
      { text: 'Workspace Trust', link: '/guides/trust' },
      { text: 'Permissions & Rules', link: '/guides/permissions' },
      { text: 'Auto-Mode Permission Review', link: '/guides/auto-permission-review' },
      { text: 'Sandboxing', link: '/guides/sandbox' },
    ],
  },
  {
    text: 'Customize & Extend',
    items: [
      { text: 'Customization Overview', link: '/guides/customizations' },
      { text: 'Models & Providers', link: '/guides/models-and-providers' },
      { text: 'Agents & Subagents', link: '/guides/agents-and-subagents' },
      { text: 'Skills & Rules', link: '/guides/skills-and-rules' },
      { text: 'Slash Commands & Workflows', link: '/guides/commands' },
      { text: 'Tools & MCP Servers', link: '/guides/tools-and-mcp' },
      { text: 'Appearance & Status Line', link: '/guides/appearance' },
    ],
  },
];

export default defineConfig({
  root: path.join(__dirname, '../docs'),
  themeDir: path.join(__dirname, 'theme'),
  base: normalizeBase(process.env.DOCS_BASE),
  ...(process.env.DOCS_ORIGIN ? { siteOrigin: process.env.DOCS_ORIGIN } : {}),
  title: 'TaskSmith',
  description:
    'Provider-agnostic coding agents. Deterministic environments. Run version-controlled agents, skills, rules, and kernel-sandboxed workflows across any model provider—from your terminal, editor, or CI.',
  logo: '/tasksmith-logo.svg',
  logoText: 'TaskSmith',
  llms: true,
  route: {
    exclude: ['README.md'],
  },
  builderConfig: {
    html: {
      favicon: path.join(__dirname, '../assets/tasksmith-logo.svg'),
    },
    server: {
      publicDir: {
        name: path.join(__dirname, '../assets'),
      },
    },
  },
  plugins: [
    {
      name: 'tasksmith-pages',
      addPages() {
        return [
          {
            routePath: '/',
            filepath: path.join(__dirname, 'pages/index.tsx'),
          },
          {
            routePath: '/overview',
            filepath: path.join(__dirname, '../docs/README.md'),
          },
          {
            routePath: '/roadmap',
            filepath: path.join(__dirname, '../ROADMAP.md'),
          },
        ];
      },
    },
  ],
  themeConfig: {
    socialLinks: [
      {
        icon: 'github',
        mode: 'link',
        content: 'https://github.com/masterkeysrd/tasksmith',
      },
    ],
    footer: {
      message: 'Released under the Apache 2.0 License.',
    },
    nav: [
      {
        text: 'Guides',
        link: '/guides/quickstart',
        activeMatch: '^/(guides|overview)',
      },
      {
        text: 'Architecture',
        link: '/architecture/daemon',
        activeMatch: '^/architecture/',
      },
      {
        text: 'Reference',
        link: '/reference/cli',
        activeMatch: '^/reference/',
      },
      {
        text: 'Roadmap',
        link: '/roadmap',
        activeMatch: '^/roadmap',
      },
      {
        text: 'Releases',
        link: 'https://github.com/masterkeysrd/tasksmith/releases',
      },
    ],
    sidebar: {
      '/overview': GUIDES_SIDEBAR,
      '/guides/': GUIDES_SIDEBAR,
      '/architecture/': [
        {
          text: 'System Architecture',
          items: [
            { text: 'Daemon Architecture', link: '/architecture/daemon' },
            { text: 'Agent Execution Harness', link: '/architecture/harness' },
            { text: 'Provider Gateway', link: '/architecture/gateway' },
            { text: 'Virtualized Sandbox', link: '/architecture/sandbox' },
            { text: 'Permission & Guard Engine', link: '/architecture/permissions' },
          ],
        },
      ],
      '/reference/': [
        {
          text: 'CLI & Daemon',
          items: [
            { text: 'CLI Overview', link: '/reference/cli' },
            { text: 'Flags & Subcommands', link: '/reference/cli/flags-and-subcommands' },
            { text: 'Slash Commands', link: '/reference/cli/slash-commands' },
            { text: 'Keyboard Shortcuts', link: '/reference/cli/keybindings' },
            { text: 'Config Files & State', link: '/reference/cli/config-files' },
            { text: 'Headless Execution (exec)', link: '/reference/exec' },
            { text: 'Daemon HTTP API', link: '/reference/daemon-api' },
          ],
        },
        {
          text: 'Built-in Tools',
          items: [
            { text: 'Tools Overview', link: '/reference/tools' },
            { text: 'Filesystem Tools', link: '/reference/tools/filesystem' },
            { text: 'Execution & Tasks', link: '/reference/tools/execution-and-tasks' },
            { text: 'Web & Network', link: '/reference/tools/web' },
            { text: 'Agents, Planning & Input', link: '/reference/tools/agents-and-planning' },
            { text: 'Workspace Resources', link: '/reference/tools/workspace-resources' },
          ],
        },
        {
          text: 'Resource Manifests (WARP)',
          items: [
            { text: 'Manifests Overview', link: '/reference/manifests' },
            { text: 'Workspace & Context', link: '/reference/manifests/workspace' },
            { text: 'Agent', link: '/reference/manifests/agent' },
            { text: 'Skill', link: '/reference/manifests/skill' },
            { text: 'Rule', link: '/reference/manifests/rule' },
            { text: 'Command', link: '/reference/manifests/command' },
            { text: 'Decision', link: '/reference/manifests/decision' },
            { text: 'Tool', link: '/reference/manifests/tool' },
            { text: 'MCP Server', link: '/reference/manifests/mcp' },
            { text: 'Plugin', link: '/reference/manifests/plugin' },
          ],
        },
        {
          text: 'Model Providers',
          items: [
            { text: 'Providers Overview & Schema', link: '/reference/providers' },
            { text: 'OpenAI', link: '/reference/providers/openai' },
            { text: 'Codex', link: '/reference/providers/codex' },
            { text: 'Anthropic', link: '/reference/providers/anthropic' },
            { text: 'Claude Code', link: '/reference/providers/claudecode' },
            { text: 'GitHub Copilot', link: '/reference/providers/github-copilot' },
            { text: 'Google GenAI', link: '/reference/providers/google-genai' },
            { text: 'Ollama', link: '/reference/providers/ollama' },
            { text: 'TypeSafe', link: '/reference/providers/typesafe' },
            { text: 'Antigravity', link: '/reference/providers/antigravity' },
          ],
        },
      ],
    },
  },
});
