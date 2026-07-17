// Rubber Duck challenge editor.
//
// This file is bundled (esbuild, IIFE) into internal/web/static/cm6.js and
// vendored into the Go binary via //go:embed. The web app has no JS bundler,
// so treat cm6.js as a committed artifact — edit THIS file and rebuild with
// `make editor-bundle` (see editor/README.md).
//
// It progressively enhances every `<textarea data-editor>` on the page into a
// CodeMirror 6 editor, keeping the textarea in sync so the plain HTML form
// POST is unchanged. If this script never runs, the textarea still submits.

import { EditorView, basicSetup } from "codemirror";
import { EditorState, Compartment } from "@codemirror/state";
import { keymap, lineNumbers } from "@codemirror/view";
import { indentWithTab } from "@codemirror/commands";
import { indentUnit } from "@codemirror/language";
import { oneDark } from "@codemirror/theme-one-dark";
import { python } from "@codemirror/lang-python";
import { cpp } from "@codemirror/lang-cpp";
import { go } from "@codemirror/lang-go";
import { javascript } from "@codemirror/lang-javascript";
import { markdown } from "@codemirror/lang-markdown";
import { vim } from "@replit/codemirror-vim";

// Language modes keyed by the `data-language` attribute (the course variant's
// language). C reuses the C/C++ grammar. Unknown languages get no grammar
// (plain text) rather than failing.
const LANGUAGES = {
  go: go,
  python: python,
  c: cpp,
  cpp: cpp,
  javascript: javascript,
  js: javascript,
  markdown: markdown,
  md: markdown,
};

// The editor stays a dark "terminal" in both site themes — matching the
// existing textarea, which was styled dark phosphor regardless of theme.
// oneDark supplies token colors; these overrides pull the chrome onto the
// site's phosphor palette (#0b0f0d bg, emerald cursor/selection).
const phosphorTheme = EditorView.theme(
  {
    "&": { backgroundColor: "#0b0f0d", color: "#e2e8f0" },
    ".cm-content": { caretColor: "#34d399" },
    ".cm-cursor, .cm-dropCursor": { borderLeftColor: "#34d399" },
    ".cm-gutters": {
      backgroundColor: "#0b0f0d",
      color: "#475569",
      border: "none",
    },
    ".cm-activeLine": { backgroundColor: "#10201788" },
    ".cm-activeLineGutter": { backgroundColor: "#10201788" },
    "&.cm-focused .cm-selectionBackground, .cm-selectionBackground, ::selection":
      { backgroundColor: "#065f4655" },
    "&.cm-focused": { outline: "none" },
    ".cm-scroller": { fontFamily: "inherit" },
  },
  { dark: true }
);

// --- persisted settings (localStorage, same convention as the theme toggle) ---
const SETTINGS_KEY = "rd.editor";
const DEFAULTS = { fontSize: 14, tabSize: 4, lineWrap: false, vim: false, relativeLines: false };

function loadSettings() {
  try {
    const s = { ...DEFAULTS, ...JSON.parse(localStorage.getItem(SETTINGS_KEY) || "{}") };
    // Stored values may be hand-edited or from an older schema; clamp the
    // numbers so a bad tabSize/fontSize can't throw during mount (e.g.
    // " ".repeat(-1)) and break every editor on the page.
    s.fontSize = clamp(parseInt(s.fontSize, 10) || DEFAULTS.fontSize, 10, 24);
    s.tabSize = clamp(parseInt(s.tabSize, 10) || DEFAULTS.tabSize, 1, 8);
    return s;
  } catch {
    return { ...DEFAULTS };
  }
}

function saveSettings(s) {
  try {
    localStorage.setItem(SETTINGS_KEY, JSON.stringify(s));
  } catch {
    // Private-mode / disabled storage: settings just won't persist.
  }
}

let settings = loadSettings();

// Every mounted editor exposes the compartments the settings panel needs to
// reconfigure, so a change to one panel applies to all editors on the page.
const editors = [];

function fontTheme(px) {
  return EditorView.theme({ "&": { fontSize: `${px}px` } });
}

function keymapExt(useVim) {
  // vim() must precede other keymaps to take priority.
  return useVim ? [vim(), keymap.of([indentWithTab])] : keymap.of([indentWithTab]);
}

