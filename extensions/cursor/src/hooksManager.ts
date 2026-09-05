import * as vscode from 'vscode';
import * as path from 'path';
import * as fs from 'fs';
import * as os from 'os';
import { DaemonManager } from './daemonManager';
export * from './binaryResolver';

export const STANDARD_CURSOR_EVENTS = [
  'sessionStart',
  'beforeSubmitPrompt',
  'preToolUse',
  'postToolUseFailure',
  'beforeShellExecution',
  'beforeMCPExecution',
  'subagentStart',
  'afterAgentResponse',
  'stop',
  'sessionEnd',
] as const;

export type CursorHookEvent = (typeof STANDARD_CURSOR_EVENTS)[number];

export interface HookCommandItem {
  command: string;
  [key: string]: any;
}

export interface CursorHooksConfig {
  version?: number;
  hooks?: Record<string, HookCommandItem[]>;
  [key: string]: any;
}

export interface CustomHookItem {
  event: string;
  command: string;
}

export interface FolderHookStatus {
  folderPath: string;
  folderName: string;
  hooksFilePath: string;
  backupFilePath: string;
  exists: boolean;
  hasBackup: boolean;
  needsUpdate: boolean;
  configuredEvents: string[];
  missingEvents: string[];
  customHooks: CustomHookItem[];
  currentReporterPath: string | null;
}

export interface MergeHooksResult {
  mergedConfig: CursorHooksConfig;
  addedCount: number;
  updatedCount: number;
  preservedCustomCount: number;
  hasChanges: boolean;
}

export function isAgentReporterCommand(command: string): boolean {
  if (!command || typeof command !== 'string') {
    return false;
  }
  const trimmed = command.trim();
  return trimmed.includes('agent-reporter');
}

export function extractExecutableFromCommand(commandStr: string): string {
  const trimmed = (commandStr || '').trim();
  if (trimmed.startsWith('"')) {
    const nextQuote = trimmed.indexOf('"', 1);
    if (nextQuote !== -1) {
      return trimmed.slice(1, nextQuote);
    }
  } else if (trimmed.startsWith("'")) {
    const nextQuote = trimmed.indexOf("'", 1);
    if (nextQuote !== -1) {
      return trimmed.slice(1, nextQuote);
    }
  }
  return trimmed.split(/\s+/)[0] || '';
}

export function normalizeReporterExecutable(rawPath: string): string {
  const normalized = path.normalize(rawPath.trim());
  const isQuoted =
    (normalized.startsWith('"') && normalized.endsWith('"')) ||
    (normalized.startsWith("'") && normalized.endsWith("'"));
  if (/\s/.test(normalized) && !isQuoted) {
    return `"${normalized}"`;
  }
  return normalized;
}

export function buildReporterCommand(
  reporterExecutable: string,
  event: string,
  agentName: string = 'Cursor Agent'
): string {
  const exec = normalizeReporterExecutable(reporterExecutable);
  return `${exec} --event ${event} --agent "${agentName}"`;
}

