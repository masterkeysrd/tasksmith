import {
  isValidElement,
  type ReactNode,
  useEffect,
  useState,
} from 'react';
import {
  getCustomMDXComponent as getOriginalMDXComponent,
  type ShikiPreProps,
  useThemeState,
} from '@rspress/core/theme-original';

export * from '@rspress/core/theme-original';

let renderSeq = 0;
let renderQueue: Promise<void> = Promise.resolve();

function enqueueRender<T>(task: () => Promise<T>): Promise<T> {
  const next = renderQueue.then(task, task);
  renderQueue = next.then(
    () => undefined,
    () => undefined,
  );
  return next;
}

function extractText(node: ReactNode): string {
  if (node == null || typeof node === 'boolean') return '';
  if (typeof node === 'string' || typeof node === 'number') return String(node);
  if (Array.isArray(node)) return node.map(extractText).join('');
  if (isValidElement<{ children?: ReactNode }>(node)) {
    return extractText(node.props.children);
  }
  return '';
}

const FONT_FAMILY =
  '-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif';

const ALLOWED_HTML_TAGS = new Set([
  'br',
  'b',
  'i',
  'u',
  's',
  'strong',
  'em',
  'code',
  'pre',
  'sub',
  'sup',
  'small',
  'span',
  'p',
  'div',
  'hr',
  'ul',
  'ol',
  'li',
  'a',
]);

function normalizeMermaidSource(source: string): string {
  return source
    .trim()
    .replace(/\\n/g, '\n')
    .replace(/<\/?([a-zA-Z][a-zA-Z0-9_-]*)\s*\/?>/g, (match, tag: string) => {
      if (ALLOWED_HTML_TAGS.has(tag.toLowerCase())) {
        return match;
      }
      return match.replace(/</g, '#lt;').replace(/>/g, '#gt;');
    });
}

