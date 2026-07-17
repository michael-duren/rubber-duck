import * as fs from "fs/promises";
import * as os from "os";
import * as path from "path";

export interface CourseSummary {
  slug: string;
  title: string;
  duration_hours?: number;
  tags: string[];
  languages: string[];
  updated_at: string;
}

export interface Challenge {
  lesson_slug: string;
  slug: string;
  title: string;
  points: number;
  starter_code: string;
  test_code: string;
}

// Files each language's challenge directory uses, mirroring the CLI's
// grader.LanguageFiles convention (internal/grader/grader.go).
export const languageFiles: Record<string, { code: string; tests: string }> = {
  go: { code: "solution.go", tests: "solution_test.go" },
  python: { code: "solution.py", tests: "test_solution.py" },
  c: { code: "solution.c", tests: "test_solution.c" },
};

export class AuthError extends Error {}

// loadToken resolves the user's CLI token the same way the duck CLI does:
// DUCK_TOKEN env var first, then ~/.config/duck/token.
export async function loadToken(): Promise<string | undefined> {
  if (process.env.DUCK_TOKEN) {
    return process.env.DUCK_TOKEN;
  }
  try {
    const p = path.join(os.homedir(), ".config", "duck", "token");
    return (await fs.readFile(p, "utf8")).trim();
  } catch {
    return undefined;
  }
}

async function getJSON(url: string, token?: string): Promise<any> {
  const headers: Record<string, string> = {};
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }
  // Without a deadline an unreachable server leaves the tree view spinning
  // forever; time out so the user gets an error node instead.
  const resp = await fetch(url, { headers, signal: AbortSignal.timeout(15_000) });
  if (resp.status === 401) {
    throw new AuthError("unauthorized: token missing or revoked — run duck auth login");
  }
  if (!resp.ok) {
    const body = (await resp.text()).slice(0, 200);
    throw new Error(`server said ${resp.status}: ${body}`);
  }
  return resp.json();
}

// listCourses hits the authenticated catalog endpoint; a user token minted
// by `duck auth login` is accepted alongside agent keys.
export async function listCourses(baseUrl: string, token: string): Promise<CourseSummary[]> {
  const data = await getJSON(`${baseUrl.replace(/\/+$/, "")}/api/v1/courses`, token);
  return data.courses ?? [];
}

// listChallenges is public: challenge prompts and tests aren't secret.
export async function listChallenges(
  baseUrl: string,
  course: string,
  language: string,
): Promise<Challenge[]> {
  const base = baseUrl.replace(/\/+$/, "");
  const data = await getJSON(
    `${base}/api/v1/courses/${encodeURIComponent(course)}/variants/${encodeURIComponent(language)}/challenges`,
  );
  return data.challenges ?? [];
}
