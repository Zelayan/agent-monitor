import * as assert from 'assert';
import * as http from 'http';
import * as path from 'path';
import * as fs from 'fs';
import * as os from 'os';
import { test } from 'node:test';
import { DiagnosticsManager, DiagnosticReport } from '../src/diagnosticsManager';
import { DaemonManager } from '../src/daemonManager';
import { HooksManager } from '../src/hooksManager';
import * as vscode from 'vscode';

function makeTempDir(prefix: string): string {
  return fs.mkdtempSync(path.join(os.tmpdir(), `agent-diag-test-${prefix}-`));
}

test('DiagnosticsManager: logs structured messages with timestamp and level', () => {
  const tmpDir = makeTempDir('log');
  try {
    const mockContext: any = { extensionPath: tmpDir, subscriptions: [] };
    const daemonManager = new DaemonManager(mockContext);
    const hooksManager = new HooksManager(daemonManager);
    const diagManager = new DiagnosticsManager(mockContext, daemonManager, hooksManager);

    diagManager.log('INFO', 'Test info message');
    diagManager.log('WARN', 'Test warn message');
    diagManager.log('ERROR', 'Test error message');

    const channel: any = diagManager.getOutputChannel();
    assert.ok(channel.lines.some((l: string) => l.includes('[INFO] Test info message')));
    assert.ok(channel.lines.some((l: string) => l.includes('[WARN] Test warn message')));
    assert.ok(channel.lines.some((l: string) => l.includes('[ERROR] Test error message')));
  } finally {
    fs.rmSync(tmpDir, { recursive: true, force: true });
  }
});

test('DiagnosticsManager: runs full diagnostics and reports binary, daemon, and hook status', async () => {
  const tmpDir = makeTempDir('diag-run');
  const server = http.createServer((req, res) => {
    if (req.url === '/healthz') {
      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end('{"status":"ok"}');
    } else if (req.url === '/readyz') {
      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end('{"ready":true}');
    } else if (req.url === '/metrics') {
      res.writeHead(200, { 'Content-Type': 'text/plain' });
      res.end('agent_monitor_uptime_seconds 42');
    } else if (req.url === '/api/events') {
      res.writeHead(200, {
        'Content-Type': 'text/event-stream',
        'Cache-Control': 'no-cache',
        Connection: 'keep-alive',
      });
      res.write('id: 1\nevent: ping\ndata: {}\n\n');
    } else {
      res.writeHead(404);
      res.end();
    }
  });

  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', () => resolve()));
  const port = (server.address() as any).port;
  const serverUrl = `http://127.0.0.1:${port}`;

  try {
    const extDir = path.join(tmpDir, 'ext');
    fs.mkdirSync(extDir, { recursive: true });

    // Mock workspace folder
    const wsDir = path.join(tmpDir, 'workspace');
    fs.mkdirSync(path.join(wsDir, '.cursor'), { recursive: true });
    fs.writeFileSync(
      path.join(wsDir, '.cursor', 'hooks.json'),
      JSON.stringify({
        version: 1,
        hooks: {
          sessionStart: [{ command: 'agent-reporter' }],
          stop: [{ command: 'agent-reporter' }],
        },
      })
    );

    (vscode.workspace as any).workspaceFolders = [
      {
        uri: { fsPath: wsDir },
        name: 'test-workspace',
        index: 0,
      },
    ];

    const mockContext: any = { extensionPath: extDir, subscriptions: [] };
    const daemonManager = new DaemonManager(mockContext);
    // Override server URL to point to our test server
    (daemonManager as any).serverUrl = serverUrl;
    const hooksManager = new HooksManager(daemonManager);
    const diagManager = new DiagnosticsManager(mockContext, daemonManager, hooksManager);

    const report: DiagnosticReport = await diagManager.runFullDiagnostics();

    assert.ok(report.items.length >= 5, 'Should have evaluated binaries, health endpoints, and hooks');
    assert.strictEqual(report.summary.total, report.items.length);

    // Verify daemon probes succeeded
    const healthItem = report.items.find((i) => i.title.includes('/healthz'));
    assert.ok(healthItem && healthItem.status === 'ok', 'healthz should be ok');

    const readyItem = report.items.find((i) => i.title.includes('/readyz'));
    assert.ok(readyItem && readyItem.status === 'ok', 'readyz should be ok');

    const metricsItem = report.items.find((i) => i.title.includes('/metrics'));
    assert.ok(metricsItem && metricsItem.status === 'ok', 'metrics should be ok');

    const sseItem = report.items.find((i) => i.title.includes('/api/events'));
    assert.ok(sseItem && sseItem.status === 'ok', 'sse stream should be ok');

    // Verify hooks warning (because only 2 events were configured out of 10)
    const hookItem = report.items.find((i) => i.category === 'hooks');
    assert.ok(hookItem, 'Hooks diagnostic item should be present');
    assert.strictEqual(hookItem?.status, 'warning', 'Should flag incomplete hooks');
  } finally {
    server.close();
    fs.rmSync(tmpDir, { recursive: true, force: true });
  }
});