function buildThemeConfig(isDark: boolean) {
  const themeVariables = isDark
    ? {
        darkMode: true,
        fontFamily: FONT_FAMILY,
        fontSize: '12px',
        background: '#0d1117',
        primaryColor: '#161b22',
        primaryTextColor: '#e6edf3',
        primaryBorderColor: '#2ac3de',
        secondaryColor: '#1c2430',
        secondaryTextColor: '#e6edf3',
        secondaryBorderColor: '#1abc9c',
        tertiaryColor: '#11161d',
        tertiaryTextColor: '#b1bac4',
        tertiaryBorderColor: '#30363d',
        mainBkg: '#161b22',
        nodeBkg: '#161b22',
        nodeBorder: '#2f81f7',
        nodeTextColor: '#e6edf3',
        clusterBkg: 'rgba(22, 27, 34, 0.55)',
        clusterBorder: '#30363d',
        titleColor: '#56d4dd',
        lineColor: '#6e7681',
        arrowheadColor: '#8b949e',
        edgeLabelBackground: '#161b22',
        labelBackground: '#161b22',
        textColor: '#e6edf3',
        useGradient: false,
        radius: 6,
        strokeWidth: 1.25,
      }
    : {
        darkMode: false,
        fontFamily: FONT_FAMILY,
        fontSize: '12px',
        background: '#ffffff',
        primaryColor: '#ffffff',
        primaryTextColor: '#0f172a',
        primaryBorderColor: '#0891b2',
        secondaryColor: '#f1f5f9',
        secondaryTextColor: '#0f172a',
        secondaryBorderColor: '#0d9488',
        tertiaryColor: '#f8fafc',
        tertiaryTextColor: '#334155',
        tertiaryBorderColor: '#cbd5e1',
        mainBkg: '#ffffff',
        nodeBkg: '#ffffff',
        nodeBorder: '#0284c7',
        nodeTextColor: '#0f172a',
        clusterBkg: '#f8fafc',
        clusterBorder: '#cbd5e1',
        titleColor: '#0f766e',
        lineColor: '#64748b',
        arrowheadColor: '#475569',
        edgeLabelBackground: '#f1f5f9',
        labelBackground: '#f1f5f9',
        textColor: '#0f172a',
        useGradient: false,
        radius: 6,
        strokeWidth: 1.25,
      };

  const clusterFill = isDark ? 'rgba(13, 17, 23, 0.65)' : '#f8fafc';
  const clusterStroke = isDark ? '#30363d' : '#cbd5e1';
  const clusterTitle = isDark ? '#8b949e' : '#475569';
  const nodeFill = isDark ? '#161b22' : '#ffffff';
  const nodeStroke = isDark ? 'rgba(42, 195, 222, 0.55)' : '#0284c7';
  const polygonStroke = isDark ? 'rgba(26, 188, 156, 0.7)' : '#0d9488';
  const edgePillBg = isDark ? '#1c2128' : '#f1f5f9';
  const edgePillBorder = isDark ? '#30363d' : '#cbd5e1';
  const edgePillText = isDark ? '#c9d1d9' : '#334155';
  const edgeStroke = isDark ? '#6e7681' : '#64748b';
  const nodeShadow = isDark
    ? 'drop-shadow(0 2px 5px rgba(0, 0, 0, 0.22))'
    : 'drop-shadow(0 1px 4px rgba(15, 23, 42, 0.06))';

  const themeCSS = `
    foreignObject, foreignObject > div {
      overflow: visible !important;
    }
    p {
      margin: 0 !important;
      padding: 0 !important;
      line-height: 1.3 !important;
    }
    .node rect, .node circle, .node ellipse {
      fill: ${nodeFill} !important;
      stroke: ${nodeStroke} !important;
      stroke-width: 1.25px !important;
      rx: 6px;
      ry: 6px;
      filter: ${nodeShadow};
    }
    .node polygon, .node path.label-container {
      fill: ${nodeFill} !important;
      stroke: ${polygonStroke} !important;
      stroke-width: 1.25px !important;
      filter: ${nodeShadow};
    }
    .cluster rect {
      fill: ${clusterFill} !important;
      stroke: ${clusterStroke} !important;
      stroke-width: 1.1px !important;
      stroke-dasharray: 5 4;
      rx: 10px;
      ry: 10px;
    }
    .cluster-label span, .cluster-label text, .cluster-label p {
      color: ${clusterTitle} !important;
      fill: ${clusterTitle} !important;
      font-weight: 600 !important;
      font-size: 11.5px !important;
    }
    .nodeLabel, .node .label {
      font-weight: 500;
      line-height: 1.3 !important;
    }
    .edgePath .path:not(.edge-thickness-invisible),
    .flowchart-link:not(.edge-thickness-invisible) {
      stroke: ${edgeStroke} !important;
      stroke-width: 1.3px !important;
    }
    .edge-thickness-invisible {
      stroke: transparent !important;
      stroke-width: 0 !important;
      fill: none !important;
    }
    .marker {
      fill: ${edgeStroke} !important;
      stroke: ${edgeStroke} !important;
    }
    .edgeLabel, .edgeLabel p, .edgeLabel span {
      background-color: ${edgePillBg} !important;
      color: ${edgePillText} !important;
      font-size: 11px !important;
      line-height: 1.25 !important;
      font-weight: 500;
    }
    .edgeLabel rect {
      fill: ${edgePillBg} !important;
      stroke: ${edgePillBorder} !important;
      stroke-width: 1px !important;
      rx: 5px;
      ry: 5px;
      opacity: 1 !important;
    }
  `;

  return { themeVariables, themeCSS };
}

