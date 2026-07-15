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
import { keymap } from "@codemirror/view";
import { indentWithTab } from "@codemirror/commands";
import { indentUnit } from "@codemirror/language";
import { oneDark } from "@codemirror/theme-one-dark";
import { python } from "@codemirror/lang-python";
import { cpp } from "@codemirror/lang-cpp";
import { go } from "@codemirror/lang-go";
import { javascript } from "@codemirror/lang-javascript";
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
const DEFAULTS = { fontSize: 14, tabSize: 4, lineWrap: false, vim: false };

function loadSettings() {
  try {
    return { ...DEFAULTS, ...JSON.parse(localStorage.getItem(SETTINGS_KEY) || "{}") };
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

function applySettings() {
  for (const ed of editors) {
    ed.view.dispatch({
      effects: [
        ed.font.reconfigure(fontTheme(settings.fontSize)),
        ed.tab.reconfigure([EditorState.tabSize.of(settings.tabSize), indentUnit.of(" ".repeat(settings.tabSize))]),
        ed.wrap.reconfigure(settings.lineWrap ? EditorView.lineWrapping : []),
        ed.keys.reconfigure(keymapExt(settings.vim)),
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

  bar.append(fontLabel, tabLabel, wrapLabel, vimLabel);

  const onChange = () => {
    settings = {
      fontSize: clamp(parseInt(font.value, 10) || DEFAULTS.fontSize, 10, 24),
      tabSize: parseInt(tab.value, 10) || DEFAULTS.tabSize,
      lineWrap: wrap.checked,
      vim: vimBox.checked,
    };
    applySettings();
  };
  font.addEventListener("change", onChange);
  tab.addEventListener("change", onChange);
  wrap.addEventListener("change", onChange);
  vimBox.addEventListener("change", onChange);

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
}

function mount(textarea) {
  const langFactory = LANGUAGES[(textarea.dataset.language || "").toLowerCase()];

  const font = new Compartment();
  const tab = new Compartment();
  const wrap = new Compartment();
  const keys = new Compartment();

  const view = new EditorView({
    state: EditorState.create({
      doc: textarea.value,
      extensions: [
        basicSetup,
        keys.of(keymapExt(settings.vim)),
        langFactory ? langFactory() : [],
        oneDark,
        phosphorTheme,
        font.of(fontTheme(settings.fontSize)),
        tab.of([
          EditorState.tabSize.of(settings.tabSize),
          indentUnit.of(" ".repeat(settings.tabSize)),
        ]),
        wrap.of(settings.lineWrap ? EditorView.lineWrapping : []),
        // Keep the textarea in sync so the existing form POST submits the
        // edited code with zero handler changes.
        EditorView.updateListener.of((u) => {
          if (u.docChanged) textarea.value = u.state.doc.toString();
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

  const ed = { view, panel, font, tab, wrap, keys };
  editors.push(ed);
  syncPanel(panel);
}

function init() {
  document.querySelectorAll("textarea[data-editor]").forEach(mount);
}

if (document.readyState === "loading") {
  document.addEventListener("DOMContentLoaded", init);
} else {
  init();
}
