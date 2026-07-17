import * as vscode from "vscode";
import * as path from "path";
import { listCourses, loadToken } from "./api";
import { baseArgs, baseUrl, disposeTerminal, duckPath, execDuck, runInTerminal } from "./cli";
import { DuckCodeLensProvider } from "./codelens";
import { ChallengeItem, CourseItem, CoursesProvider, VariantItem } from "./courses";
import { challengeSlugFor, findCourseRootFor } from "./local";

interface ChallengeRef {
  root: string; // course root directory
  slug?: string; // challenge dir; undefined = whole course
}

export function activate(context: vscode.ExtensionContext): void {
  const provider = new CoursesProvider();

  // Refresh when a pull (ours or the user's own terminal) scaffolds a
  // course, so "pulled" markers stay current.
  const watcher = vscode.workspace.createFileSystemWatcher("**/.duck-course.json");
  watcher.onDidCreate(() => provider.refresh());
  watcher.onDidDelete(() => provider.refresh());

  context.subscriptions.push(
    vscode.window.registerTreeDataProvider("duckCourses", provider),
    vscode.languages.registerCodeLensProvider(
      [{ language: "go" }, { language: "python" }, { language: "c" }],
      new DuckCodeLensProvider(),
    ),
    vscode.window.onDidCloseTerminal(disposeTerminal),
    watcher,

    vscode.commands.registerCommand("duck.refreshCourses", () => provider.refresh()),

    vscode.commands.registerCommand("duck.login", () => {
      const term = vscode.window.createTerminal("duck auth login");
      term.show();
      term.sendText([duckPath(), "auth", "login", ...baseArgs()].join(" "));
      vscode.window
        .showInformationMessage(
          "Complete the login in the terminal, then refresh the course list.",
          "Refresh",
        )
        .then((pick) => pick === "Refresh" && provider.refresh());
    }),

    vscode.commands.registerCommand("duck.openCoursePage", (item?: CourseItem) => {
      if (item instanceof CourseItem) {
        vscode.env.openExternal(vscode.Uri.parse(`${baseUrl()}/courses/${item.course.slug}`));
      }
    }),

    vscode.commands.registerCommand("duck.pullCourse", (item?: VariantItem) => pullCourse(provider, item)),

    vscode.commands.registerCommand("duck.test", async (arg?: unknown) => {
      const ref = await resolveChallenge(arg);
      if (!ref) {
        return;
      }
      runInTerminal(ref.root, ref.slug ? ["test", ref.slug] : ["test"]);
    }),

    vscode.commands.registerCommand("duck.testAll", async (arg?: unknown) => {
      const ref = await resolveChallenge(arg, { allowCourse: true });
      if (!ref) {
        return;
      }
      runInTerminal(ref.root, ["test"]);
    }),

    vscode.commands.registerCommand("duck.submit", async (arg?: unknown) => {
      const ref = await resolveChallenge(arg, { requireSlug: true });
      if (!ref?.slug) {
        return;
      }
      runInTerminal(ref.root, ["submit", ref.slug]);
    }),

    vscode.commands.registerCommand("duck.submitRemote", async (arg?: unknown) => {
      const ref = await resolveChallenge(arg, { requireSlug: true });
      if (!ref?.slug) {
        return;
      }
      runInTerminal(ref.root, ["submit", ref.slug, "--remote"]);
    }),
  );
}

// resolveChallenge turns whatever invoked a command — a tree item, a
// CodeLens argument, or nothing (command palette) — into a course root and
// challenge slug, falling back to the active editor's location.
async function resolveChallenge(
  arg: unknown,
  opts: { requireSlug?: boolean; allowCourse?: boolean } = {},
): Promise<ChallengeRef | undefined> {
  if (arg instanceof ChallengeItem && arg.localDir) {
    return { root: path.dirname(arg.localDir), slug: arg.slug };
  }
  if (arg && typeof arg === "object" && "root" in arg) {
    const ref = arg as ChallengeRef;
    return { root: ref.root, slug: ref.slug };
  }

  const editor = vscode.window.activeTextEditor;
  if (editor && editor.document.uri.scheme === "file") {
    const course = await findCourseRootFor(editor.document.fileName);
    if (course) {
      const slug = challengeSlugFor(course, editor.document.fileName);
      if (slug || opts.allowCourse || !opts.requireSlug) {
        return { root: course.root, slug };
      }
    }
  }
  vscode.window.showErrorMessage(
    "Open a file inside a pulled challenge (or pick one in the Rubber Duck view) first.",
  );
  return undefined;
}

async function pullCourse(provider: CoursesProvider, item?: VariantItem): Promise<void> {
  let course: string | undefined;
  let language: string | undefined;

  if (item instanceof VariantItem) {
    course = item.course;
    language = item.language;
  } else {
    // Invoked from the palette: pick course, then language.
    const token = await loadToken();
    if (!token) {
      vscode.window.showErrorMessage("Sign in first: run Duck: Login.");
      return;
    }
    const courses = await listCourses(baseUrl(), token);
    const coursePick = await vscode.window.showQuickPick(
      courses.map((c) => ({ label: c.title, description: c.slug, course: c })),
      { placeHolder: "Course to pull" },
    );
    if (!coursePick) {
      return;
    }
    course = coursePick.course.slug;
    language =
      coursePick.course.languages.length === 1
        ? coursePick.course.languages[0]
        : await vscode.window.showQuickPick(coursePick.course.languages, {
            placeHolder: "Language variant",
          });
    if (!language) {
      return;
    }
  }

  const folder = await pickWorkspaceFolder();
  if (!folder) {
    return;
  }

  const spec = `${course}/${language}`;
  try {
    const out = await vscode.window.withProgress(
      { location: vscode.ProgressLocation.Notification, title: `duck pull ${spec}` },
      () => execDuck(folder.uri.fsPath, ["pull", spec, ...baseArgs()]),
    );
    provider.refresh();
    const dir = path.join(folder.uri.fsPath, `${course}-${language}`);
    const pick = await vscode.window.showInformationMessage(
      out.trim().split("\n").pop() ?? `Pulled ${spec}`,
      "Reveal in Explorer",
    );
    if (pick) {
      await vscode.commands.executeCommand("revealInExplorer", vscode.Uri.file(dir));
    }
  } catch (err) {
    vscode.window.showErrorMessage(`duck pull ${spec} failed: ${(err as Error).message}`);
  }
}

async function pickWorkspaceFolder(): Promise<vscode.WorkspaceFolder | undefined> {
  const folders = vscode.workspace.workspaceFolders;
  if (!folders?.length) {
    vscode.window.showErrorMessage("Open a folder first — duck pull scaffolds the course inside it.");
    return undefined;
  }
  if (folders.length === 1) {
    return folders[0];
  }
  return vscode.window.showWorkspaceFolderPick({
    placeHolder: "Folder to pull the course into",
  });
}

export function deactivate(): void {}
