import * as vscode from 'vscode';
import { DaemonManager } from './daemonManager';
import { HooksManager } from './hooksManager';
import { StatusBarTracker } from './statusBar';
import { DashboardPanel, SidebarViewProvider } from './dashboardPanel';
import { DiagnosticsManager } from './diagnosticsManager';

let daemonManager: DaemonManager | undefined;
let hooksManager: HooksManager | undefined;
let statusBarTracker: StatusBarTracker | undefined;
let diagnosticsManager: DiagnosticsManager | undefined;

export async function activate(context: vscode.ExtensionContext) {
  daemonManager = new DaemonManager(context);
  hooksManager = new HooksManager(daemonManager);
  statusBarTracker = new StatusBarTracker(daemonManager);
  diagnosticsManager = new DiagnosticsManager(context, daemonManager, hooksManager);

  // 1. 自动启动后台服务并提示配置工作区 Hooks
  daemonManager.ensureRunning().then((running) => {
    if (running && hooksManager) {
      hooksManager.promptIfHooksMissing();
    }
  });

  // 2. 注册命令
  context.subscriptions.push(
    vscode.commands.registerCommand('agent-monitor.openDashboard', () => {
      DashboardPanel.createOrShow(daemonManager!);
    }),

    vscode.commands.registerCommand('agent-monitor.configureHooks', async () => {
      if (hooksManager) {
        await hooksManager.configureWorkspaceHooks();
      }
    }),

    vscode.commands.registerCommand('agent-monitor.restoreHooks', async () => {
      if (hooksManager) {
        await hooksManager.restoreWorkspaceHooksBackup();
      }
    }),

    vscode.commands.registerCommand('agent-monitor.restartDaemon', async () => {
      if (daemonManager) {
        vscode.window.showInformationMessage('Restarting Agent Monitor daemon...');
        const success = await daemonManager.startDaemon();
        if (success) {
          vscode.window.showInformationMessage('✓ Agent Monitor daemon restarted successfully!');
          if (statusBarTracker) {
            statusBarTracker.connectSSE();
          }
        } else {
          vscode.window.showErrorMessage('Failed to restart Agent Monitor daemon.');
        }
      }
    }),

    vscode.commands.registerCommand('agent-monitor.showDiagnostics', async () => {
      if (diagnosticsManager) {
        vscode.window.showInformationMessage('Running Agent Monitor diagnostics...');
        const report = await diagnosticsManager.runFullDiagnostics();
        if (report.summary.error > 0) {
          vscode.window.showErrorMessage(
            `Agent Monitor Diagnostics: ${report.summary.error} issue(s) detected. Check "Agent Monitor Diagnostics" Output tab.`
          );
        } else if (report.summary.warning > 0) {
          vscode.window.showWarningMessage(
            `Agent Monitor Diagnostics: ${report.summary.warning} warning(s). Check "Agent Monitor Diagnostics" Output tab.`
          );
        } else {
          vscode.window.showInformationMessage('✓ Agent Monitor Diagnostics: All health checks passed!');
        }
      }
    })
  );

  // 3. 注册侧边栏 WebviewView
  const sidebarProvider = new SidebarViewProvider(daemonManager);
  context.subscriptions.push(
    vscode.window.registerWebviewViewProvider('agent-monitor.sidebarView', sidebarProvider)
  );

  // 4. 插件 Settings 变更时：仅重启由本扩展 spawn 的 daemon
  context.subscriptions.push(
    vscode.workspace.onDidChangeConfiguration(async (e) => {
      if (!daemonManager) {
        return;
      }
      const watched = [
        'agentMonitor.llmBaseUrl',
        'agentMonitor.llmModel',
        'agentMonitor.llmApiKey',
        'agentMonitor.llmGoalEveryN',
        'agentMonitor.apiKey',
        'agentMonitor.serverUrl',
      ];
      if (!watched.some((key) => e.affectsConfiguration(key))) {
        return;
      }

      if (daemonManager.isManagedDaemon()) {
        const success = await daemonManager.startDaemon();
        if (success && statusBarTracker) {
          statusBarTracker.connectSSE();
        }
        return;
      }

      daemonManager.logUnmanagedSettingsHint();
    })
  );

  // 5. 清理注册
  context.subscriptions.push(daemonManager, statusBarTracker, diagnosticsManager);
}

export function deactivate() {
  if (daemonManager) {
    daemonManager.dispose();
  }
  if (statusBarTracker) {
    statusBarTracker.dispose();
  }
  if (diagnosticsManager) {
    diagnosticsManager.dispose();
  }
}
