// depdog VS Code extension: thin glue that runs `depdog lsp` (an LSP server
// over stdio) and lets vscode-languageclient do the rest. The server publishes
// depdog's rule violations as diagnostics on import lines and answers hover
// with the `depdog explain` verdict; see docs/lsp.md in the depdog repo.
'use strict';

const vscode = require('vscode');
const { LanguageClient } = require('vscode-languageclient/node');

// The seven depdog adapter languages, as VS Code language identifiers.
const LANGUAGES = [
  'go',
  'typescript',
  'javascript',
  'typescriptreact',
  'javascriptreact',
  'python',
  'rust',
  'java',
  'ruby',
  'kotlin',
];

let client;

function activate(context) {
  const serverOptions = {
    command: 'depdog', // must be on PATH
    args: ['lsp'],
  };
  const clientOptions = {
    documentSelector: LANGUAGES.map((language) => ({ scheme: 'file', language })),
    // Belt and braces: the server also registers its own depdog.yaml watcher
    // dynamically (workspace/didChangeWatchedFiles); this covers clients or
    // sessions where that registration is unavailable.
    synchronize: {
      fileEvents: vscode.workspace.createFileSystemWatcher('**/depdog.yaml'),
    },
  };
  client = new LanguageClient('depdog', 'depdog', serverOptions, clientOptions);
  client.start();
  context.subscriptions.push(client);
}

function deactivate() {
  return client ? client.stop() : undefined;
}

module.exports = { activate, deactivate };
