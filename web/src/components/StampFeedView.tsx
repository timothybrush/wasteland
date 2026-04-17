import type { StampFeedEntry, StampFeedResponse } from "../api/types";
import styles from "./StampFeedView.module.css";

export function StampFeedView({ data }: { data: StampFeedResponse }) {
  const githubHref = safeHref(data.github_url);

  return (
    <div className={styles.page}>
      <div className={styles.header}>
        <h2 className={styles.name}>{data.handle}</h2>
        {githubHref && (
          <a
            className={styles.githubLink}
            href={githubHref}
            target="_blank"
            rel="noopener noreferrer"
            aria-label="View profile on GitHub (opens in new tab)"
          >
            View on GitHub ↗
          </a>
        )}
      </div>
      <p className={styles.banner}>No character sheet yet — showing federation activity</p>

      {data.stamps_error ? (
        <p className={styles.errorNote}>Couldn't load recent stamps — try again later.</p>
      ) : (
        <section className={styles.stampList}>
          {data.stamps.map((stamp) => (
            <StampCard key={stamp.id} stamp={stamp} />
          ))}
        </section>
      )}
    </div>
  );
}

function StampCard({ stamp }: { stamp: StampFeedEntry }) {
  const date = formatDate(stamp.created_at);
  const hasValence = stamp.quality > 0 || stamp.reliability > 0;
  const evidenceHref = safeHref(stamp.evidence_url);

  return (
    <article className={styles.card}>
      <header className={styles.cardHeader}>
        <div className={styles.tagList}>
          {stamp.skill_tags.map((tag, idx) => (
            <span key={`${tag}-${idx}`} className={styles.tag}>
              {tag}
            </span>
          ))}
        </div>
        {hasValence && (
          <span className={styles.valence} title="Quality / Reliability (0-5)">
            <span className={styles.srOnly}>
              Quality {stamp.quality} of 5, Reliability {stamp.reliability} of 5
            </span>
            <span aria-hidden="true">
              Q{stamp.quality} R{stamp.reliability}
            </span>
          </span>
        )}
      </header>

      {evidenceHref && (
        <div className={styles.evidenceLine}>
          <span aria-hidden="true">→ </span>
          <a
            className={styles.evidenceLink}
            href={evidenceHref}
            target="_blank"
            rel="noopener noreferrer"
            aria-label={`View evidence ${stamp.evidence_label || evidenceHref} (opens in new tab)`}
          >
            {stamp.evidence_label || evidenceHref}
          </a>
        </div>
      )}

      <div className={styles.meta}>
        <span>validated by {stamp.validator}</span>
        {date && <span>· {date}</span>}
      </div>

      {stamp.message && <p className={styles.message}>{stamp.message}</p>}
      {stamp.evidence_text && !evidenceHref && <p className={styles.message}>{stamp.evidence_text}</p>}
    </article>
  );
}

// safeHref returns the URL only when its scheme is http(s). Prevents
// javascript: or data: URLs from backend evidence fields becoming
// clickable XSS gadgets.
function safeHref(raw: string | undefined): string | null {
  if (!raw) return null;
  try {
    const parsed = new URL(raw);
    if (parsed.protocol === "http:" || parsed.protocol === "https:") {
      return raw;
    }
  } catch {
    // Not a valid URL; fall through.
  }
  return null;
}

function formatDate(raw: string): string {
  if (!raw) return "";
  const date = new Date(raw);
  if (Number.isNaN(date.getTime())) return raw;
  // Format in the viewer's local timezone so timestamps read as the
  // date the event happened from their perspective, not UTC.
  return date.toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
}