export function mergeHooks(
  existingConfig: CursorHooksConfig | null | undefined,
  reporterCmd: string,
  agentName: string = 'Cursor Agent'
): MergeHooksResult {
  const existingHooks: Record<string, HookCommandItem[]> =
    existingConfig && existingConfig.hooks && typeof existingConfig.hooks === 'object'
      ? existingConfig.hooks
      : {};

  let addedCount = 0;
  let updatedCount = 0;
  let preservedCustomCount = 0;

  const mergedHooks: Record<string, HookCommandItem[]> = { ...existingHooks };

  for (const evt of STANDARD_CURSOR_EVENTS) {
    const expectedCmd = buildReporterCommand(reporterCmd, evt, agentName);
    const expectedItem: HookCommandItem = { command: expectedCmd };
    const currentList = mergedHooks[evt];

    if (!Array.isArray(currentList) || currentList.length === 0) {
      mergedHooks[evt] = [expectedItem];
      addedCount++;
    } else {
      const customItems: HookCommandItem[] = [];
      const agentItems: HookCommandItem[] = [];

      for (const item of currentList) {
        if (item && typeof item === 'object' && item.command && isAgentReporterCommand(item.command)) {
          agentItems.push(item);
        } else {
          customItems.push(item);
        }
      }

      preservedCustomCount += customItems.length;

      if (agentItems.length === 0) {
        mergedHooks[evt] = [...customItems, expectedItem];
        addedCount++;
      } else {
        const isUpToDate = agentItems.length === 1 && agentItems[0].command === expectedCmd;
        if (!isUpToDate) {
          updatedCount++;
        }
        mergedHooks[evt] = [...customItems, expectedItem];
      }
    }
  }

  for (const [evt, list] of Object.entries(existingHooks)) {
    if (!STANDARD_CURSOR_EVENTS.includes(evt as any) && Array.isArray(list)) {
      preservedCustomCount += list.length;
    }
  }

  const mergedConfig: CursorHooksConfig = {
    version: 1,
    ...(existingConfig || {}),
    hooks: mergedHooks,
  };

  const hasChanges =
    !existingConfig ||
    JSON.stringify(existingConfig) !== JSON.stringify(mergedConfig);

  return {
    mergedConfig,
    addedCount,
    updatedCount,
    preservedCustomCount,
    hasChanges,
  };
}

export function inspectFolderHookStatus(
  folderPath: string,
  folderName: string,
  currentReporterPath: string
): FolderHookStatus {
  const cursorDir = path.join(folderPath, '.cursor');
  const hooksFilePath = path.join(cursorDir, 'hooks.json');
  const backupFilePath = path.join(cursorDir, 'hooks.json.bak');

  const exists = fs.existsSync(hooksFilePath);
  const hasBackup = fs.existsSync(backupFilePath);

  if (!exists) {
    return {
      folderPath,
      folderName,
      hooksFilePath,
      backupFilePath,
      exists: false,
      hasBackup,
      needsUpdate: true,
      configuredEvents: [],
      missingEvents: [...STANDARD_CURSOR_EVENTS],
      customHooks: [],
      currentReporterPath: null,
    };
  }

  try {
    const raw = fs.readFileSync(hooksFilePath, 'utf-8');
    const json: CursorHooksConfig = JSON.parse(raw);
    const hooks = json.hooks || {};

    const configuredEvents: string[] = [];
    const missingEvents: string[] = [];
    const customHooks: CustomHookItem[] = [];
    let detectedReporterPath: string | null = null;
    let needsUpdate = false;

    for (const evt of STANDARD_CURSOR_EVENTS) {
      const list = hooks[evt];
      if (!Array.isArray(list) || list.length === 0) {
        missingEvents.push(evt);
        needsUpdate = true;
        continue;
      }

      let hasAgentReporter = false;
      for (const item of list) {
        if (item && typeof item === 'object' && item.command) {
          if (isAgentReporterCommand(item.command)) {
            hasAgentReporter = true;
            const exec = extractExecutableFromCommand(item.command);
            if (!detectedReporterPath && exec) {
              detectedReporterPath = exec;
            }
            const expectedCmd = buildReporterCommand(currentReporterPath, evt);
            if (item.command !== expectedCmd) {
              needsUpdate = true;
            }
          } else {
            customHooks.push({ event: evt, command: item.command });
          }
        }
      }

      if (hasAgentReporter) {
        configuredEvents.push(evt);
      } else {
        missingEvents.push(evt);
        needsUpdate = true;
      }
    }

    for (const [evt, list] of Object.entries(hooks)) {
      if (!STANDARD_CURSOR_EVENTS.includes(evt as any) && Array.isArray(list)) {
        for (const item of list) {
          if (item && item.command) {
            customHooks.push({ event: evt, command: item.command });
          }
        }
      }
    }

    return {
      folderPath,
      folderName,
      hooksFilePath,
      backupFilePath,
      exists: true,
      hasBackup,
      needsUpdate,
      configuredEvents,
      missingEvents,
      customHooks,
      currentReporterPath: detectedReporterPath,
    };
  } catch (_) {
    return {
      folderPath,
      folderName,
      hooksFilePath,
      backupFilePath,
      exists: true,
      hasBackup,
      needsUpdate: true,
      configuredEvents: [],
      missingEvents: [...STANDARD_CURSOR_EVENTS],
      customHooks: [],
      currentReporterPath: null,
    };
  }
}

