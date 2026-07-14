# Rubber Duck for VS Code

Browse the [Rubber Duck](https://duckgc.com) course catalog, pull a course's
challenges into your workspace, and run `duck test` / `duck submit` without
leaving the editor. The extension is a thin UI over the `duck` CLI — install
it first and make sure it's on your PATH (or point the `duck.path` setting
at it).

## Features

- **Courses view** (duck icon in the activity bar): every published course,
  its language variants, and each variant's challenges with point values.
  Variants you've already pulled are marked, and their challenges open the
  local `solution.*` file on click.
- **Pull**: the download icon on a variant (or *Duck: Pull Course* from the
  palette) runs `duck pull <course>/<language>` into a workspace folder.
- **Test / Submit**: CodeLens actions at the top of every solution file,
  inline icons on pulled challenges in the tree, and *Duck: Test / Submit*
  palette commands that detect the challenge from the active editor. Output
  streams into a shared `duck` terminal. `duck submit` grades locally and
  claims the verdict instantly; *Submit (Grade on Server)* uses `--remote`.
- **Login**: *Duck: Login* opens a terminal running `duck login` (it prompts
  for credentials interactively). The extension reads the same token the CLI
  uses — `$DUCK_TOKEN` or `~/.config/duck/token` — to browse the catalog.

## Settings

| Setting        | Default               | Purpose                                   |
| -------------- | --------------------- | ----------------------------------------- |
| `duck.path`    | `duck`                | Path to the duck CLI binary               |
| `duck.baseUrl` | `https://duckgc.com`  | Server base URL (passed as `--base` too)  |

## Development

```sh
npm install
npm run compile   # or: npm run watch
```

Press F5 in VS Code to launch an Extension Development Host, or build a
.vsix with `npm run package` and install it via *Extensions: Install from
VSIX*.
