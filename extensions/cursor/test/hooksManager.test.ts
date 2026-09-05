import test from 'node:test';
import assert from 'node:assert';
import * as fs from 'fs';
import * as path from 'path';
import * as os from 'os';
import * as mockVscode from './vscodeMock';
import {
  STANDARD_CURSOR_EVENTS,
  isAgentReporterCommand,
  extractExecutableFromCommand,
  normalizeReporterExecutable,
  buildReporterCommand,
  mergeHooks,
  inspectFolderHookStatus,
  createBackupFile,
  restoreBackupFile,
  detectUserLevelAgentHooks,
  HooksManager,
} from '../src/hooksManager';

function makeTempDir(prefix: string): string {
  return fs.mkdtempSync(path.join(os.tmpdir(), `wp21-test-${prefix}-`));
}

test('normalizeReporterExecutable and buildReporterCommand', () => {
  // Path without spaces
  const normalPath = '/usr/local/bin/agent-reporter';
  assert.strictEqual(normalizeReporterExecutable(normalPath), path.normalize(normalPath));

  // Path with spaces should be wrapped in double quotes
  const spacedPath = '/Users/John Doe/bin/agent-reporter';
  const expectedQuoted = `"${path.normalize(spacedPath)}"`;
  assert.strictEqual(normalizeReporterExecutable(spacedPath), expectedQuoted);

  // Already quoted path should not get double quoted
  assert.strictEqual(normalizeReporterExecutable(expectedQuoted), expectedQuoted);
  const singleQuoted = `'${path.normalize(spacedPath)}'`;
  assert.strictEqual(normalizeReporterExecutable(singleQuoted), singleQuoted);

  // buildReporterCommand generates valid CLI invocations
  const cmd = buildReporterCommand('/usr/bin/agent-reporter', 'sessionStart', 'Cursor Agent');
  assert.strictEqual(cmd, `${path.normalize('/usr/bin/agent-reporter')} --event sessionStart --agent "Cursor Agent"`);
});

test('isAgentReporterCommand and extractExecutableFromCommand', () => {
  assert.strictEqual(isAgentReporterCommand('agent-reporter --event sessionStart'), true);
  assert.strictEqual(isAgentReporterCommand('/usr/local/bin/agent-reporter --event stop'), true);
  assert.strictEqual(isAgentReporterCommand('"C:\\Program Files\\agent-reporter.exe" --event sessionStart'), true);
  assert.strictEqual(isAgentReporterCommand('npm test'), false);
  assert.strictEqual(isAgentReporterCommand('eslint .'), false);
  assert.strictEqual(isAgentReporterCommand(''), false);

  assert.strictEqual(
    extractExecutableFromCommand('agent-reporter --event sessionStart'),
    'agent-reporter'
  );
  assert.strictEqual(
    extractExecutableFromCommand('"C:\\Program Files\\agent-reporter.exe" --event sessionStart'),
    'C:\\Program Files\\agent-reporter.exe'
  );
  assert.strictEqual(
    extractExecutableFromCommand("'/opt/bin/agent-reporter' --event sessionStart"),
    '/opt/bin/agent-reporter'
  );
});

test('mergeHooks: fresh configuration creates all standard events', () => {
  const result = mergeHooks(null, 'agent-reporter');
  assert.strictEqual(result.addedCount, 10);
  assert.strictEqual(result.updatedCount, 0);
  assert.strictEqual(result.preservedCustomCount, 0);
  assert.strictEqual(result.hasChanges, true);
  assert.strictEqual(result.mergedConfig.version, 1);

  for (const evt of STANDARD_CURSOR_EVENTS) {
    const list = result.mergedConfig.hooks?.[evt];
    assert.ok(Array.isArray(list), `Expected array for event ${evt}`);
    assert.strictEqual(list.length, 1);
    assert.ok(list[0].command.includes(`--event ${evt}`));
  }

  // Second merge with same reporter path is idempotent
  const second = mergeHooks(result.mergedConfig, 'agent-reporter');
  assert.strictEqual(second.hasChanges, false);
  assert.strictEqual(second.addedCount, 0);
  assert.strictEqual(second.updatedCount, 0);
});

