import {
  expect,
  request,
  type APIRequestContext,
  type Browser,
  type BrowserContext,
  type Page,
} from "@playwright/test";

export const WEB_BASE_URL =
  process.env.PLAYWRIGHT_WEB_BASE_URL || "http://127.0.0.1:4173";
export const API_BASE_URL =
  process.env.PLAYWRIGHT_API_BASE_URL || "http://127.0.0.1:8999";
export const ACTIVE_UPSTREAM = "e2e/wl-commons";

export interface CapturedRequest {
  url: string;
  path: string;
  method: string;
  headers: Record<string, string>;
  body: string | null;
}

export interface HostedRequestLog {
  method: string;
  path: string;
  query?: string;
  headers?: Record<string, string>;
  body?: string;
}

export interface DoltHubRequestLog {
  method: string;
  path: string;
  query?: string;
  headers?: Record<string, string>;
  body?: string;
}

export interface BranchSnapshot {
  wanted?: Record<string, string>[];
  completions?: Record<string, string>[];
  stamps?: Record<string, string>[];
  rigs?: Record<string, string>[];
}

export interface PullRequestSnapshot {
  id: string;
  upstream_owner: string;
  upstream_db: string;
  from_branch_owner: string;
  from_branch_repo: string;
  from_branch: string;
  author: string;
  state: string;
  title?: string;
  description?: string;
  url: string;
}

export interface RepositorySnapshot {
  owner: string;
  db: string;
  branches: Record<string, BranchSnapshot>;
  pull_requests?: PullRequestSnapshot[];
}

export interface TestState {
  hosted_requests: HostedRequestLog[];
  dolthub: {
    requests: DoltHubRequestLog[];
    repositories: RepositorySnapshot[];
    pull_requests: PullRequestSnapshot[];
  };
}

export interface LeaderboardEntry {
  rig_handle: string;
  completions: number;
  avg_quality: number;
  avg_reliability: number;
  avg_creativity: number;
  top_skills?: string[];
}

export interface LeaderboardResponse {
  entries: LeaderboardEntry[];
}

export interface ScoreboardEntry {
  rig_handle: string;
  display_name?: string;
  trust_tier: string;
  stamp_count: number;
  weighted_score: number;
  unique_towns: number;
  completions: number;
  avg_quality: number;
  avg_reliability: number;
  avg_creativity: number;
  top_skills?: string[];
}

export interface ScoreboardResponse {
  entries: ScoreboardEntry[];
  updated_at: string;
}

export interface SeedRepository {
  owner: string;
  db: string;
  main_sql?: string[];
  fork_of?: { owner: string; db: string };
  branches?: Array<{
    name: string;
    from?: string;
    sql?: string[];
  }>;
}

export interface SeedPullRequest {
  id?: string;
  upstream_owner: string;
  upstream_db: string;
  from_owner: string;
  from_db?: string;
  from_branch: string;
  author?: string;
  state?: string;
  title?: string;
  description?: string;
}

export interface SeedRequest {
  repositories?: SeedRepository[];
  prs?: SeedPullRequest[];
}

export interface WantedStateSQLInput {
  wantedID: string;
  status: string;
  claimedBy?: string;
  completedBy?: string;
  evidence?: string;
  completionID?: string;
  validatedBy?: string;
  stampID?: string;
}

interface TestSessionResponse {
  session_cookie_name: string;
  session_cookie_value: string;
  subject_cookie_name: string;
  subject_cookie_value: string;
}

export async function newApiRequestContext(): Promise<APIRequestContext> {
  return request.newContext({ baseURL: API_BASE_URL });
}

export async function resetTestState(api: APIRequestContext): Promise<void> {
  const resp = await api.post("/__test/reset");
  expect(
    resp.ok(),
    `POST /__test/reset failed with ${resp.status()}`,
  ).toBeTruthy();
}

export async function seedDoltHub(
  api: APIRequestContext,
  payload: SeedRequest,
): Promise<void> {
  const resp = await api.post("/__test/seed", { data: payload });
  expect(
    resp.ok(),
    `POST /__test/seed failed with ${resp.status()}: ${await resp.text()}`,
  ).toBeTruthy();
}

export async function fetchTestState(
  api: APIRequestContext,
): Promise<TestState> {
  const resp = await api.get("/__test/state");
  expect(
    resp.ok(),
    `GET /__test/state failed with ${resp.status()}`,
  ).toBeTruthy();
  return (await resp.json()) as TestState;
}