// Relative line numbers (vim-style): the cursor line shows its absolute
// number, every other line its distance from the cursor. Added at higher
// precedence than basicSetup's lineNumbers(), so this formatNumber wins;
// off = empty, falling back to basicSetup's absolute gutter. The gutter
// re-renders on cursor line changes because basicSetup's
// highlightActiveLineGutter marks the active line.
function lineNumbersExt(relative) {
  if (!relative) return [];
  return lineNumbers({
    formatNumber: (n, state) => {
      const cursor = state.doc.lineAt(state.selection.main.head).number;
      return String(n === cursor ? n : Math.abs(cursor - n));
    },
  });
}

function applySettings() {
  for (const ed of editors) {
    ed.view.dispatch({
      effects: [
        ed.font.reconfigure(fontTheme(settings.fontSize)),
        ed.tab.reconfigure([EditorState.tabSize.of(settings.tabSize), indentUnit.of(" ".repeat(settings.tabSize))]),
        ed.wrap.reconfigure(settings.lineWrap ? EditorView.lineWrapping : []),
        ed.keys.reconfigure(keymapExt(settings.vim)),
        ed.nums.reconfigure(lineNumbersExt(settings.relativeLines)),
      ],
    });
  }
  saveSettings(settings);
  for (const ed of editors) syncPanel(ed.panel);
}

// --- settings panel (inline-styled: Tailwind only scans .templ files, so
// utility classes used only here would never be generated) ---
function buildPanel() {
  const bar = document.createElement("div");
  bar.style.cssText =
    "display:flex;flex-wrap:wrap;align-items:center;gap:.75rem;margin-top:.5rem;" +
    "padding:.35rem .5rem;background:#0b0f0d;border:1px solid #1e293b;border-bottom:none;" +
    "font-family:ui-monospace,monospace;font-size:12px;color:#94a3b8;";

  const mkLabel = (text) => {
    const l = document.createElement("label");
    l.style.cssText = "display:flex;align-items:center;gap:.3rem;";
    l.append(text);
    return l;
  };

  // font size
  const font = document.createElement("input");
  font.type = "number";
  font.min = "10";
  font.max = "24";
  font.step = "1";
  font.dataset.role = "font";
  font.style.cssText = "width:3.5rem;background:#0e1410;color:#e2e8f0;border:1px solid #334155;padding:.1rem .3rem;";
  const fontLabel = mkLabel("font");
  fontLabel.append(font);

  // tab size
  const tab = document.createElement("select");
  tab.dataset.role = "tab";
  tab.style.cssText = "background:#0e1410;color:#e2e8f0;border:1px solid #334155;padding:.1rem .3rem;";
  for (const n of [2, 4, 8]) {
    const o = document.createElement("option");
    o.value = String(n);
    o.textContent = String(n);
    tab.append(o);
  }
  const tabLabel = mkLabel("tab");
  tabLabel.append(tab);

  // wrap
  const wrap = document.createElement("input");
  wrap.type = "checkbox";
  wrap.dataset.role = "wrap";
  const wrapLabel = mkLabel("wrap");
  wrapLabel.append(wrap);

  // vim
  const vimBox = document.createElement("input");
  vimBox.type = "checkbox";
  vimBox.dataset.role = "vim";
  const vimLabel = mkLabel("vim");
  vimLabel.append(vimBox);

  // relative line numbers
  const rel = document.createElement("input");
  rel.type = "checkbox";
  rel.dataset.role = "rel";
  const relLabel = mkLabel("rel#");
  relLabel.append(rel);

  bar.append(fontLabel, tabLabel, wrapLabel, vimLabel, relLabel);

  const onChange = () => {
    settings = {
      fontSize: clamp(parseInt(font.value, 10) || DEFAULTS.fontSize, 10, 24),
      tabSize: parseInt(tab.value, 10) || DEFAULTS.tabSize,
      lineWrap: wrap.checked,
      vim: vimBox.checked,
      relativeLines: rel.checked,
    };
    applySettings();
  };
  font.addEventListener("change", onChange);
  tab.addEventListener("change", onChange);
  wrap.addEventListener("change", onChange);
  vimBox.addEventListener("change", onChange);
  rel.addEventListener("change", onChange);

  return bar;
}

function clamp(n, lo, hi) {
  return Math.min(hi, Math.max(lo, n));
}

function syncPanel(panel) {
  panel.querySelector('[data-role="font"]').value = String(settings.fontSize);
  panel.querySelector('[data-role="tab"]').value = String(settings.tabSize);
  panel.querySelector('[data-role="wrap"]').checked = settings.lineWrap;
  panel.querySelector('[data-role="vim"]').checked = settings.vim;
  panel.querySelector('[data-role="rel"]').checked = settings.relativeLines;
}

