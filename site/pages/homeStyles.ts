export const HOME_CSS = `
.ts-home {
  position: relative;
  max-width: 1140px;
  margin: 0 auto;
  padding: 2.75rem 1.5rem 5.5rem;
  overflow-x: clip;
}

.ts-hero {
  position: relative;
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
  gap: 1.25rem;
  padding: 0.75rem 0 3.25rem;
}

.ts-hero::before {
  content: '';
  position: absolute;
  top: -3.5rem;
  left: 50%;
  transform: translateX(-50%);
  width: min(880px, 100vw);
  height: 420px;
  background: radial-gradient(
    ellipse 62% 54% at 50% 34%,
    rgba(42, 195, 222, 0.14),
    rgba(26, 188, 156, 0.06) 48%,
    transparent 76%
  );
  pointer-events: none;
  z-index: 0;
}

.ts-hero > * {
  position: relative;
  z-index: 1;
}

.ts-logo {
  display: block;
  filter: drop-shadow(0 14px 28px rgba(42, 195, 222, 0.22));
  transition: transform 0.22s ease;
}

.ts-logo:hover {
  transform: translateY(-2px);
}

.ts-hero-title {
  max-width: 860px;
  font-size: clamp(2.25rem, 4.8vw, 3.55rem);
  font-weight: 800;
  letter-spacing: -0.032em;
  line-height: 1.1;
  margin: 0.15rem 0 0;
  color: var(--rp-c-text-1);
}

.ts-hero-gradient {
  background: linear-gradient(125deg, #2ac3de 0%, #1abc9c 58%, #56d4dd 100%);
  -webkit-background-clip: text;
  background-clip: text;
  color: transparent;
}

.ts-hero-subtitle {
  max-width: 740px;
  font-size: clamp(1.02rem, 1.85vw, 1.18rem);
  line-height: 1.62;
  margin: 0;
  color: var(--rp-c-text-2);
}

.ts-install-card {
  width: min(100%, 740px);
  display: flex;
  flex-direction: column;
  border-radius: 12px;
  background: #0d1117;
  border: 1px solid rgba(42, 195, 222, 0.3);
  box-shadow: 0 16px 38px rgba(0, 0, 0, 0.24);
  color: #e6edf3;
  overflow: hidden;
  margin-top: 0.45rem;
}

.ts-install-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
  padding: 0.62rem 1rem;
  background: #161b22;
  border-bottom: 1px solid #21262d;
}

.ts-install-tabs {
  display: flex;
  align-items: center;
  gap: 0.35rem;
}

.ts-install-tab {
  cursor: pointer;
  border: 1px solid transparent;
  border-radius: 6px;
  padding: 0.34rem 0.82rem;
  font-size: 0.8rem;
  font-weight: 600;
  background: transparent;
  color: #9198a1;
  transition: background 0.15s ease, color 0.15s ease, border-color 0.15s ease;
}

.ts-install-tab:hover {
  color: #e6edf3;
  background: rgba(255, 255, 255, 0.05);
}

.ts-install-tab[data-active='true'] {
  background: rgba(42, 195, 222, 0.15);
  border-color: rgba(42, 195, 222, 0.32);
  color: #56d4dd;
}

.ts-install-meta {
  font-size: 0.75rem;
  color: #8b949e;
  white-space: nowrap;
}

.ts-install-body {
  display: flex;
  align-items: center;
  gap: 1rem;
  min-height: 5.75rem;
  padding: 1.15rem 1.15rem 1.15rem 1.3rem;
  text-align: left;
}

.ts-install-lines {
  flex: 1;
  min-width: 0;
  overflow-x: auto;
  display: flex;
  flex-direction: column;
  justify-content: center;
  gap: 0.45rem;
  font-size: 0.88rem;
  line-height: 1.55;
}

.ts-copy-btn {
  cursor: pointer;
  border: 1px solid #30363d;
  background: #161b22;
  color: #c9d1d9;
  border-radius: 7px;
  padding: 0.45rem 0.88rem;
  font-size: 0.8rem;
  font-weight: 600;
  flex-shrink: 0;
  transition: border-color 0.15s ease, background 0.15s ease, color 0.15s ease;
}

.ts-copy-btn:hover {
  border-color: #58a6ff;
  color: #ffffff;
  background: #21262d;
}

.ts-copy-btn[data-copied='true'] {
  border-color: rgba(63, 185, 80, 0.55);
  background: rgba(63, 185, 80, 0.16);
  color: #7ee787;
}

.ts-cta-row {
  display: flex;
  flex-wrap: wrap;
  justify-content: center;
  gap: 0.75rem;
  margin-top: 0.35rem;
}

.ts-btn-primary {
  display: inline-flex;
  align-items: center;
  gap: 0.4rem;
  padding: 0.68rem 1.35rem;
  border-radius: 9px;
  background: linear-gradient(135deg, #2ac3de 0%, #1abc9c 100%);
  color: #0b1117;
  font-weight: 700;
  font-size: 0.94rem;
  text-decoration: none;
  box-shadow: 0 8px 22px rgba(42, 195, 222, 0.24);
  transition: transform 0.16s ease, box-shadow 0.16s ease, filter 0.16s ease;
}

.ts-btn-primary:hover {
  transform: translateY(-1px);
  box-shadow: 0 12px 26px rgba(42, 195, 222, 0.34);
  filter: brightness(1.04);
}

.ts-btn-secondary {
  display: inline-flex;
  align-items: center;
  gap: 0.4rem;
  padding: 0.68rem 1.25rem;
  border-radius: 9px;
  border: 1px solid var(--rp-c-divider);
  background: var(--rp-c-bg-soft);
  color: var(--rp-c-text-1);
  font-weight: 600;
  font-size: 0.94rem;
  text-decoration: none;
  transition: border-color 0.16s ease, transform 0.16s ease, background 0.16s ease;
}

.ts-btn-secondary:hover {
  border-color: rgba(42, 195, 222, 0.5);
  transform: translateY(-1px);
}

/* Terminal Tour */
.ts-tour-section {
  margin-bottom: 3.75rem;
}

.ts-tour-tabs {
  display: flex;
  flex-wrap: wrap;
  justify-content: center;
  gap: 0.5rem;
  margin-bottom: 0.9rem;
}

.ts-tour-tab {
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  gap: 0.48rem;
  padding: 0.45rem 0.95rem;
  border-radius: 999px;
  border: 1px solid var(--rp-c-divider);
  background: var(--rp-c-bg-soft);
  color: var(--rp-c-text-2);
  font-size: 0.82rem;
  font-weight: 600;
  transition: border-color 0.16s ease, color 0.16s ease, background 0.16s ease;
}

.ts-tour-tab:hover {
  color: var(--rp-c-text-1);
  border-color: rgba(42, 195, 222, 0.45);
}

.ts-tour-tab[data-active='true'] {
  background: rgba(42, 195, 222, 0.13);
  border-color: rgba(42, 195, 222, 0.55);
  color: var(--rp-c-text-1);
}

.ts-tour-num {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 1.25rem;
  height: 1.25rem;
  border-radius: 50%;
  font-size: 0.72rem;
  font-weight: 700;
  background: rgba(145, 152, 161, 0.18);
  color: inherit;
}

.ts-tour-tab[data-active='true'] .ts-tour-num {
  background: #2ac3de;
  color: #0d1117;
}

.ts-tour-caption {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  align-items: center;
  gap: 0.65rem;
  margin-top: 0.75rem;
  padding: 0 0.25rem;
  font-size: 0.85rem;
  color: var(--rp-c-text-2);
}

.ts-inline-link {
  color: #2ac3de;
  font-weight: 600;
  text-decoration: none;
  white-space: nowrap;
}

.ts-inline-link:hover {
  text-decoration: underline;
}

/* Provider Strip */
.ts-provider-strip {
  margin-bottom: 4.25rem;
  padding: 1.35rem 1.5rem;
  border-radius: 14px;
  border: 1px solid var(--rp-c-divider);
  background: var(--rp-c-bg-soft);
}

.ts-provider-header {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  align-items: center;
  gap: 0.5rem;
  margin-bottom: 1rem;
}

.ts-eyebrow {
  font-size: 0.74rem;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: #2ac3de;
}

.ts-provider-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(165px, 1fr));
  gap: 0.65rem;
}

.ts-provider-chip {
  display: flex;
  flex-direction: column;
  gap: 0.2rem;
  padding: 0.7rem 0.85rem;
  border-radius: 10px;
  border: 1px solid var(--rp-c-divider);
  background: var(--rp-c-bg);
  text-decoration: none;
  transition: border-color 0.16s ease, transform 0.16s ease;
}

.ts-provider-chip:hover {
  border-color: rgba(42, 195, 222, 0.55);
  transform: translateY(-1px);
}

.ts-provider-name {
  font-size: 0.88rem;
  font-weight: 700;
  color: var(--rp-c-text-1);
}

.ts-provider-role {
  font-size: 0.75rem;
  color: var(--rp-c-text-2);
}

/* Declarative Split Section */
.ts-split-section {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1.08fr);
  gap: 2.25rem;
  align-items: center;
  margin-bottom: 4.5rem;
}

@media (max-width: 920px) {
  .ts-split-section {
    grid-template-columns: 1fr;
  }
}

.ts-section-title {
  margin: 0.35rem 0 0.75rem;
  font-size: clamp(1.55rem, 3vw, 2.05rem);
  font-weight: 800;
  letter-spacing: -0.02em;
  line-height: 1.2;
  color: var(--rp-c-text-1);
}

.ts-section-desc {
  margin: 0 0 1.25rem;
  font-size: 0.97rem;
  line-height: 1.62;
  color: var(--rp-c-text-2);
}

.ts-tier-list {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  margin-bottom: 1.25rem;
}

.ts-tier-item {
  display: grid;
  grid-template-columns: 96px 1fr;
  align-items: baseline;
  gap: 0.75rem;
  padding: 0.55rem 0.8rem;
  border-radius: 9px;
  border: 1px solid var(--rp-c-divider);
  background: var(--rp-c-bg-soft);
  font-size: 0.84rem;
}

.ts-tier-badge {
  font-weight: 700;
  color: var(--rp-c-text-1);
}

.ts-code-card {
  border-radius: 12px;
  background: #0d1117;
  border: 1px solid #30363d;
  box-shadow: 0 18px 42px rgba(0, 0, 0, 0.26);
  overflow: hidden;
}

.ts-code-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 0.55rem 0.95rem;
  background: #161b22;
  border-bottom: 1px solid #21262d;
  font-size: 0.76rem;
  color: #9198a1;
}

/* Feature Grid */
.ts-features-header {
  text-align: center;
  max-width: 680px;
  margin: 0 auto 2rem;
}

.ts-feature-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(310px, 1fr));
  gap: 1.15rem;
  margin-bottom: 4rem;
}

.ts-feature-card {
  display: flex;
  flex-direction: column;
  gap: 0.58rem;
  padding: 1.4rem;
  border-radius: 13px;
  border: 1px solid var(--rp-c-divider);
  background: var(--rp-c-bg-soft);
  text-decoration: none;
  color: inherit;
  transition: transform 0.18s ease, border-color 0.18s ease, box-shadow 0.18s ease;
}

.ts-feature-card:hover {
  transform: translateY(-2px);
  border-color: rgba(42, 195, 222, 0.5);
  box-shadow: 0 12px 28px rgba(0, 0, 0, 0.12);
}

.ts-feature-more {
  margin-top: auto;
  padding-top: 0.45rem;
  font-size: 0.82rem;
  font-weight: 600;
  color: var(--rp-c-text-2);
  display: inline-flex;
  align-items: center;
  gap: 0.3rem;
  transition: color 0.16s ease, gap 0.16s ease;
}

.ts-feature-card:hover .ts-feature-more {
  color: #2ac3de;
  gap: 0.5rem;
}

/* Bottom CTA */
.ts-bottom-banner {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  align-items: center;
  gap: 1.5rem;
  padding: 1.75rem 2rem;
  border-radius: 14px;
  border: 1px solid rgba(42, 195, 222, 0.28);
  background: linear-gradient(
    135deg,
    rgba(42, 195, 222, 0.08) 0%,
    rgba(26, 188, 156, 0.05) 100%
  );
}

@media (max-width: 640px) {
  .ts-install-meta {
    display: none;
  }
  .ts-bottom-banner {
    padding: 1.35rem;
  }
}
`;
