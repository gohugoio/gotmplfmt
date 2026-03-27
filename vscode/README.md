# gotmplfmt VSCode Extension

Formats Go templates using the [gotmplfmt](https://github.com/gohugoio/gotmplfmt) CLI.

## Prerequisites

The `gotmplfmt` binary must be installed and on your `PATH`:

```bash
go install github.com/gohugoio/gotmplfmt@latest
```

Or set `gotmplfmt.path` in your VSCode settings to the full path of the binary.

## Settings

| Setting              | Default                              | Description                              |
| -------------------- | ------------------------------------ | ---------------------------------------- |
| `gotmplfmt.path`     | `"gotmplfmt"`                        | Path to the gotmplfmt binary.            |
| `gotmplfmt.languages`| `["html", "gohtml", "go-template"]`  | Language IDs to register formatting for. |

To use gotmplfmt as the default formatter for HTML files, add to your `settings.json`:

```json
"[html]": {
  "editor.defaultFormatter": "gohugoio.gotmplfmt"
}
```

## Local Development

```bash
# From the repo root, install the CLI:
go install .

# Build and install the extension:
cd vscode
npm install
npm run compile
code --install-extension gotmplfmt-*.vsix || npx vsce package && code --install-extension gotmplfmt-*.vsix
```

Or press `F5` in VSCode with this folder open to launch an Extension Development Host.

## Publishing

### First-time setup

1. Create a publisher at https://marketplace.visualstudio.com/manage (sign in with a Microsoft account).
2. Create a Personal Access Token (PAT) at https://dev.azure.com — scope it to **Marketplace > Manage**.
3. Log in locally:
   ```bash
   npx vsce login gohugoio
   ```

### Publish a new version

```bash
cd vscode
npm run compile
npx vsce publish minor   # or: patch, major, 0.2.0
```

This bumps the version in `package.json`, creates the VSIX, and publishes it.

### Updating after CLI changes

The extension calls the `gotmplfmt` binary at runtime — users get CLI improvements by updating their Go binary (`go install github.com/gohugoio/gotmplfmt@latest`). Only publish a new extension version when the extension code itself changes.