export async function mergePullRequest(
  api: APIRequestContext,
  prID: string,
): Promise<void> {
  const resp = await api.post("/__test/merge-pr", { data: { pr_id: prID } });
  expect(
    resp.ok(),
    `POST /__test/merge-pr failed with ${resp.status()}: ${await resp.text()}`,
  ).toBeTruthy();
}

export async function fetchLeaderboard(
  api: APIRequestContext,
): Promise<LeaderboardResponse> {
  const resp = await api.get("/api/leaderboard");
  expect(
    resp.ok(),
    `GET /api/leaderboard failed with ${resp.status()}: ${await resp.text()}`,
  ).toBeTruthy();
  return (await resp.json()) as LeaderboardResponse;
}

export async function fetchScoreboard(
  api: APIRequestContext,
): Promise<ScoreboardResponse> {
  const resp = await api.get("/api/scoreboard");
  expect(
    resp.ok(),
    `GET /api/scoreboard failed with ${resp.status()}: ${await resp.text()}`,
  ).toBeTruthy();
  return (await resp.json()) as ScoreboardResponse;
}

export async function newActorContext(
  browser: Browser,
  actor: string,
): Promise<BrowserContext> {
  const api = await newApiRequestContext();
  try {
    const session = await createActorSession(api, actor);
    const context = await browser.newContext();
    await context.addCookies([
      {
        name: session.session_cookie_name,
        value: session.session_cookie_value,
        url: WEB_BASE_URL,
      },
      {
        name: session.subject_cookie_name,
        value: session.subject_cookie_value,
        url: WEB_BASE_URL,
      },
    ]);
    await context.addInitScript(
      ({ upstream }) => {
        localStorage.setItem("wl_active", upstream);
        sessionStorage.removeItem("wl_impersonate");
      },
      { upstream: ACTIVE_UPSTREAM },
    );
    return context;
  } finally {
    await api.dispose();
  }
}

export async function newActorPage(
  browser: Browser,
  actor: string,
): Promise<{ context: BrowserContext; page: Page }> {
  const context = await newActorContext(browser, actor);
  const page = await context.newPage();
  return { context, page };
}

async function createActorSession(
  api: APIRequestContext,
  actor: string,
): Promise<TestSessionResponse> {
  const resp = await api.post("/__test/session", { data: { actor } });
  expect(
    resp.ok(),
    `POST /__test/session failed with ${resp.status()}: ${await resp.text()}`,
  ).toBeTruthy();
  return (await resp.json()) as TestSessionResponse;
}

export function captureRequests(page: Page): CapturedRequest[] {
  const captured: CapturedRequest[] = [];
  page.on("request", (req) => {
    const url = req.url();
    if (!url.includes("/api/")) {
      return;
    }
    captured.push({
      url,
      path: new URL(url).pathname,
      method: req.method(),
      headers: req.headers(),
      body: req.postData(),
    });
  });
  return captured;
}

export function findCapturedRequest(
  requests: CapturedRequest[],
  path: string,
  method: string,
): CapturedRequest {
  const target = requests.find(
    (request) => request.path === path && request.method === method,
  );
  if (!target) {
    throw new Error(
      `request not found for ${method} ${path}: ${JSON.stringify(requests, null, 2)}`,
    );
  }
  return target;
}

export function findHostedRequest(
  logs: HostedRequestLog[],
  path: string,
  method: string,
): HostedRequestLog {
  const target = logs.find(
    (entry) => entry.path === path && entry.method === method,
  );
  if (!target) {
    throw new Error(
      `hosted request not found for ${method} ${path}: ${JSON.stringify(logs, null, 2)}`,
    );
  }
  return target;
}

export function findRepository(
  state: TestState,
  owner: string,
  db: string,
): RepositorySnapshot {
  const repo = state.dolthub.repositories.find(
    (entry) => entry.owner === owner && entry.db === db,
  );
  if (!repo) {
    throw new Error(
      `repository ${owner}/${db} not found: ${JSON.stringify(state.dolthub.repositories, null, 2)}`,
    );
  }
  return repo;
}

export function findPullRequest(
  state: TestState,
  input: {
    upstreamOwner: string;
    upstreamDB: string;
    fromOwner: string;
    fromBranch: string;
  },
): PullRequestSnapshot {
  const pr = state.dolthub.pull_requests.find(
    (entry) =>
      entry.upstream_owner === input.upstreamOwner &&
      entry.upstream_db === input.upstreamDB &&
      entry.from_branch_owner === input.fromOwner &&
      entry.from_branch === input.fromBranch,
  );
  if (!pr) {
    throw new Error(
      `pull request not found: ${JSON.stringify(input)} in ${JSON.stringify(state.dolthub.pull_requests, null, 2)}`,
    );
  }
  return pr;
}

