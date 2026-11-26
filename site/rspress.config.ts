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

export default defineConfig({
  root: path.join(__dirname, '../docs'),
  themeDir: path.join(__dirname, 'theme'),
  base: normalizeBase(process.env.DOCS_BASE),
  ...(process.env.DOCS_ORIGIN ? { siteOrigin: process.env.DOCS_ORIGIN } : {}),
  title: 'TaskSmith',
  description:
    'An extensible, daemon-backed AI coding agent for the terminal with OS-level sandboxing, multi-provider routing, and declarative workspaces.',
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
        link: '/guides/workspaces',
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
      '/overview': [
        {
          text: 'Documentation',
          items: [
            { text: 'Overview', link: '/overview' },
          ],
        },
        {
          text: 'User Guides',
          items: [
            { text: 'Workspaces & Projects', link: '/guides/workspaces' },
            { text: 'Editor Integration (ACP)', link: '/guides/acp' },
            { text: 'CLI & Interactive Workflow', link: '/guides/cli' },
            { text: 'Security & Permissions', link: '/guides/permissions' },
            { text: 'Customizations & Extensions', link: '/guides/customizations' },
            { text: 'Tools & MCP Servers', link: '/guides/tools-and-mcp' },
          ],
        },
      ],
      '/guides/': [
        {
          text: 'Documentation',
          items: [
            { text: 'Overview', link: '/overview' },
          ],
        },
        {
          text: 'User Guides',
          items: [
            { text: 'Workspaces & Projects', link: '/guides/workspaces' },
            { text: 'Editor Integration (ACP)', link: '/guides/acp' },
            { text: 'CLI & Interactive Workflow', link: '/guides/cli' },
            { text: 'Security & Permissions', link: '/guides/permissions' },
            { text: 'Customizations & Extensions', link: '/guides/customizations' },
            { text: 'Tools & MCP Servers', link: '/guides/tools-and-mcp' },
          ],
        },
      ],
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
          text: 'Reference Specifications',
          items: [
            { text: 'CLI Command Reference', link: '/reference/cli' },
            { text: 'Headless Execution Reference', link: '/reference/exec' },
            { text: 'Built-in Tool Reference', link: '/reference/tools' },
            { text: 'Resource Manifests', link: '/reference/manifests' },
            { text: 'Providers Overview & Schema', link: '/reference/providers' },
          ],
        },
        {
          text: 'Model Providers',
          items: [
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
