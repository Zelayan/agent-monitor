import * as vscode from 'vscode';
import * as http from 'http';
import { DaemonManager } from './daemonManager';

export class StatusBarTracker {
  private statusBarItem: vscode.StatusBarItem;
  private req: http.ClientRequest | null = null;
  private isDisposed = false;
  private activeTasksCount = 0;
  private activeStartTime = 0;
  private timerHandle: NodeJS.Timeout | null = null;

  constructor(private daemonManager: DaemonManager) {
    this.statusBarItem = vscode.window.createStatusBarItem(
      vscode.StatusBarAlignment.Right,
      100
    );
    this.statusBarItem.command = 'agent-monitor.openDashboard';
    this.statusBarItem.text = '$(pulse) Agent Monitor';
    this.statusBarItem.tooltip = 'Click to open Agent Monitor Dashboard';

    const config = vscode.workspace.getConfiguration('agentMonitor');
    if (config.get<boolean>('statusBarEnabled', true)) {
      this.statusBarItem.show();
    }

    this.startTimer();
    this.connectSSE();
  }

  private startTimer() {
    this.timerHandle = setInterval(() => {
      if (this.activeTasksCount > 0 && this.activeStartTime > 0) {
        const sec = Math.max(0, Math.floor((Date.now() - this.activeStartTime) / 1000));
        const m = String(Math.floor(sec / 60)).padStart(2, '0');
        const s = String(sec % 60).padStart(2, '0');
        this.statusBarItem.text = `$(sync~spin) Agent: ${m}:${s} (${this.activeTasksCount})`;
        this.statusBarItem.backgroundColor = new vscode.ThemeColor('statusBarItem.warningBackground');
      } else if (this.activeTasksCount === 0) {
        this.statusBarItem.text = `$(check) Agent: Idle`;
        this.statusBarItem.backgroundColor = undefined;
      }
    }, 1000);
  }

  public connectSSE() {
    if (this.isDisposed) return;
    if (this.req) {
      this.req.destroy();
      this.req = null;
    }

    try {
      const serverUrl = this.daemonManager.getServerUrl();
      const config = vscode.workspace.getConfiguration('agentMonitor');
      const apiKey = config.get<string>('apiKey', '');

      let streamPath = '/api/stream';
      if (apiKey) {
        streamPath += `?token=${encodeURIComponent(apiKey)}`;
      }

      const u = new URL(streamPath, serverUrl);
      const req = http.request(u.href, {
        headers: {
          Accept: 'text/event-stream',
          'Cache-Control': 'no-cache',
        },
      }, (res) => {
        let buffer = '';
        res.on('data', (chunk) => {
          buffer += chunk.toString();
          const lines = buffer.split('\n');
          buffer = lines.pop() || '';

          for (const line of lines) {
            if (line.startsWith('data: ')) {
              const jsonStr = line.slice(6).trim();
              try {
                const data = JSON.parse(jsonStr);
                this.handleTaskUpdate(data);
              } catch (_) {}
            }
          }
        });

        res.on('end', () => this.scheduleReconnect());
        res.on('error', () => this.scheduleReconnect());
      });

      req.on('error', () => this.scheduleReconnect());
      req.end();
      this.req = req;
    } catch (_) {
      this.scheduleReconnect();
    }
  }

  private handleTaskUpdate(data: any) {
    if (data && data.status) {
      // 协同停止：当检测到会话处于 abort_requested 状态，尝试在 IDE 内部触发 Cursor 官方取消操作
      if (data.controlState === 'abort_requested' && data.status === 'running') {
        vscode.commands.executeCommand('workbench.action.chat.cancel').then(undefined, () => {});
      }

      if (data.status === 'running') {
        this.activeTasksCount = 1;
        this.activeStartTime = data.activeRunStart || data.startTime || Date.now();
      } else if (data.status === 'completed' || data.status === 'failed') {
        this.activeTasksCount = 0;
        this.activeStartTime = 0;
        if (data.status === 'failed') {
          this.statusBarItem.text = `$(error) Agent: Failed`;
          this.statusBarItem.backgroundColor = new vscode.ThemeColor('statusBarItem.errorBackground');
        }
      }
    }
  }

  private scheduleReconnect() {
    if (this.isDisposed) return;
    setTimeout(() => {
      this.connectSSE();
    }, 4000);
  }

  public dispose() {
    this.isDisposed = true;
    if (this.timerHandle) {
      clearInterval(this.timerHandle);
      this.timerHandle = null;
    }
    if (this.req) {
      this.req.destroy();
      this.req = null;
    }
    this.statusBarItem.dispose();
  }
}
