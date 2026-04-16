import * as vscode from "vscode";
import { execFile } from "child_process";

const installCommand = "go install github.com/gohugoio/gotmplfmt@latest";

export function activate(context: vscode.ExtensionContext) {
  const config = vscode.workspace.getConfiguration("gotmplfmt");
  const bin = config.get<string>("path", "gotmplfmt");

  checkBinaryExists(bin);

  const languages: string[] = config.get("languages", [
    "html",
    "gohtml",
    "go-template",
  ]);

  for (const lang of languages) {
    const provider = vscode.languages.registerDocumentFormattingEditProvider(
      { language: lang },
      new GotmplfmtFormatter()
    );
    context.subscriptions.push(provider);
  }
}

function checkBinaryExists(bin: string) {
  execFile(bin, ["--help"], (err) => {
    if (!err) {
      return;
    }
    const code = (err as NodeJS.ErrnoException).code;
    if (code !== "ENOENT") {
      return;
    }
    vscode.window
      .showWarningMessage(
        `gotmplfmt binary not found. Install it with: ${installCommand}`,
        "Install",
        "Configure Path"
      )
      .then((choice) => {
        if (choice === "Install") {
          const terminal = vscode.window.createTerminal("gotmplfmt");
          terminal.show();
          terminal.sendText(installCommand);
        } else if (choice === "Configure Path") {
          vscode.commands.executeCommand(
            "workbench.action.openSettings",
            "gotmplfmt.path"
          );
        }
      });
  });
}

export function deactivate() {}

class GotmplfmtFormatter
  implements vscode.DocumentFormattingEditProvider
{
  provideDocumentFormattingEdits(
    document: vscode.TextDocument
  ): vscode.ProviderResult<vscode.TextEdit[]> {
    const config = vscode.workspace.getConfiguration("gotmplfmt");
    const bin = config.get<string>("path", "gotmplfmt");
    const text = document.getText();

    return new Promise((resolve, reject) => {
      const proc = execFile(
        bin,
        [],
        { timeout: 10000, maxBuffer: 10 * 1024 * 1024 },
        (err, stdout, stderr) => {
          if (err) {
            const code = (err as NodeJS.ErrnoException).code;
            if (code === "ENOENT") {
              checkBinaryExists(bin);
            } else {
              const msg = stderr?.trim() || err.message;
              vscode.window.showErrorMessage(`gotmplfmt: ${msg}`);
            }
            return resolve([]);
          }
          if (stdout === text) {
            return resolve([]);
          }
          const fullRange = new vscode.Range(
            document.positionAt(0),
            document.positionAt(text.length)
          );
          resolve([vscode.TextEdit.replace(fullRange, stdout)]);
        }
      );
      proc.stdin?.end(text);
    });
  }
}
