import * as vscode from 'vscode';
import * as path from 'path';
import * as fs from 'fs';
import { DaemonManager } from './daemonManager';

export class HooksManager {
  constructor(private daemonManager: DaemonManager) {}

  public async configureWorkspaceHooks(): Promise<void> {
    const folders = vscode.workspace.workspaceFolders;
    if (!folders || folders.length === 0) {
      vscode.window.showWarningMessage('No workspace folder open. Please open a folder to configure Cursor hooks.');
      return;
    }

    const rootPath = folders[0].uri.fsPath;
    const cursorDir = path.join(rootPath, '.cursor');
    const hooksFile = path.join(cursorDir, 'hooks.json');

    // 寻找本地 reporter 路径；若找到绝对路径则使用绝对路径，避免用户终端 PATH 缺失
    let reporterCmd = 'agent-reporter';
    const resolvedPath = this.daemonManager.resolveBinaryPath('agent-reporter');
    if (resolvedPath) {
      reporterCmd = resolvedPath;
    }

    const hooksConfig = {
      version: 1,
      hooks: {
        sessionStart: [{ command: `${reporterCmd} --event sessionStart --agent "Cursor Agent"` }],
        beforeSubmitPrompt: [{ command: `${reporterCmd} --event beforeSubmitPrompt --agent "Cursor Agent"` }],
        preToolUse: [{ command: `${reporterCmd} --event preToolUse --agent "Cursor Agent"` }],
        postToolUseFailure: [{ command: `${reporterCmd} --event postToolUseFailure --agent "Cursor Agent"` }],
        beforeShellExecution: [{ command: `${reporterCmd} --event beforeShellExecution --agent "Cursor Agent"` }],
        beforeMCPExecution: [{ command: `${reporterCmd} --event beforeMCPExecution --agent "Cursor Agent"` }],
        subagentStart: [{ command: `${reporterCmd} --event subagentStart --agent "Cursor Agent"` }],
        afterAgentResponse: [{ command: `${reporterCmd} --event afterAgentResponse --agent "Cursor Agent"` }],
        stop: [{ command: `${reporterCmd} --event stop --agent "Cursor Agent"` }],
        sessionEnd: [{ command: `${reporterCmd} --event sessionEnd --agent "Cursor Agent"` }],
      },
    };

    try {
      if (!fs.existsSync(cursorDir)) {
        fs.mkdirSync(cursorDir, { recursive: true });
      }

      let existingHooks: any = {};
      if (fs.existsSync(hooksFile)) {
        try {
          const content = fs.readFileSync(hooksFile, 'utf-8');
          existingHooks = JSON.parse(content);
        } catch (_) {}
      }

      // 智能合并现有配置
      const merged = {
        version: 1,
        ...existingHooks,
        hooks: {
          ...(existingHooks.hooks || {}),
          ...hooksConfig.hooks,
        },
      };

      fs.writeFileSync(hooksFile, JSON.stringify(merged, null, 2), 'utf-8');
      vscode.window.showInformationMessage(`✓ Successfully configured Cursor hooks at .cursor/hooks.json!`);
    } catch (err) {
      vscode.window.showErrorMessage(`Failed to configure hooks: ${err}`);
    }
  }

  public async promptIfHooksMissing(): Promise<void> {
    const folders = vscode.workspace.workspaceFolders;
    if (!folders || folders.length === 0) return;

    const hooksFile = path.join(folders[0].uri.fsPath, '.cursor', 'hooks.json');
    if (!fs.existsSync(hooksFile)) {
      const choice = await vscode.window.showInformationMessage(
        'Agent Monitor: Cursor lifecycle hooks are not configured for this workspace. Would you like to enable them now?',
        'Configure Hooks (.cursor/hooks.json)',
        'Not Now'
      );
      if (choice === 'Configure Hooks (.cursor/hooks.json)') {
        await this.configureWorkspaceHooks();
      }
    }
  }
}
