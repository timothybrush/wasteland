# Profile Fallback View — Stamp Feed for Pre-Backfill Users

## Summary

When a viewer navigates to `/profile/{handle}` for a user who has
federation activity in `hop/wl-commons` but has not yet been backfilled
into `hop/the-pile`, render a distinct "stamp feed" view instead of
404-ing. The stamp feed shows the rig handle (as a GitHub link) and up
to 10 recent stamps with evidence links, so every active federation
participant has a useful profile page.

## Trigger Rule

The backend checks `hop/the-pile.boot_blocks` for a row matching
`handle`.

- **Row present** → character sheet (existing behavior, unchanged).
- **Row absent** → fall back to `hop/wl-commons` stamp query.
  - **≥1 stamp where `subject = handle`** → stamp feed (new).
  - **0 stamps** → 404 (true unknown handle).

The backend may run both queries concurrently; if `boot_blocks` hits,
discard the `wl-commons` result.

## Data Sources

| Mode             | Source DB        | Tables                              |
|------------------|------------------|-------------------------------------|
| character_sheet  | `hop/the-pile`   | `boot_blocks`, `stamps`             |
| stamp_feed       | `hop/wl-commons` | `stamps` ⟕ `completions`            |

Stamp feed query:

```sql
SELECT s.id, s.skill_tags, s.valence, s.message, s.author,
       s.created_at, c.evidence
FROM stamps s
LEFT JOIN completions c ON s.context_id = c.id
WHERE s.subject = ?
ORDER BY s.created_at DESC, s.id DESC
LIMIT 10
```

No cache — matches the existing profile handler's behavior. Volume is
trivial (`wl-commons` has 14 completions total at time of spec).

## API Contract

`GET /api/profile/{handle}` — **breaking change**: response becomes a
discriminated union.

**Character sheet (existing shape + `kind`):**

```json
{
  "kind": "character_sheet",
  "handle": "bmizerany",
  "display_name": "Blake Mizerany",
  "quality": 3.75,
  "reliability": 4.0
}
```

**Stamp feed (new):**

```json
{
  "kind": "stamp_feed",
  "handle": "rileywhite",
  "github_url": "https://github.com/rileywhite",
  "stamps": [
    {
      "id": "s-abc",
      "skill_tags": ["go", "backend"],
      "quality": 4,
      "reliability": 5,
      "validator": "julianknutsen",
      "message": "Added retry middleware",
      "evidence_url": "https://github.com/gastownhall/gascity/pull/548",
      "evidence_label": "gastownhall/gascity#548",
      "created_at": "2026-04-13T09:33:05Z"
    }
  ],
  "stamps_error": null
}
```

**Stamp feed, `wl-commons` unreachable:**

```json
{
  "kind": "stamp_feed",
  "handle": "rileywhite",
  "github_url": "https://github.com/rileywhite",
  "stamps": [],
  "stamps_error": "stamps_unavailable"
}
```

**404:** no boot_block and no `wl-commons` stamps.

Evidence parsing rules (server-side):

- `^https?://github\.com/([^/]+)/([^/]+)/pull/(\d+)` → `evidence_label`
  = `owner/repo#N`, `evidence_url` unchanged.
- `^https?://github\.com/([^/]+)/([^/]+)` → `evidence_label` =
  `owner/repo`, `evidence_url` unchanged.
- Else → `evidence_label` = null, `evidence_url` = null, raw text
  delivered as `evidence_text`.
- Missing / NULL evidence → all three fields null; card renders without
  an evidence line.

## UI — Stamp Feed View

New React component: `web/src/components/StampFeedView.tsx`.
`ProfileView` delegates to it when `data.kind === "stamp_feed"`.

**Header:**

```
{handle}                             [View on GitHub ↗]
──────────────────────────────────────────────────────
No character sheet yet — showing federation activity
```

- `handle` as `<h1>`.
- GitHub link → `github.com/{handle}`, opens in a new tab
  (`target="_blank" rel="noopener noreferrer"`).
- Banner styled in the existing `unverifiedNote` tone or similar.

**Stamp card (one per row, up to 10):**

```
[go] [backend]                                    Q4 R5
→ gastownhall/gascity#548
validated by julianknutsen · 2026-04-13
"Added retry middleware"
```

- Skill tags as pill badges.
- Q/R scores shown top-right when valence is present.
- Evidence line omitted when `evidence_url` is null.
- Message shown below, truncated at ~200 chars with a "show more"
  affordance.
- Cards ordered by `created_at DESC`.

**Loading state:** reuse the existing `SkeletonRows` component.

**Empty with `stamps_error` set:** render header / banner normally; the
card region shows "Couldn't load recent stamps — try again later" with
a retry affordance.

**Zero stamps without error:** impossible under the trigger rules
(would be a 404).

## Out of Scope (v1)

- **Backfill triggering from the UI.** The-pile pipeline is external;
  there is no in-repo mechanism to request ingestion.
- **Profile search fallback.** `ProfileSearch` queries `the-pile.rigs`
  and will not surface fallback-eligible handles. Users reach the
  fallback only via direct URL or external link. Known gap.
- **Pagination / "all stamps" view.** Fixed 10, no load-more.
- **GitHub API calls** to resolve PR authors or profile metadata.
- **Non-PR evidence rendering beyond `owner/repo`.** Commit and blob
  URLs render as `owner/repo` linked to the full URL; we do not
  special-case them.

## Implementation Breakdown

Three phases, each ≤ 5 files, each independently verifiable.

### Phase 1 — Backend: wl-commons client + discriminated response

- Extend `internal/pile/` (or add a parallel package) to also query
  `hop/wl-commons`.
- New `QueryStampFeed(handle) ([]StampEntry, error)`.
- Refactor `pile.QueryProfile` to return a `ProfileResponse` union:
  `{Kind, CharacterSheet?, StampFeed?}`.
- Update `handleProfile` in `internal/api/pile_handlers.go` to marshal
  the union and branch on `boot_block` presence → `wl-commons`
  fallback.
- Update 404 semantics: 404 only when both sources are empty.
- Unit tests: fake `RowQuerier` for both clients; test all four
  branches (sheet present / sheet absent + stamps / both absent /
  wl-commons error).

### Phase 2 — Frontend types + component

- Update `web/src/api/types.ts` to a discriminated union keyed on
  `kind`.
- New `web/src/components/StampFeedView.tsx` + CSS module.
- `ProfileView.tsx` branches on `data.kind` and delegates.
- Reuse existing `SkeletonRows` for loading.

### Phase 3 — Polish + error-state handling

- `stamps_error` banner variant.
- Evidence-parsing correctness tests (PR, non-PR, free text, NULL).
- Style pass to match the existing Ayu / parchment theme.
