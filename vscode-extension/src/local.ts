import * as vscode from "vscode";
import * as fs from "fs/promises";
import * as path from "path";

export interface CourseMeta {
  base_url: string;
  course: string;
  language: string;
}

export interface LocalCourse {
  root: string;
  meta: CourseMeta;
}

const META_FILE = ".duck-course.json";

// findLocalCourses scans the workspace for directories `duck pull`
// scaffolded, identified by their .duck-course.json marker.
export async function findLocalCourses(): Promise<LocalCourse[]> {
  const markers = await vscode.workspace.findFiles(
    `**/${META_FILE}`,
    "**/node_modules/**",
    50,
  );
  const courses: LocalCourse[] = [];
  for (const uri of markers) {
    try {
      const meta = JSON.parse(await fs.readFile(uri.fsPath, "utf8")) as CourseMeta;
      courses.push({ root: path.dirname(uri.fsPath), meta });
    } catch {
      // unreadable/corrupt marker: skip, the CLI will complain if used
    }
  }
  return courses;
}

// findCourseRootFor walks up from a file the way the CLI's findCourseRoot
// does, so commands can resolve the course (and challenge slug) from the
// active editor.
export async function findCourseRootFor(
  filePath: string,
): Promise<LocalCourse | undefined> {
  let dir = path.dirname(filePath);
  for (;;) {
    try {
      const meta = JSON.parse(
        await fs.readFile(path.join(dir, META_FILE), "utf8"),
      ) as CourseMeta;
      return { root: dir, meta };
    } catch {
      const parent = path.dirname(dir);
      if (parent === dir) {
        return undefined;
      }
      dir = parent;
    }
  }
}

// challengeSlugFor returns the challenge directory name for a file inside a
// course tree: the first path segment under the course root.
export function challengeSlugFor(course: LocalCourse, filePath: string): string | undefined {
  const rel = path.relative(course.root, filePath);
  if (rel.startsWith("..") || path.isAbsolute(rel)) {
    return undefined;
  }
  const [first] = rel.split(path.sep);
  return first && first !== META_FILE ? first : undefined;
}
