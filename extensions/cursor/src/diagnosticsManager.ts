import * as vscode from 'vscode';
import * as http from 'http';
import * as path from 'path';
import * as fs from 'fs';
import { DaemonManager } from './daemonManager';
import { HooksManager, inspectFolderHookStatus } from './hooksManager';
import {
  getPlatformArchKey,
  isPlatformSupported,
  resolveBinary,
  ResolvedBinary,
} from './binaryResolver';

export interface DiagnosticItem {
  category: 'daemon' | 'hooks' | 'binary' | 'sse';
  status: 'ok' | 'warning' | 'error';
  title: string;
  detail: string;
  recommendation?: string;
}

export interface DiagnosticReport {
  timestamp: string;
  platform: string;
  items: DiagnosticItem[];
  summary: {
    total: number;
    ok: number;
    warning: number;
    error: number;
  };
}

export class DiagnosticsManager {
  private outputChannel: vscode.OutputChannel;

  constructor(
    private context: vscode.ExtensionContext,
    private daemonManager: DaemonManager,
    private hooksManager: HooksManager
  ) {
    this.outputChannel = vscode.window.createOutputChannel('Agent Monitor Diagnostics');
  }

  public getOutputChannel(): vscode.OutputChannel {
    return this.outputChannel;
  }

  public log(level: 'INFO' | 'WARN' | 'ERROR', message: string): void {
    const time = new Date().toISOString().substring(11, 19);
    this.outputChannel.appendLine(`[${time}] [${level}] ${message}`);
  }

  public async runFullDiagnostics(): Promise<DiagnosticReport> {
    this.log('INFO', 'Starting Agent Monitor full diagnostics...');
    const items: DiagnosticItem[] = [];
    const platformKey = getPlatformArchKey();

    // 1. Binary checks (agent-monitor and agent-reporter)
    const wsRoots = vscode.workspace.workspaceFolders?.map((f) => f.uri.fsPath);
    const monitorResolved = resolveBinary('agent-monitor', {
      extensionPath: this.context.extensionPath,
      workspaceRoots: wsRoots,
    });
    this.evaluateBinary('agent-monitor', monitorResolved, platformKey, items);

    const reporterResolved = resolveBinary('agent-reporter', {
      extensionPath: this.context.extensionPath,
      workspaceRoots: wsRoots,
    });
    this.evaluateBinary('agent-reporter', reporterResolved, platformKey, items);

    // 2. Daemon connectivity & HTTP endpoints (/healthz, /readyz, /metrics)
    const serverUrl = this.daemonManager.getServerUrl();
    await this.evaluateDaemonHealth(serverUrl, items);

    // 3. SSE Stream connectivity
    await this.evaluateSSEStream(serverUrl, items);

    // 4. Workspace Hooks (.cursor/hooks.json)
    this.evaluateWorkspaceHooks(items);

    const summary = {
      total: items.length,
      ok: items.filter((i) => i.status === 'ok').length,
      warning: items.filter((i) => i.status === 'warning').length,
      error: items.filter((i) => i.status === 'error').length,
    };

    const report: DiagnosticReport = {
      timestamp: new Date().toISOString(),
      platform: platformKey,
      items,
      summary,
    };

    this.printReportToOutput(report);
    return report;
  }

  private evaluateBinary(
    name: string,
    resolved: ResolvedBinary | null,
    platformKey: string,
    items: DiagnosticItem[]
  ): void {
    if (!resolved) {
      const supported = isPlatformSupported(platformKey);
      items.push({
        category: 'binary',
        status: 'error',
        title: `Binary '${name}' not found`,
        detail: `Could not locate ${name} executable for platform '${platformKey}'.`,
        recommendation: supported
          ? 'Reinstall the extension or compile binaries locally using `make build`.'
          : 'Compile from source (`go install github.com/Zelayan/agent-monitor@latest`) and place on system PATH.',
      });
      return;
    }

    if (resolved.checksumVerified === false) {
      items.push({
        category: 'binary',
        status: 'warning',
        title: `Binary '${name}' SHA-256 mismatch`,
        detail: `Path: ${resolved.path} (source: ${resolved.source}). Checksum differs from bundle manifest.`,
        recommendation: 'Binary may have been modified or locally built.',
      });
      return;
    }

    items.push({
      category: 'binary',
      status: 'ok',
      title: `Binary '${name}' resolved`,
      detail: `${resolved.path} (source: ${resolved.source}, platform: ${resolved.platformKey})`,
    });
  }

  private async evaluateDaemonHealth(serverUrl: string, items: DiagnosticItem[]): Promise<void> {
    const endpoints = [
      { path: '/healthz', name: 'Liveness Probe (/healthz)' },
      { path: '/readyz', name: 'Readiness Probe (/readyz)' },
      { path: '/metrics', name: 'Metrics Endpoint (/metrics)' },
    ];

    for (const ep of endpoints) {
      const res = await this.httpGet(serverUrl, ep.path, 1200);
      if (res.ok) {
        items.push({
          category: 'daemon',
          status: 'ok',
          title: ep.name,
          detail: `HTTP ${res.status}: ${res.body.trim().substring(0, 80)}`,
        });
      } else {
        items.push({
          category: 'daemon',
          status: res.status === 0 ? 'error' : 'warning',
          title: ep.name,
          detail: res.error || `HTTP ${res.status}`,
          recommendation:
            res.status === 0
              ? 'Ensure agent-monitor daemon is running (`agent-monitor.restartDaemon`).'
              : 'Daemon returned non-200 status. Check backend logs.',
        });
      }
    }
  }