export function getUserHooksPath(): string {
  return path.join(os.homedir(), '.cursor', 'hooks.json');
}

export function detectUserLevelAgentHooks(userHooksPath: string = getUserHooksPath()): string[] {
  try {
    if (!fs.existsSync(userHooksPath)) {
      return [];
    }
    const content = fs.readFileSync(userHooksPath, 'utf-8');
    const json: CursorHooksConfig = JSON.parse(content);
    const duplicateEvents: string[] = [];
    if (json.hooks && typeof json.hooks === 'object') {
      for (const [evt, list] of Object.entries(json.hooks)) {
        if (Array.isArray(list)) {
          for (const item of list) {
            if (item && typeof item === 'object' && item.command && isAgentReporterCommand(item.command)) {
              duplicateEvents.push(evt);
              break;
            }
          }
        }
      }
    }
    return duplicateEvents;
  } catch (_) {
    return [];
  }
}

export function createBackupFile(hooksFilePath: string): boolean {
  try {
    if (fs.existsSync(hooksFilePath)) {
      const backupPath = `${hooksFilePath}.bak`;
      fs.copyFileSync(hooksFilePath, backupPath);
      return true;
    }
    return false;
  } catch (err) {
    console.error(`Failed to create backup: ${err}`);
    return false;
  }
}

export function restoreBackupFile(hooksFilePath: string): boolean {
  try {
    const backupPath = `${hooksFilePath}.bak`;
    if (fs.existsSync(backupPath)) {
      fs.copyFileSync(backupPath, hooksFilePath);
      return true;
    }
    return false;
  } catch (err) {
    console.error(`Failed to restore backup: ${err}`);
    return false;
  }
}

export class HooksManager {
  constructor(private daemonManager: DaemonManager) {}

  public getReporterCommand(): string {
    const resolvedPath = this.daemonManager.resolveBinaryPath('agent-reporter');
    return resolvedPath || 'agent-reporter';
  }

  public async configureWorkspaceHooks(): Promise<void> {
    const folders = vscode.workspace.workspaceFolders;
    if (!folders || folders.length === 0) {
      vscode.window.showWarningMessage('No workspace folder open. Please open a folder to configure Cursor hooks.');
      return;
    }

    const reporterCmd = this.getReporterCommand();
    this.warnIfUserLevelHooksExist();

    if (folders.length === 1) {
      await this.configureSingleFolder(folders[0], reporterCmd, true);
      return;
    }

    // Multi-root workspace traversal & selection
    const statuses = folders.map((f) => inspectFolderHookStatus(f.uri.fsPath, f.name, reporterCmd));
    const allItem: vscode.QuickPickItem = {
      label: `$(checklist) Configure All Workspace Folders`,
      description: `${folders.length} folders`,
      detail: `Batch configure hooks safely across all workspaces with non-destructive merge and backup`,
    };

    const folderItems: (vscode.QuickPickItem & { folder: vscode.WorkspaceFolder })[] = folders.map((f, idx) => {
      const st = statuses[idx];
      let icon = '$(check)';
      let statusDesc = 'Configured';
      if (!st.exists) {
        icon = '$(circle-slash)';
        statusDesc = 'Missing .cursor/hooks.json';
      } else if (st.needsUpdate) {
        icon = '$(sync)';
        statusDesc = 'Needs update (outdated or missing events)';
      }
      const customNote = st.customHooks.length > 0 ? ` (${st.customHooks.length} custom hooks preserved)` : '';
      return {
        folder: f,
        label: `${icon} ${f.name}`,
        description: statusDesc,
        detail: `${f.uri.fsPath}${customNote}`,
      };
    });

    const selected = await vscode.window.showQuickPick([allItem, ...folderItems], {
      placeHolder: 'Select workspace folder to configure Cursor lifecycle hooks',
      title: 'Agent Monitor: Multi-Root Workspace Hook Configuration',
    });

    if (!selected) {
      return;
    }

    if (selected === allItem || (selected && selected.label && selected.label.includes('Configure All Workspace Folders'))) {
      await this.configureAllFolders(folders, reporterCmd);
    } else {
      const pickedFolder = (selected as any).folder as vscode.WorkspaceFolder;
      if (pickedFolder) {
        await this.configureSingleFolder(pickedFolder, reporterCmd, true);
      }
    }
  }