function MermaidDiagram({ source }: { source: string }) {
  const [theme] = useThemeState();
  const [svg, setSvg] = useState<string>('');
  const [expanded, setExpanded] = useState(false);
  const [zoom, setZoom] = useState(1);

  useEffect(() => {
    let cancelled = false;

    void enqueueRender(async () => {
      if (cancelled) return;
      try {
        const mermaid = (await import('mermaid')).default;
        const isDark =
          theme === 'dark' ||
          (typeof document !== 'undefined' &&
            document.documentElement.classList.contains('dark'));

        const { themeVariables, themeCSS } = buildThemeConfig(isDark);

        mermaid.initialize({
          startOnLoad: false,
          securityLevel: 'strict',
          suppressErrorRendering: true,
          markdownAutoWrap: false,
          look: 'classic',
          theme: 'base',
          fontFamily: FONT_FAMILY,
          themeVariables,
          themeCSS,
          flowchart: {
            curve: 'rounded',
            padding: 8,
            nodeSpacing: 24,
            rankSpacing: 32,
            diagramPadding: 8,
            subGraphTitleMargin: { top: 4, bottom: 6 },
            wrappingWidth: 900,
          },
        });

        const uniqueId = `mermaid-svg-${++renderSeq}`;
        const normalized = normalizeMermaidSource(source);
        const { svg: rendered } = await mermaid.render(uniqueId, normalized);
        if (!cancelled) {
          setSvg(rendered);
        }
      } catch (err) {
        console.error('[MermaidDiagram] Failed to render diagram:', err);
      }
    });

    return () => {
      cancelled = true;
    };
  }, [source, theme]);

  useEffect(() => {
    if (!expanded) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setExpanded(false);
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [expanded]);

  if (!svg) {
    return (
      <pre data-lang="mermaid" style={{ overflowX: 'auto', padding: '1rem' }}>
        <code>{source}</code>
      </pre>
    );
  }

  return (
    <>
      <div
        className="rp-not-doc"
        style={{
          position: 'relative',
          margin: '1.1rem 0',
          padding: '0.9rem 1rem',
          borderRadius: '10px',
          border: '1px solid var(--rp-c-divider)',
          background: 'var(--rp-c-bg-soft)',
          overflowX: 'auto',
        }}
      >
        <button
          type="button"
          onClick={() => {
            setZoom(1);
            setExpanded(true);
          }}
          title="Expand diagram"
          style={{
            position: 'absolute',
            top: '0.65rem',
            right: '0.65rem',
            zIndex: 2,
            cursor: 'pointer',
            padding: '0.28rem 0.6rem',
            fontSize: '0.75rem',
            fontWeight: 600,
            borderRadius: '6px',
            border: '1px solid var(--rp-c-divider)',
            background: 'var(--rp-c-bg)',
            color: 'var(--rp-c-text-2)',
            opacity: 0.85,
          }}
        >
          Expand ⤢
        </button>

        <div
          style={{
            display: 'flex',
            justifyContent: 'center',
            minWidth: 'min-content',
          }}
          // Mermaid's strict mode sanitizes SVG output
          dangerouslySetInnerHTML={{ __html: svg }}
        />
      </div>

      {expanded && (
        <div
          className="rp-not-doc"
          role="dialog"
          aria-modal="true"
          onClick={() => setExpanded(false)}
          style={{
            position: 'fixed',
            inset: 0,
            zIndex: 9999,
            background: 'rgba(8, 12, 18, 0.82)',
            backdropFilter: 'blur(4px)',
            display: 'flex',
            flexDirection: 'column',
            padding: '1.25rem',
          }}
        >
          {/* Modal Toolbar */}
          <div
            onClick={(e) => e.stopPropagation()}
            style={{
              display: 'flex',
              justifyContent: 'flex-end',
              alignItems: 'center',
              gap: '0.5rem',
              marginBottom: '0.75rem',
            }}
          >
            <button
              type="button"
              onClick={() => setZoom((z) => Math.max(0.5, Number((z - 0.25).toFixed(2))))}
              style={modalBtnStyle}
            >
              −
            </button>
            <button
              type="button"
              onClick={() => setZoom(1)}
              style={modalBtnStyle}
            >
              {Math.round(zoom * 100)}%
            </button>
            <button
              type="button"
              onClick={() => setZoom((z) => Math.min(2.5, Number((z + 0.25).toFixed(2))))}
              style={modalBtnStyle}
            >
              +
            </button>
            <button
              type="button"
              onClick={() => setExpanded(false)}
              style={{ ...modalBtnStyle, marginLeft: '0.5rem' }}
            >
              Close (Esc)
            </button>
          </div>

          {/* Zoomable Diagram Viewport */}
          <div
            onClick={(e) => e.stopPropagation()}
            style={{
              flex: 1,
              overflow: 'auto',
              borderRadius: '12px',
              border: '1px solid var(--rp-c-divider)',
              background: 'var(--rp-c-bg)',
              display: 'flex',
              alignItems: 'safe center',
              justifyContent: 'safe center',
              padding: '2rem',
            }}
          >
            <div
              style={{
                transform: `scale(${zoom})`,
                transformOrigin: 'center top',
                transition: 'transform 0.14s ease-out',
              }}
              dangerouslySetInnerHTML={{ __html: svg }}
            />
          </div>
        </div>
      )}
    </>
  );
}

const modalBtnStyle = {
  cursor: 'pointer',
  padding: '0.35rem 0.7rem',
  fontSize: '0.8rem',
  fontWeight: 600,
  borderRadius: '6px',
  border: '1px solid rgba(255, 255, 255, 0.18)',
  background: '#161b22',
  color: '#e6edf3',
} as const;

export function getCustomMDXComponent() {
  const original = getOriginalMDXComponent();
  const OriginalPre = original.pre;

  function PreWithMermaid(props: ShikiPreProps) {
    const childClass = isValidElement<{ className?: string }>(props.children)
      ? props.children.props.className ?? ''
      : '';
    const isMermaid =
      props.lang === 'mermaid' || childClass.includes('language-mermaid');

    if (isMermaid) {
      const source = extractText(props.children);
      return <MermaidDiagram source={source} />;
    }

    return <OriginalPre {...props} />;
  }

  return {
    ...original,
    pre: PreWithMermaid,
  };
}