test('mergeHooks: non-destructive merge preserves custom user hooks and custom fields', () => {
  const existing = {
    version: 1,
    customField: 'keep-me',
    hooks: {
      sessionStart: [
        { command: 'echo "Custom session start hook"' },
        { command: '/old/agent-reporter --event sessionStart --agent "Old"' },
      ],
      preToolUse: [
        { command: 'npm run lint-check', flags: { autoFix: false } },
      ],
      customEvent: [
        { command: 'do-something-special' },
      ],
    },
  };

  const reporterCmd = '/new/bin/agent-reporter';
  const result = mergeHooks(existing, reporterCmd);

  assert.strictEqual(result.hasChanges, true);
  assert.strictEqual(result.mergedConfig.customField, 'keep-me');

  // sessionStart: custom hook preserved, agent-reporter updated
  const sessionStartHooks = result.mergedConfig.hooks?.sessionStart!;
  assert.strictEqual(sessionStartHooks.length, 2);
  assert.strictEqual(sessionStartHooks[0].command, 'echo "Custom session start hook"');
  assert.strictEqual(sessionStartHooks[1].command, buildReporterCommand(reporterCmd, 'sessionStart'));

  // preToolUse: custom hook preserved, new agent-reporter appended
  const preToolHooks = result.mergedConfig.hooks?.preToolUse!;
  assert.strictEqual(preToolHooks.length, 2);
  assert.strictEqual(preToolHooks[0].command, 'npm run lint-check');
  assert.deepStrictEqual((preToolHooks[0] as any).flags, { autoFix: false });
  assert.strictEqual(preToolHooks[1].command, buildReporterCommand(reporterCmd, 'preToolUse'));

  // customEvent: untouched
  assert.deepStrictEqual(result.mergedConfig.hooks?.customEvent, [{ command: 'do-something-special' }]);

  // All 10 standard events are present
  for (const evt of STANDARD_CURSOR_EVENTS) {
    assert.ok(result.mergedConfig.hooks?.[evt], `Event ${evt} should exist`);
  }
  assert.strictEqual(result.preservedCustomCount, 3);
});

test('inspectFolderHookStatus, backup, and restore', () => {
  const tmpDir = makeTempDir('inspect-test');
  try {
    const reporterCmd = '/bin/agent-reporter';

    // 1. Initial status on empty folder
    const status1 = inspectFolderHookStatus(tmpDir, 'test-ws', reporterCmd);
    assert.strictEqual(status1.exists, false);
    assert.strictEqual(status1.needsUpdate, true);
    assert.strictEqual(status1.hasBackup, false);
    assert.strictEqual(status1.configuredEvents.length, 0);
    assert.strictEqual(status1.missingEvents.length, 10);

    // 2. Configure hooks
    const cursorDir = path.join(tmpDir, '.cursor');
    fs.mkdirSync(cursorDir, { recursive: true });
    const hooksFile = path.join(cursorDir, 'hooks.json');
    const mergeRes = mergeHooks(null, reporterCmd);
    fs.writeFileSync(hooksFile, JSON.stringify(mergeRes.mergedConfig, null, 2), 'utf-8');

    const status2 = inspectFolderHookStatus(tmpDir, 'test-ws', reporterCmd);
    assert.strictEqual(status2.exists, true);
    assert.strictEqual(status2.needsUpdate, false);
    assert.strictEqual(status2.configuredEvents.length, 10);
    assert.strictEqual(status2.missingEvents.length, 0);

    // 3. Backup creation and restore
    assert.strictEqual(createBackupFile(hooksFile), true);
    assert.strictEqual(fs.existsSync(`${hooksFile}.bak`), true);

    // Corrupt or overwrite hooksFile
    fs.writeFileSync(hooksFile, '{"corrupted": true}', 'utf-8');
    assert.strictEqual(restoreBackupFile(hooksFile), true);
    const restored = JSON.parse(fs.readFileSync(hooksFile, 'utf-8'));
    assert.strictEqual(restored.version, 1);
    assert.ok(restored.hooks.sessionStart);
  } finally {
    fs.rmSync(tmpDir, { recursive: true, force: true });
  }
});

test('detectUserLevelAgentHooks detects user-level duplicate events', () => {
  const tmpDir = makeTempDir('user-hooks');
  try {
    const userHooksFile = path.join(tmpDir, 'hooks.json');
    const mockUserHooks = {
      version: 1,
      hooks: {
        sessionStart: [{ command: '/opt/agent-reporter --event sessionStart' }],
        stop: [{ command: '/opt/agent-reporter --event stop' }],
        preToolUse: [{ command: 'echo custom' }],
      },
    };
    fs.writeFileSync(userHooksFile, JSON.stringify(mockUserHooks), 'utf-8');

    const duplicates = detectUserLevelAgentHooks(userHooksFile);
    assert.deepStrictEqual(duplicates.sort(), ['sessionStart', 'stop'].sort());

    const emptyDuplicates = detectUserLevelAgentHooks(path.join(tmpDir, 'non-existent.json'));
    assert.deepStrictEqual(emptyDuplicates, []);
  } finally {
    fs.rmSync(tmpDir, { recursive: true, force: true });
  }
});

