import * as vscode from "vscode";
import * as fs from "fs/promises";
import * as path from "path";
import {
  AuthError,
  CourseSummary,
  languageFiles,
  listChallenges,
  listCourses,
  loadToken,
} from "./api";
import { baseUrl } from "./cli";
import { findLocalCourses, LocalCourse } from "./local";

export class CourseItem extends vscode.TreeItem {
  constructor(public readonly course: CourseSummary) {
    super(course.title, vscode.TreeItemCollapsibleState.Collapsed);
    this.contextValue = "duckCourse";
    this.description = course.languages.join(", ");
    this.tooltip = [
      course.slug,
      course.tags.length ? `tags: ${course.tags.join(", ")}` : "",
      course.duration_hours ? `~${course.duration_hours}h` : "",
    ]
      .filter(Boolean)
      .join("\n");
    this.iconPath = new vscode.ThemeIcon("book");
  }
}

export class VariantItem extends vscode.TreeItem {
  constructor(
    public readonly course: string,
    public readonly language: string,
    public readonly local: LocalCourse | undefined,
  ) {
    super(language, vscode.TreeItemCollapsibleState.Collapsed);
    this.contextValue = local ? "duckVariantPulled" : "duckVariant";
    this.description = local ? "pulled" : undefined;
    this.tooltip = local
      ? `pulled to ${local.root}`
      : `duck pull ${course}/${language}`;
    this.iconPath = new vscode.ThemeIcon(local ? "folder-active" : "code");
  }
}

export class ChallengeItem extends vscode.TreeItem {
  constructor(
    public readonly course: string,
    public readonly language: string,
    public readonly slug: string,
    lessonSlug: string,
    points: number,
    public readonly localDir: string | undefined,
  ) {
    super(slug, vscode.TreeItemCollapsibleState.None);
    this.contextValue = localDir ? "duckChallengePulled" : "duckChallenge";
    this.description = `${points} pts${lessonSlug ? "" : " · final"}`;
    this.tooltip = lessonSlug ? `lesson: ${lessonSlug}` : "final challenge";
    this.iconPath = new vscode.ThemeIcon(
      localDir ? "file-code" : "circle-large-outline",
    );
    if (localDir) {
      const file = languageFiles[language]?.code ?? "";
      this.command = {
        command: "vscode.open",
        title: "Open Solution",
        arguments: [vscode.Uri.file(path.join(localDir, file))],
      };
    }
  }
}

class MessageItem extends vscode.TreeItem {
  constructor(message: string, icon: string) {
    super(message, vscode.TreeItemCollapsibleState.None);
    this.iconPath = new vscode.ThemeIcon(icon);
  }
}

export class CoursesProvider implements vscode.TreeDataProvider<vscode.TreeItem> {
  private _onDidChange = new vscode.EventEmitter<void>();
  readonly onDidChangeTreeData = this._onDidChange.event;

  private locals: LocalCourse[] = [];

  refresh(): void {
    this._onDidChange.fire();
  }

  getTreeItem(item: vscode.TreeItem): vscode.TreeItem {
    return item;
  }

  async getChildren(item?: vscode.TreeItem): Promise<vscode.TreeItem[]> {
    if (!item) {
      return this.rootItems();
    }
    if (item instanceof CourseItem) {
      return item.course.languages.map(
        (lang) =>
          new VariantItem(item.course.slug, lang, this.localFor(item.course.slug, lang)),
      );
    }
    if (item instanceof VariantItem) {
      return this.challengeItems(item);
    }
    return [];
  }

  private localFor(course: string, language: string): LocalCourse | undefined {
    return this.locals.find(
      (l) => l.meta.course === course && l.meta.language === language,
    );
  }

  private async rootItems(): Promise<vscode.TreeItem[]> {
    const token = await loadToken();
    await vscode.commands.executeCommand("setContext", "duck.signedIn", !!token);
    if (!token) {
      return []; // viewsWelcome shows the login prompt
    }
    try {
      const [courses, locals] = await Promise.all([
        listCourses(baseUrl(), token),
        findLocalCourses(),
      ]);
      this.locals = locals;
      if (courses.length === 0) {
        return [new MessageItem("No courses published yet", "info")];
      }
      return courses.map((c) => new CourseItem(c));
    } catch (err) {
      if (err instanceof AuthError) {
        await vscode.commands.executeCommand("setContext", "duck.signedIn", false);
        return [];
      }
      return [new MessageItem(`Couldn't load courses: ${(err as Error).message}`, "warning")];
    }
  }

  private async challengeItems(item: VariantItem): Promise<vscode.TreeItem[]> {
    try {
      const challenges = await listChallenges(baseUrl(), item.course, item.language);
      return Promise.all(
        challenges.map(async (c) => {
          let dir: string | undefined;
          if (item.local) {
            const candidate = path.join(item.local.root, c.slug);
            dir = (await exists(candidate)) ? candidate : undefined;
          }
          return new ChallengeItem(
            item.course,
            item.language,
            c.slug,
            c.lesson_slug,
            c.points,
            dir,
          );
        }),
      );
    } catch (err) {
      return [new MessageItem(`Couldn't load challenges: ${(err as Error).message}`, "warning")];
    }
  }
}

async function exists(p: string): Promise<boolean> {
  try {
    await fs.stat(p);
    return true;
  } catch {
    return false;
  }
}
