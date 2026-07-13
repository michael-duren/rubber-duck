import * as vscode from "vscode";
import * as path from "path";
import { languageFiles } from "./api";
import { challengeSlugFor, findCourseRootFor } from "./local";

const solutionFiles = new Set(Object.values(languageFiles).map((f) => f.code));

// DuckCodeLensProvider puts Test / Submit lenses at the top of solution
// files inside a pulled course tree.
export class DuckCodeLensProvider implements vscode.CodeLensProvider {
  async provideCodeLenses(doc: vscode.TextDocument): Promise<vscode.CodeLens[]> {
    if (doc.uri.scheme !== "file" || !solutionFiles.has(path.basename(doc.fileName))) {
      return [];
    }
    const course = await findCourseRootFor(doc.fileName);
    if (!course) {
      return [];
    }
    const slug = challengeSlugFor(course, doc.fileName);
    if (!slug) {
      return [];
    }
    const range = new vscode.Range(0, 0, 0, 0);
    const arg = { root: course.root, slug };
    return [
      new vscode.CodeLens(range, {
        command: "duck.test",
        title: "duck test",
        arguments: [arg],
      }),
      new vscode.CodeLens(range, {
        command: "duck.submit",
        title: "duck submit",
        arguments: [arg],
      }),
      new vscode.CodeLens(range, {
        command: "duck.submitRemote",
        title: "duck submit --remote",
        arguments: [arg],
      }),
    ];
  }
}