  public async configureAllFolders(folders: readonly vscode.WorkspaceFolder[], reporterCmd: string): Promise<void> {
    let configuredCount = 0;
    let totalCustomPreserved = 0;
    const createdNewHooksFiles: string[] = [];

    for (const folder of folders) {
      const folderPath = folder.uri.fsPath;
      const cursorDir = path.join(folderPath, '.cursor');
      const hooksFilePath = path.join(cursorDir, 'hooks.json');

      try {
        if (!fs.existsSync(cursorDir)) {
          fs.mkdirSync(cursorDir, { recursive: true });
        }

        let existingConfig: CursorHooksConfig | null = null;
        const fileExisted = fs.existsSync(hooksFilePath);
        if (fileExisted) {
          createBackupFile(hooksFilePath);
          try {
            existingConfig = JSON.parse(fs.readFileSync(hooksFilePath, 'utf-8'));
          } catch (_) {}
        } else {
          createdNewHooksFiles.push(hooksFilePath);
        }

        const mergeResult = mergeHooks(existingConfig, reporterCmd);
        fs.writeFileSync(hooksFilePath, JSON.stringify(mergeResult.mergedConfig, null, 2), 'utf-8');
        configuredCount++;
        totalCustomPreserved += mergeResult.preservedCustomCount;
      } catch (err) {
        vscode.window.showErrorMessage(`Failed to configure hooks in "${folder.name}": ${err}`);
      }
    }

    const note = totalCustomPreserved > 0 ? ` (${totalCustomPreserved} custom hooks preserved)` : '';
    const choice = await vscode.window.showInformationMessage(
      `✓ Successfully updated Cursor hooks across ${configuredCount} workspace folder(s)${note}! Backups saved as .cursor/hooks.json.bak.`,
      'Undo All'
    );

    if (choice === 'Undo All') {
      await this.restoreWorkspaceHooksBackup();
      for (const newlyCreated of createdNewHooksFiles) {
        try {
          if (fs.existsSync(newlyCreated)) {
            fs.unlinkSync(newlyCreated);
          }
        } catch (_) {}
      }
    }
  }