function mount(textarea) {
  const langFactory = LANGUAGES[(textarea.dataset.language || "").toLowerCase()];

  const font = new Compartment();
  const tab = new Compartment();
  const wrap = new Compartment();
  const keys = new Compartment();
  const nums = new Compartment();

  const view = new EditorView({
    state: EditorState.create({
      doc: textarea.value,
      extensions: [
        // Order matters: vim (inside keys) must precede basicSetup, or
        // basicSetup's history/search keymaps steal Ctrl-u/Ctrl-d before
        // vim sees them. nums precedes it so its lineNumbers config wins.
        keys.of(keymapExt(settings.vim)),
        nums.of(lineNumbersExt(settings.relativeLines)),
        basicSetup,
        langFactory ? langFactory() : [],
        oneDark,
        phosphorTheme,
        font.of(fontTheme(settings.fontSize)),
        tab.of([
          EditorState.tabSize.of(settings.tabSize),
          indentUnit.of(" ".repeat(settings.tabSize)),
        ]),
        wrap.of(settings.lineWrap ? EditorView.lineWrapping : []),
        // data-editor-height pins the editor to a fixed height with its own
        // scrollbar (long docs like course markdown); without it the editor
        // grows with its content, like the challenge textareas did.
        textarea.dataset.editorHeight
          ? EditorView.theme({
              "&": { height: textarea.dataset.editorHeight },
              ".cm-scroller": { overflow: "auto" },
            })
          : [],
        // Keep the textarea in sync so the existing form POST submits the
        // edited code with zero handler changes. The input event lets
        // listeners on the textarea (the edit page's live preview) keep
        // working as if the user typed into it directly.
        EditorView.updateListener.of((u) => {
          if (u.docChanged) {
            textarea.value = u.state.doc.toString();
            textarea.dispatchEvent(new Event("input", { bubbles: true }));
          }
        }),
      ],
    }),
  });

  const panel = buildPanel();

  // Replace the textarea's visible slot with [panel][editor]; keep the
  // textarea in the DOM (hidden) so its name="code" still posts.
  textarea.style.display = "none";
  const wrapper = document.createElement("div");
  wrapper.style.cssText = "border:1px solid #334155;margin-top:.5rem;";
  wrapper.append(view.dom);
  textarea.after(panel, wrapper);

  // Belt-and-suspenders: also flush on submit in case a change landed
  // without a doc-change event firing first.
  const form = textarea.closest("form");
  if (form) form.addEventListener("submit", () => {
    textarea.value = view.state.doc.toString();
  });

  const ed = { view, panel, font, tab, wrap, keys, nums };
  editors.push(ed);
  syncPanel(panel);
}

// On phone-sized viewports CodeMirror (and a code-sized textarea) is not a
// usable editing surface, so for textareas that opt in via data-mobile-notice
// (the challenge cards) we swap the whole editing slot — textarea and submit
// button — for a pointer to a desktop. Width is checked once at load; a
// mid-session resize across the breakpoint needs a reload, which is fine for
// the resize-to-phone-width case.
function mobileNotice(textarea) {
  textarea.style.display = "none";
  const form = textarea.closest("form");
  const submit = form ? form.querySelector('button[type="submit"]') : null;
  if (submit) submit.style.display = "none";

  const note = document.createElement("div");
  note.style.cssText =
    "margin-top:.5rem;padding:.75rem 1rem;border:1px dashed #64748b;" +
    "background:#0b0f0d;font-family:ui-monospace,monospace;font-size:13px;" +
    "line-height:1.5;color:#94a3b8;";
  note.textContent =
    "// code editing needs a bigger screen — open this page on a desktop, " +
    "or work locally with the duck CLI.";
  textarea.after(note);
}

function init() {
  const mobile = window.matchMedia("(max-width: 767px)").matches;
  document.querySelectorAll("textarea[data-editor]").forEach((t) => {
    // Guard against double-mounting if the bundle is ever included twice.
    if (t.dataset.editorMounted) return;
    t.dataset.editorMounted = "true";
    if (mobile) {
      // Only textareas that opted in (challenge code) get swapped for the
      // desktop pointer; everything else (the course markdown editor) keeps
      // its plain textarea so the form stays usable on a phone.
      if ("mobileNotice" in t.dataset) mobileNotice(t);
      return;
    }
    mount(t);
  });
}

if (document.readyState === "loading") {
  document.addEventListener("DOMContentLoaded", init);
} else {
  init();
}
