import * as vscode from "vscode";
import { execFile } from "child_process";

export function duckPath(): string {
  return vscode.workspace.getConfiguration("duck").get<string>("path", "duck");
}

export function baseUrl(): string {
  return vscode.workspace
    .getConfiguration("duck")
    .get<string>("baseUrl", "https://duckgc.com")
    .replace(/\/+$/, "");
}

// baseArgs returns --base only when the user configured a non-default
// server, keeping commands copy-paste friendly in the common case.
export function baseArgs(): string[] {
  const base = baseUrl();
  return base === "https://duckgc.com" ? [] : ["--base", base];
}

let terminal: vscode.Terminal | undefined;

// shellQuote joins command parts into a single line, single-quoting anything
// the shell could interpret. Every sendText of config-derived values (duck.path,
// duck.baseUrl) must go through this — those settings are attacker-influenced
// text as far as the shell is concerned.
export function shellQuote(parts: string[]): string {
  const quote = (s: string) => (/[^\w@%+=:,./-]/.test(s) ? `'${s.replace(/'/g, `'\\''`)}'` : s);
  return parts.map(quote).join(" ");
}

// runInTerminal runs a duck command in a shared, visible terminal so test
// and submit output streams where the user can read and rerun it.
export function runInTerminal(cwd: string, args: string[]): void {
  if (!terminal || terminal.exitStatus !== undefined) {
    terminal = vscode.window.createTerminal({ name: "duck", cwd });
  }
  terminal.show(true);
  terminal.sendText(`cd ${shellQuote([cwd])} && ${shellQuote([duckPath(), ...args])}`);
}

export function disposeTerminal(t: vscode.Terminal): void {
  if (t === terminal) {
    terminal = undefined;
  }
}

// execDuck runs a non-interactive duck command to completion (used for
// pull, where we want to refresh the tree and offer next steps after).
export function execDuck(cwd: string, args: string[]): Promise<string> {
  return new Promise((resolve, reject) => {
    execFile(duckPath(), args, { cwd, timeout: 120_000 }, (err, stdout, stderr) => {
      if (err) {
        const detail = (stderr || stdout || err.message).trim();
        if ((err as NodeJS.ErrnoException).code === "ENOENT") {
          reject(
            new Error(
              `duck CLI not found (${duckPath()}) — install it or set the "duck.path" setting`,
            ),
          );
          return;
        }
        reject(new Error(detail));
        return;
      }
      resolve(stdout);
    });
  });
}