  public async configureSingleFolder(
    folder: vscode.WorkspaceFolder,
    reporterCmd: string,
    allowDiffPreview: boolean
  ): Promise<boolean> {
    const folderPath = folder.uri.fsPath;
    const cursorDir = path.join(folderPath, '.cursor');
    const hooksFilePath = path.join(cursorDir, 'hooks.json');

    try {
      if (!fs.existsSync(cursorDir)) {
        fs.mkdirSync(cursorDir, { recursive: true });
      }

      let existingConfig: CursorHooksConfig | null = null;
      const fileExisted = fs.existsSync(hooksFilePath);
      if (fileExisted) {
        try {
          existingConfig = JSON.parse(fs.readFileSync(hooksFilePath, 'utf-8'));
        } catch (_) {}
      }

      const mergeResult = mergeHooks(existingConfig, reporterCmd);

      if (!mergeResult.hasChanges) {
        vscode.window.showInformationMessage(`✓ Cursor hooks in "${folder.name}" are already up to date.`);
        return true;
      }

      if (allowDiffPreview && fileExisted) {
        const choice = await vscode.window.showInformationMessage(
          `Agent Monitor: Proposed updates for "${folder.name}" .cursor/hooks.json (${mergeResult.preservedCustomCount} custom hooks preserved).`,
          'Apply Changes',
          'Preview Diff',
          'Cancel'
        );

        if (choice === 'Cancel' || !choice) {
          return false;
        }

        if (choice === 'Preview Diff') {
          const proposedFilePath = path.join(cursorDir, 'hooks.json.proposed');
          fs.writeFileSync(proposedFilePath, JSON.stringify(mergeResult.mergedConfig, null, 2), 'utf-8');
          try {
            await vscode.commands.executeCommand(
              'vscode.diff',
              vscode.Uri.file(hooksFilePath),
              vscode.Uri.file(proposedFilePath),
              `${folder.name}: Current hooks.json ↔ Proposed hooks.json`
            );
          } catch (_) {}

          const confirm = await vscode.window.showInformationMessage(
            `Apply proposed changes to "${folder.name}" .cursor/hooks.json?`,
            'Apply Changes',
            'Cancel'
          );

          try {
            if (fs.existsSync(proposedFilePath)) {
              fs.unlinkSync(proposedFilePath);
            }
          } catch (_) {}

          if (confirm !== 'Apply Changes') {
            return false;
          }
        }
      }

      if (fileExisted) {
        createBackupFile(hooksFilePath);
      }

      fs.writeFileSync(hooksFilePath, JSON.stringify(mergeResult.mergedConfig, null, 2), 'utf-8');

      const customNote = mergeResult.preservedCustomCount > 0
        ? ` (${mergeResult.preservedCustomCount} custom hook commands preserved)`
        : '';
      const backupNote = fileExisted ? ' (Backup created at .cursor/hooks.json.bak)' : '';

      const undoActionLabel = fileExisted ? 'Undo (Restore Backup)' : 'Undo (Remove Created Hooks)';
      const userAction = await vscode.window.showInformationMessage(
        `✓ Successfully updated Cursor hooks at ${folder.name}/.cursor/hooks.json!${customNote}${backupNote}`,
        undoActionLabel
      );

      if (userAction === undoActionLabel) {
        if (fileExisted) {
          if (restoreBackupFile(hooksFilePath)) {
            vscode.window.showInformationMessage(`✓ Restored hooks from backup for "${folder.name}".`);
          } else {
            vscode.window.showErrorMessage(`Failed to restore backup: backup file not found.`);
          }
        } else {
          try {
            if (fs.existsSync(hooksFilePath)) {
              fs.unlinkSync(hooksFilePath);
              vscode.window.showInformationMessage(`✓ Undone: removed newly created .cursor/hooks.json for "${folder.name}".`);
            }
          } catch (err) {
            vscode.window.showErrorMessage(`Failed to remove .cursor/hooks.json: ${err}`);
          }
        }
      }

      return true;
    } catch (err) {
      vscode.window.showErrorMessage(`Failed to configure hooks in "${folder.name}": ${err}`);
      return false;
    }
  }

