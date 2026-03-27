import * as vscode from "vscode";
import { execFile } from "child_process";

export function activate(context: vscode.ExtensionContext) {
  const config = vscode.workspace.getConfiguration("gotmplfmt");
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
            const msg = stderr?.trim() || err.message;
            vscode.window.showErrorMessage(`gotmplfmt: ${msg}`);
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
