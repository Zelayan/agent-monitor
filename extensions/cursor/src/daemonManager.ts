import * as vscode from 'vscode';
import * as http from 'http';
import * as path from 'path';
import * as fs from 'fs';
import { spawn, ChildProcess } from 'child_process';

export class DaemonManager {
  private childProcess: ChildProcess | null = null;
  private outputChannel: vscode.OutputChannel;
  private serverUrl: string;

  constructor(private context: vscode.ExtensionContext) {
    this.outputChannel = vscode.window.createOutputChannel('Agent Monitor');
    const config = vscode.workspace.getConfiguration('agentMonitor');
    this.serverUrl = config.get<string>('serverUrl', 'http://127.0.0.1:8000').replace(/\/+$/, '');
  }

  public getServerUrl(): string {
    return this.serverUrl;
  }

  public isManagedDaemon(): boolean {
    return this.childProcess !== null;
  }

  public logUnmanagedSettingsHint(): void {
    const msg =
      'Settings changed, but the running Agent Monitor process was not started by this extension (e.g. systemd). Run "Agent Monitor: Restart Backend Daemon", or stop the external service so the extension can launch it with the new settings.';
    this.outputChannel.appendLine(`[DaemonManager] ${msg}`);
    void vscode.window.showInformationMessage(msg);
  }

  public refreshServerUrlFromConfig(): void {
    const config = vscode.workspace.getConfiguration('agentMonitor');
    this.serverUrl = config.get<string>('serverUrl', 'http://127.0.0.1:8000').replace(/\/+$/, '');
  }

  public async ensureRunning(): Promise<boolean> {
    const isLive = await this.pingServer();
    if (isLive) {
      this.outputChannel.appendLine(`[DaemonManager] Existing Agent Monitor daemon detected at ${this.serverUrl}`);
      return true;
    }

    const config = vscode.workspace.getConfiguration('agentMonitor');
    const autoStart = config.get<boolean>('autoStartDaemon', true);
    if (!autoStart) {
      return false;
    }

    return this.startDaemon();
  }

  public async pingServer(): Promise<boolean> {
    return new Promise((resolve) => {
      try {
        const u = new URL('/api/tasks', this.serverUrl);
        const req = http.get(u.href, { timeout: 800 }, (res) => {
          resolve(res.statusCode !== undefined && res.statusCode < 500);
        });
        req.on('error', () => resolve(false));
        req.on('timeout', () => {
          req.destroy();
          resolve(false);
        });
      } catch (_) {
        resolve(false);
      }
    });
  }

  public async startDaemon(): Promise<boolean> {
    if (this.childProcess) {
      this.stopDaemon();
    }

    // 优先寻找扩展目录下的二进制或系统 PATH 中的二进制
    const binaryPath = this.resolveBinaryPath('agent-monitor');
    if (!binaryPath) {
      this.outputChannel.appendLine('[DaemonManager] agent-monitor binary not found in extension or PATH. Running in remote/external mode.');
      return false;
    }

    this.outputChannel.appendLine(`[DaemonManager] Starting agent-monitor daemon from: ${binaryPath}`);

    try {
      this.refreshServerUrlFromConfig();
      const port = new URL(this.serverUrl).port || '8000';
      const env = { ...process.env, PORT: port };

      const config = vscode.workspace.getConfiguration('agentMonitor');
      const apiKey = (config.get<string>('apiKey', '') || '').trim();
      if (apiKey) {
        env['AGENT_MONITOR_API_KEY'] = apiKey;
      }

      const llmBaseUrl = (config.get<string>('llmBaseUrl', '') || '').trim();
      const llmModel = (config.get<string>('llmModel', '') || '').trim();
      const llmApiKey = (config.get<string>('llmApiKey', '') || '').trim();
      if (llmBaseUrl) {
        env['AGENT_MONITOR_LLM_BASE_URL'] = llmBaseUrl;
      }
      if (llmModel) {
        env['AGENT_MONITOR_LLM_MODEL'] = llmModel;
      }
      if (llmApiKey) {
        env['AGENT_MONITOR_LLM_API_KEY'] = llmApiKey;
      }
      if (llmBaseUrl && llmModel) {
        this.outputChannel.appendLine(
          `[DaemonManager] LLM session titles enabled (model=${llmModel}, base=${llmBaseUrl})`
        );
      }

      this.childProcess = spawn(binaryPath, [], {
        env,
        stdio: ['ignore', 'pipe', 'pipe'],
      });

      this.childProcess.stdout?.on('data', (data) => {
        this.outputChannel.append(`[Server] ${data.toString()}`);
      });

      this.childProcess.stderr?.on('data', (data) => {
        this.outputChannel.append(`[Server ERR] ${data.toString()}`);
      });

      this.childProcess.on('exit', (code, signal) => {
        this.outputChannel.appendLine(`[DaemonManager] Daemon exited with code ${code}, signal ${signal}`);
        this.childProcess = null;
      });

      // 循环重试确认服务存活
      for (let i = 0; i < 15; i++) {
        await new Promise((r) => setTimeout(r, 200));
        if (await this.pingServer()) {
          this.outputChannel.appendLine('[DaemonManager] Daemon started successfully and listening!');
          return true;
        }
      }

      return false;
    } catch (err) {
      this.outputChannel.appendLine(`[DaemonManager] Failed to start daemon: ${err}`);
      return false;
    }
  }

  public stopDaemon() {
    if (this.childProcess) {
      this.outputChannel.appendLine('[DaemonManager] Stopping background daemon...');
      try {
        this.childProcess.kill('SIGTERM');
      } catch (_) {}
      this.childProcess = null;
    }
  }

  public resolveBinaryPath(name: string): string | null {
    const ext = process.platform === 'win32' ? '.exe' : '';
    const binName = name + ext;

    // 1. 检查扩展自带的 bin 目录
    const localBin = path.join(this.context.extensionPath, 'bin', binName);
    if (fs.existsSync(localBin)) {
      return localBin;
    }

    // 2. 检查常见系统路径
    const systemPaths = [
      '/usr/local/bin',
      '/usr/bin',
      path.join(process.env.HOME || '', '.local', 'bin'),
      path.join(process.env.HOME || '', 'go', 'bin'),
    ];

    for (const p of systemPaths) {
      const full = path.join(p, binName);
      if (fs.existsSync(full)) {
        return full;
      }
    }

    return null;
  }

  public dispose() {
    this.stopDaemon();
    this.outputChannel.dispose();
  }
}