  public async restoreWorkspaceHooksBackup(targetFolder?: vscode.WorkspaceFolder): Promise<void> {
    const folders = vscode.workspace.workspaceFolders;
    if (!folders || folders.length === 0) {
      vscode.window.showWarningMessage('No workspace folder open.');
      return;
    }

    if (targetFolder) {
      const hooksFilePath = path.join(targetFolder.uri.fsPath, '.cursor', 'hooks.json');
      if (restoreBackupFile(hooksFilePath)) {
        vscode.window.showInformationMessage(`✓ Successfully restored hooks from backup in "${targetFolder.name}".`);
      } else {
        vscode.window.showErrorMessage(`No backup file found (.cursor/hooks.json.bak) in "${targetFolder.name}".`);
      }
      return;
    }

    const foldersWithBak = folders.filter((f) => {
      const bakPath = path.join(f.uri.fsPath, '.cursor', 'hooks.json.bak');
      return fs.existsSync(bakPath);
    });

    if (foldersWithBak.length === 0) {
      vscode.window.showInformationMessage('No .cursor/hooks.json.bak backup files found in open workspaces.');
      return;
    }

    if (foldersWithBak.length === 1) {
      const f = foldersWithBak[0];
      const choice = await vscode.window.showInformationMessage(
        `Restore .cursor/hooks.json from backup in "${f.name}"?`,
        'Restore Backup',
        'Cancel'
      );
      if (choice === 'Restore Backup') {
        const hooksPath = path.join(f.uri.fsPath, '.cursor', 'hooks.json');
        restoreBackupFile(hooksPath);
        vscode.window.showInformationMessage(`✓ Successfully restored hooks from backup in "${f.name}".`);
      }
      return;
    }

    const allItem: vscode.QuickPickItem = {
      label: `$(history) Restore All Backups (${foldersWithBak.length} folders)`,
      description: 'Restore .cursor/hooks.json.bak in all workspaces',
    };

    const items: (vscode.QuickPickItem & { folder: vscode.WorkspaceFolder })[] = foldersWithBak.map((f) => ({
      folder: f,
      label: `$(folder) ${f.name}`,
      description: 'Backup available (.cursor/hooks.json.bak)',
      detail: f.uri.fsPath,
    }));

    const selected = await vscode.window.showQuickPick([allItem, ...items], {
      placeHolder: 'Select workspace folder to restore from backup',
      title: 'Agent Monitor: Restore Hooks Backup',
    });

    if (!selected) {
      return;
    }

    if (selected === allItem || (selected && selected.label && selected.label.includes('Restore All Backups'))) {
      let restoredCount = 0;
      for (const f of foldersWithBak) {
        const hooksPath = path.join(f.uri.fsPath, '.cursor', 'hooks.json');
        if (restoreBackupFile(hooksPath)) {
          restoredCount++;
        }
      }
      vscode.window.showInformationMessage(`✓ Restored hooks from backup across ${restoredCount} workspace folder(s).`);
    } else {
      const picked = (selected as any).folder as vscode.WorkspaceFolder;
      if (picked) {
        const hooksPath = path.join(picked.uri.fsPath, '.cursor', 'hooks.json');
        restoreBackupFile(hooksPath);
        vscode.window.showInformationMessage(`✓ Successfully restored hooks from backup in "${picked.name}".`);
      }
    }
  }

  public warnIfUserLevelHooksExist(): void {
    const userEvents = detectUserLevelAgentHooks();
    if (userEvents.length > 0) {
      vscode.window.showWarningMessage(
        `Agent Monitor: ~/.cursor/hooks.json also configures agent-reporter for events (${userEvents.join(
          ', '
        )}). Having both user-level and workspace-level hooks will cause duplicate events. Consider keeping only workspace or user hooks.`
      );
    }
  }

  public async promptIfHooksMissing(): Promise<void> {
    const folders = vscode.workspace.workspaceFolders;
    if (!folders || folders.length === 0) {
      return;
    }

    const reporterCmd = this.getReporterCommand();
    const foldersNeedingUpdate: vscode.WorkspaceFolder[] = [];

    for (const folder of folders) {
      const status = inspectFolderHookStatus(folder.uri.fsPath, folder.name, reporterCmd);
      if (status.needsUpdate) {
        foldersNeedingUpdate.push(folder);
      }
    }

    if (foldersNeedingUpdate.length === 0) {
      return;
    }

    if (folders.length === 1) {
      const f = folders[0];
      const status = inspectFolderHookStatus(f.uri.fsPath, f.name, reporterCmd);
      const promptText = !status.exists
        ? 'Agent Monitor: Cursor lifecycle hooks are not configured for this workspace. Would you like to enable them now?'
        : 'Agent Monitor: Cursor extension was updated. Update workspace .cursor/hooks.json with latest binary paths?';

      const choice = await vscode.window.showInformationMessage(
        promptText,
        'Configure Hooks (.cursor/hooks.json)',
        'Not Now'
      );
      if (choice === 'Configure Hooks (.cursor/hooks.json)') {
        await this.configureSingleFolder(f, reporterCmd, false);
      }
      return;
    }

    // Multi-root workspace notification
    const choice = await vscode.window.showInformationMessage(
      `Agent Monitor: Cursor lifecycle hooks need configuration in ${foldersNeedingUpdate.length} of ${folders.length} workspace folders. Would you like to enable them now?`,
      'Configure All Folders',
      'Select Folders...',
      'Not Now'
    );

    if (choice === 'Configure All Folders') {
      await this.configureAllFolders(foldersNeedingUpdate, reporterCmd);
    } else if (choice === 'Select Folders...') {
      await this.configureWorkspaceHooks();
    }
  }
}