export function findRow(
  rows: Record<string, string>[] | undefined,
  key: string,
  value: string,
): Record<string, string> {
  const row = rows?.find((entry) => entry[key] === value);
  if (!row) {
    throw new Error(
      `row ${key}=${value} not found in ${JSON.stringify(rows, null, 2)}`,
    );
  }
  return row;
}

export function findLeaderboardEntry(
  response: LeaderboardResponse,
  rigHandle: string,
): LeaderboardEntry {
  const entry = response.entries.find((row) => row.rig_handle === rigHandle);
  if (!entry) {
    throw new Error(
      `leaderboard entry not found for ${rigHandle}: ${JSON.stringify(response, null, 2)}`,
    );
  }
  return entry;
}

export function findScoreboardEntry(
  response: ScoreboardResponse,
  rigHandle: string,
): ScoreboardEntry {
  const entry = response.entries.find((row) => row.rig_handle === rigHandle);
  if (!entry) {
    throw new Error(
      `scoreboard entry not found for ${rigHandle}: ${JSON.stringify(response, null, 2)}`,
    );
  }
  return entry;
}

export function buildWantedInsertSQL(
  wantedID: string,
  title: string,
  description: string,
  postedBy = "alice",
): string {
  return [
    "INSERT INTO wanted (id, title, description, priority, posted_by, status, effort_level, created_at, updated_at)",
    `VALUES ('${escapeSQL(wantedID)}', '${escapeSQL(title)}', '${escapeSQL(description)}', 2, '${escapeSQL(postedBy)}', 'open', 'medium', NOW(), NOW())`,
  ].join(" ");
}

export function buildInReviewSubmissionSQL(
  wantedID: string,
  completedBy: string,
  evidence: string,
  completionID: string,
): string[] {
  return buildWantedStateSQL({
    wantedID,
    status: "in_review",
    claimedBy: completedBy,
    completedBy,
    evidence,
    completionID,
  });
}

export function buildWantedStateSQL(input: WantedStateSQLInput): string[] {
  const wantedID = escapeSQL(input.wantedID);
  const claimedBy = input.claimedBy?.trim();
  const completedBy = input.completedBy?.trim();
  const evidence = input.evidence ?? "";
  const completionID = input.completionID?.trim() || `c-${input.wantedID}-${input.status}`;
  const columns: string[] = [];
  const values: string[] = [];
  const statements = [
    `DELETE FROM completions WHERE wanted_id='${wantedID}'`,
    `UPDATE wanted SET claimed_by=${quotedOrNull(claimedBy)}, status='${escapeSQL(input.status)}', updated_at=NOW() WHERE id='${wantedID}'`,
  ];

  if (completedBy) {
    columns.push("id", "wanted_id", "completed_by", "completed_at");
    values.push(
      `'${escapeSQL(completionID)}'`,
      `'${wantedID}'`,
      `'${escapeSQL(completedBy)}'`,
      "NOW()",
    );
    if (input.evidence !== undefined) {
      columns.push("evidence");
      values.push(`'${escapeSQL(evidence)}'`);
    }
    if (input.validatedBy?.trim()) {
      columns.push("validated_by", "validated_at");
      values.push(`'${escapeSQL(input.validatedBy.trim())}'`, "NOW()");
    }
    if (input.stampID?.trim()) {
      columns.push("stamp_id");
      values.push(`'${escapeSQL(input.stampID.trim())}'`);
    }
    statements.push(
      `INSERT INTO completions (${columns.join(", ")}) VALUES (${values.join(", ")})`,
    );
  }

  return statements;
}

function quotedOrNull(value?: string): string {
  if (!value) {
    return "NULL";
  }
  return `'${escapeSQL(value)}'`;
}

export async function seedWantedAcrossRepos(
  api: APIRequestContext,
  wantedID: string,
  title: string,
  description: string,
  postedBy = "alice",
): Promise<void> {
  const insert = buildWantedInsertSQL(wantedID, title, description, postedBy);
  await seedDoltHub(api, {
    repositories: [
      { owner: "e2e", db: "wl-commons", main_sql: [insert] },
      { owner: "alice", db: "wl-commons", main_sql: [insert] },
      { owner: "bob", db: "wl-commons", main_sql: [insert] },
      { owner: "charlie", db: "wl-commons", main_sql: [insert] },
    ],
  });
}

function escapeSQL(value: string): string {
  return value.replace(/'/g, "''");
}