test('HooksManager: multi-root workspace traversal, batch configuration, and restore', async () => {
  const dirA = makeTempDir('rootA');
  const dirB = makeTempDir('rootB');

  try {
    mockVscode.resetMock();
    const folderA: mockVscode.MockWorkspaceFolder = { uri: { fsPath: dirA }, name: 'WorkspaceA', index: 0 };
    const folderB: mockVscode.MockWorkspaceFolder = { uri: { fsPath: dirB }, name: 'WorkspaceB', index: 1 };
    mockVscode.workspace.workspaceFolders = [folderA, folderB];

    const mockDaemonManager: any = {
      resolveBinaryPath: (_name: string) => '/mock/agent-reporter',
    };

    const manager = new HooksManager(mockDaemonManager);

    // 1. Batch configure all folders
    mockVscode.window.quickPickSelection = {
      label: '$(checklist) Configure All Workspace Folders',
    };

    await manager.configureWorkspaceHooks();

    // Verify both folders now have .cursor/hooks.json configured
    const hooksA = path.join(dirA, '.cursor', 'hooks.json');
    const hooksB = path.join(dirB, '.cursor', 'hooks.json');
    assert.strictEqual(fs.existsSync(hooksA), true);
    assert.strictEqual(fs.existsSync(hooksB), true);

    const parsedA = JSON.parse(fs.readFileSync(hooksA, 'utf-8'));
    assert.strictEqual(parsedA.version, 1);
    assert.strictEqual(parsedA.hooks.sessionStart[0].command, '/mock/agent-reporter --event sessionStart --agent "Cursor Agent"');

    // 2. Modify one folder and test backup & restore
    fs.writeFileSync(hooksA, '{"modified": true}', 'utf-8');
    assert.strictEqual(createBackupFile(hooksA), true);

    // Call restoreWorkspaceHooksBackup
    await manager.restoreWorkspaceHooksBackup(folderA as any);
    const restoredA = JSON.parse(fs.readFileSync(hooksA, 'utf-8'));
    assert.strictEqual(restoredA.modified, true);

    // 3. Test promptIfHooksMissing in multi-root environment
    // Remove hooks from dirB to make it need update
    fs.rmSync(hooksB);
    mockVscode.resetMock();
    mockVscode.workspace.workspaceFolders = [folderA, folderB];
    mockVscode.window.infoMessageSelection = 'Configure All Folders';

    await manager.promptIfHooksMissing();
    assert.strictEqual(fs.existsSync(hooksB), true);
  } finally {
    fs.rmSync(dirA, { recursive: true, force: true });
    fs.rmSync(dirB, { recursive: true, force: true });
  }
});

test('mergeHooks: deduplicates redundant agent-reporter hooks in single event', () => {
  const existing = {
    version: 1,
    hooks: {
      sessionStart: [
        { command: '/old/agent-reporter --event sessionStart' },
        { command: '/another/agent-reporter --event sessionStart' },
        { command: 'echo "Keep custom"' },
      ],
    },
  };

  const result = mergeHooks(existing, 'agent-reporter');
  const sessionHooks = result.mergedConfig.hooks?.sessionStart!;
  assert.strictEqual(sessionHooks.length, 2);
  // Custom hook kept
  assert.strictEqual(sessionHooks[0].command, 'echo "Keep custom"');
  // Only 1 agent-reporter command now
  assert.strictEqual(sessionHooks[1].command, 'agent-reporter --event sessionStart --agent "Cursor Agent"');
});

test('HooksManager: single workspace folder configure and diff cancellation', async () => {
  const dir = makeTempDir('single-root');
  try {
    mockVscode.resetMock();
    const folder: mockVscode.MockWorkspaceFolder = { uri: { fsPath: dir }, name: 'SingleWorkspace', index: 0 };
    mockVscode.workspace.workspaceFolders = [folder];

    const mockDaemonManager: any = {
      resolveBinaryPath: (_name: string) => '/usr/local/bin/agent-reporter',
    };

    const manager = new HooksManager(mockDaemonManager);

    // Initial configuration on fresh workspace
    await manager.configureWorkspaceHooks();
    const hooksPath = path.join(dir, '.cursor', 'hooks.json');
    assert.strictEqual(fs.existsSync(hooksPath), true);

    // Calling again when already up to date should report up to date
    mockVscode.resetMock();
    mockVscode.workspace.workspaceFolders = [folder];
    await manager.configureWorkspaceHooks();
    assert.ok(mockVscode.window.infoMessages.some((msg) => msg.includes('already up to date')));
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

test('HooksManager: undo on newly created hooks removes file cleanly', async () => {
  const dir = makeTempDir('undo-new-root');
  try {
    mockVscode.resetMock();
    const folder: mockVscode.MockWorkspaceFolder = { uri: { fsPath: dir }, name: 'UndoWorkspace', index: 0 };
    mockVscode.workspace.workspaceFolders = [folder];

    const mockDaemonManager: any = {
      resolveBinaryPath: (_name: string) => '/usr/local/bin/agent-reporter',
    };

    const manager = new HooksManager(mockDaemonManager);
    mockVscode.window.infoMessageSelection = 'Undo (Remove Created Hooks)';

    await manager.configureSingleFolder(folder as any, '/usr/local/bin/agent-reporter', false);
    const hooksPath = path.join(dir, '.cursor', 'hooks.json');
    assert.strictEqual(fs.existsSync(hooksPath), false, 'Hooks file should be deleted on undo of new configuration');
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});