  private async evaluateSSEStream(serverUrl: string, items: DiagnosticItem[]): Promise<void> {
    const sseRes = await new Promise<{ ok: boolean; error?: string }>((resolve) => {
      try {
        const u = new URL('/api/events', serverUrl);
        const req = http.get(
          u.href,
          {
            headers: { Accept: 'text/event-stream' },
            timeout: 1500,
          },
          (res) => {
            const isSSE = (res.headers['content-type'] || '').includes('text/event-stream');
            if (res.statusCode === 200 && isSSE) {
              res.destroy();
              resolve({ ok: true });
            } else {
              res.destroy();
              resolve({
                ok: false,
                error: `Expected 200 text/event-stream, got ${res.statusCode} ${res.headers['content-type']}`,
              });
            }
          }
        );
        req.on('error', (err) => resolve({ ok: false, error: err.message }));
        req.on('timeout', () => {
          req.destroy();
          resolve({ ok: false, error: 'SSE connection timed out' });
        });
      } catch (err: any) {
        resolve({ ok: false, error: err?.message || String(err) });
      }
    });

    if (sseRes.ok) {
      items.push({
        category: 'sse',
        status: 'ok',
        title: 'SSE Event Stream (/api/events)',
        detail: 'Connected successfully and negotiated text/event-stream protocol.',
      });
    } else {
      items.push({
        category: 'sse',
        status: 'warning',
        title: 'SSE Event Stream (/api/events)',
        detail: sseRes.error || 'Failed to establish SSE stream',
        recommendation: 'Check network proxies or port conflicts preventing SSE streaming.',
      });
    }
  }

  private evaluateWorkspaceHooks(items: DiagnosticItem[]): void {
    const folders = vscode.workspace.workspaceFolders;
    if (!folders || folders.length === 0) {
      items.push({
        category: 'hooks',
        status: 'warning',
        title: 'No Workspace Folders Open',
        detail: 'Cursor hooks can only be evaluated when a workspace folder is open.',
        recommendation: 'Open a project folder in Cursor.',
      });
      return;
    }

    const reporterCmd = this.hooksManager.getReporterCommand();
    for (const folder of folders) {
      const status = inspectFolderHookStatus(folder.uri.fsPath, folder.name, reporterCmd);
      if (!status.exists) {
        items.push({
          category: 'hooks',
          status: 'warning',
          title: `Hooks Missing in '${folder.name}'`,
          detail: `.cursor/hooks.json does not exist.`,
          recommendation: 'Run command "Agent Monitor: Configure Workspace Hooks".',
        });
      } else if (status.needsUpdate) {
        items.push({
          category: 'hooks',
          status: 'warning',
          title: `Hooks Incomplete in '${folder.name}'`,
          detail: `Missing ${status.missingEvents.length} events or binary path outdated.`,
          recommendation: 'Run command "Agent Monitor: Configure Workspace Hooks".',
        });
      } else {
        items.push({
          category: 'hooks',
          status: 'ok',
          title: `Hooks Configured in '${folder.name}'`,
          detail: `All ${status.configuredEvents.length} events configured with active binary path.`,
        });
      }
    }
  }

  private httpGet(
    baseUrl: string,
    urlPath: string,
    timeoutMs: number
  ): Promise<{ ok: boolean; status: number; body: string; error?: string }> {
    return new Promise((resolve) => {
      try {
        const u = new URL(urlPath, baseUrl);
        const req = http.get(u.href, { timeout: timeoutMs }, (res) => {
          let body = '';
          res.setEncoding('utf-8');
          res.on('data', (chunk) => (body += chunk));
          res.on('end', () => {
            resolve({
              ok: (res.statusCode || 0) >= 200 && (res.statusCode || 0) < 300,
              status: res.statusCode || 0,
              body,
            });
          });
        });
        req.on('error', (err) => resolve({ ok: false, status: 0, body: '', error: err.message }));
        req.on('timeout', () => {
          req.destroy();
          resolve({ ok: false, status: 0, body: '', error: 'Request timed out' });
        });
      } catch (err: any) {
        resolve({ ok: false, status: 0, body: '', error: err?.message || String(err) });
      }
    });
  }

  private printReportToOutput(report: DiagnosticReport): void {
    this.outputChannel.clear();
    this.outputChannel.show(true);

    const banner = [
      '======================================================================',
      '               AGENT MONITOR SYSTEM DIAGNOSTIC REPORT                 ',
      '======================================================================',
      `Timestamp: ${report.timestamp}`,
      `Platform:  ${report.platform}`,
      `Summary:   ${report.summary.total} checked, ${report.summary.ok} OK, ${report.summary.warning} Warning, ${report.summary.error} Error`,
      '----------------------------------------------------------------------',
    ];

    banner.forEach((line) => this.outputChannel.appendLine(line));

    for (const item of report.items) {
      const badge = item.status === 'ok' ? '✓ [OK]' : item.status === 'warning' ? '⚠ [WARN]' : '✖ [ERROR]';
      this.outputChannel.appendLine(`${badge} [${item.category.toUpperCase()}] ${item.title}`);
      this.outputChannel.appendLine(`      Detail: ${item.detail}`);
      if (item.recommendation) {
        this.outputChannel.appendLine(`      Fix:    ${item.recommendation}`);
      }
      this.outputChannel.appendLine('');
    }

    this.outputChannel.appendLine('======================================================================');
  }

  public dispose(): void {
    this.outputChannel.dispose();
  }
}
