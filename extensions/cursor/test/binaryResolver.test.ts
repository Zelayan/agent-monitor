import test from 'node:test';
import assert from 'node:assert';
import * as fs from 'fs';
import * as path from 'path';
import * as os from 'os';
import * as crypto from 'crypto';
import {
  getPlatformArchKey,
  isPlatformSupported,
  getPlatformIdentifier,
  getBinaryFileName,
  ensureExecutable,
  computeFileSha256,
  verifyBinaryChecksum,
  getResolutionFailureMessage,
  resolveBinary,
  resolveExtensionBinary,
  SUPPORTED_PLATFORMS,
} from '../src/binaryResolver';
import { DaemonManager } from '../src/daemonManager';
import { HooksManager } from '../src/hooksManager';
import * as mockVscode from './vscodeMock';

function makeTempDir(prefix: string): string {
  return fs.mkdtempSync(path.join(os.tmpdir(), `wp22-test-${prefix}-`));
}

test('getPlatformArchKey maps OS and architecture correctly', () => {
  assert.strictEqual(getPlatformArchKey('darwin', 'arm64'), 'darwin-arm64');
  assert.strictEqual(getPlatformArchKey('darwin', 'x64'), 'darwin-x64');
  assert.strictEqual(getPlatformArchKey('darwin', 'amd64'), 'darwin-x64');

  assert.strictEqual(getPlatformArchKey('linux', 'x64'), 'linux-x64');
  assert.strictEqual(getPlatformArchKey('linux', 'amd64'), 'linux-x64');
  assert.strictEqual(getPlatformArchKey('linux', 'arm64'), 'linux-arm64');
  assert.strictEqual(getPlatformArchKey('linux', 'aarch64'), 'linux-arm64');

  assert.strictEqual(getPlatformArchKey('win32', 'x64'), 'win32-x64');
  assert.strictEqual(getPlatformArchKey('win32', 'amd64'), 'win32-x64');

  // Unsupported or exotic platforms
  assert.strictEqual(getPlatformArchKey('freebsd', 'x64'), 'freebsd-x64');
  assert.strictEqual(getPlatformArchKey('sunos', 'x64'), 'sunos-x64');
});

test('isPlatformSupported and getPlatformIdentifier recognize supported matrix', () => {
  for (const p of SUPPORTED_PLATFORMS) {
    assert.strictEqual(isPlatformSupported(p), true, `Platform ${p} should be supported`);
  }
  assert.strictEqual(isPlatformSupported('freebsd-x64'), false);
  assert.strictEqual(isPlatformSupported('darwin-ia32'), false);
  assert.strictEqual(isPlatformSupported('win32-arm64'), false);

  assert.strictEqual(getPlatformIdentifier('darwin', 'arm64'), 'darwin-arm64');
  assert.strictEqual(getPlatformIdentifier('darwin', 'x64'), 'darwin-x64');
  assert.strictEqual(getPlatformIdentifier('linux', 'x64'), 'linux-x64');
  assert.strictEqual(getPlatformIdentifier('linux', 'arm64'), 'linux-arm64');
  assert.strictEqual(getPlatformIdentifier('win32', 'x64'), 'win32-x64');
  assert.strictEqual(getPlatformIdentifier('freebsd', 'x64'), 'unknown');
});

test('getBinaryFileName handles Windows vs POSIX extensions', () => {
  assert.strictEqual(getBinaryFileName('agent-monitor', 'darwin'), 'agent-monitor');
  assert.strictEqual(getBinaryFileName('agent-monitor', 'linux'), 'agent-monitor');
  assert.strictEqual(getBinaryFileName('agent-monitor', 'win32'), 'agent-monitor.exe');
  assert.strictEqual(getBinaryFileName('agent-monitor.exe', 'win32'), 'agent-monitor.exe');
});

test('ensureExecutable marks file as executable on POSIX', () => {
  const tmpDir = makeTempDir('chmod-test');
  try {
    const filePath = path.join(tmpDir, 'test-bin');
    fs.writeFileSync(filePath, '#!/bin/sh\necho ok\n', { mode: 0o644 });

    if (process.platform !== 'win32') {
      const initialStat = fs.statSync(filePath);
      assert.strictEqual((initialStat.mode & 0o111) === 0, true);

      assert.strictEqual(ensureExecutable(filePath, 'darwin'), true);

      const updatedStat = fs.statSync(filePath);
      assert.notStrictEqual(updatedStat.mode & 0o111, 0, 'Executable bits should be set');
    } else {
      assert.strictEqual(ensureExecutable(filePath, 'win32'), true);
    }
  } finally {
    fs.rmSync(tmpDir, { recursive: true, force: true });
  }
});

test('checksum computation and verification', () => {
  const tmpDir = makeTempDir('checksum-test');
  try {
    const extDir = path.join(tmpDir, 'ext');
    const binDir = path.join(extDir, 'bin');
    const platformDir = path.join(binDir, 'darwin-arm64');
    fs.mkdirSync(platformDir, { recursive: true });

    const binaryFile = path.join(platformDir, 'agent-monitor');
    const content = 'mock-binary-payload-for-sha256';
    fs.writeFileSync(binaryFile, content, 'utf-8');

    const expectedSha = crypto.createHash('sha256').update(content).digest('hex');
    assert.strictEqual(computeFileSha256(binaryFile), expectedSha);

    // 1. Missing checksums.json -> valid: true (graceful fallback)
    const resNoTable = verifyBinaryChecksum(binaryFile, 'darwin-arm64', 'agent-monitor', extDir);
    assert.strictEqual(resNoTable.valid, true);

    // 2. Matching checksums.json -> valid: true
    const checksums = {
      'darwin-arm64': {
        'agent-monitor': expectedSha,
      },
    };
    fs.writeFileSync(path.join(binDir, 'checksums.json'), JSON.stringify(checksums), 'utf-8');

    const resMatch = verifyBinaryChecksum(binaryFile, 'darwin-arm64', 'agent-monitor', extDir);
    assert.strictEqual(resMatch.valid, true);
    assert.strictEqual(resMatch.expected, expectedSha);

    // 3. Corrupted or mismatched binary -> valid: false
    fs.writeFileSync(binaryFile, 'corrupted-content', 'utf-8');
    const resMismatch = verifyBinaryChecksum(binaryFile, 'darwin-arm64', 'agent-monitor', extDir);
    assert.strictEqual(resMismatch.valid, false);
    assert.ok(resMismatch.error?.includes('Checksum mismatch'));
  } finally {
    fs.rmSync(tmpDir, { recursive: true, force: true });
  }
});

test('getResolutionFailureMessage gives clear guidance', () => {
  const unsupportedMsg = getResolutionFailureMessage('agent-monitor', 'freebsd-x64');
  assert.ok(unsupportedMsg.includes('freebsd-x64'));
  assert.ok(unsupportedMsg.includes('Supported pre-built targets'));
  assert.ok(unsupportedMsg.includes('go install github.com/Zelayan/agent-monitor@latest'));

  const supportedMsg = getResolutionFailureMessage('agent-monitor', 'darwin-arm64');
  assert.ok(supportedMsg.includes('darwin-arm64'));
  assert.ok(supportedMsg.includes('local repository build paths'));
});

test('resolveBinary priority hierarchy: embedded platform -> legacy -> local build -> system PATH', () => {
  const tmpDir = makeTempDir('resolve-priority');
  try {
    const extDir = path.join(tmpDir, 'extension');
    const platformBinDir = path.join(extDir, 'bin', 'darwin-arm64');
    const flatBinDir = path.join(extDir, 'bin');
    const localBuildDir = path.join(tmpDir, 'bin');
    const systemPathDir = path.join(tmpDir, 'system-bin');

    fs.mkdirSync(platformBinDir, { recursive: true });
    fs.mkdirSync(localBuildDir, { recursive: true });
    fs.mkdirSync(systemPathDir, { recursive: true });

    const platformBin = path.join(platformBinDir, 'agent-monitor');
    const flatBin = path.join(flatBinDir, 'agent-monitor');
    const localBin = path.join(localBuildDir, 'agent-monitor');
    const sysBin = path.join(systemPathDir, 'agent-monitor');

    // Case 1: Platform-specific binary takes highest precedence
    fs.writeFileSync(platformBin, 'platform-bin', { mode: 0o755 });
    fs.writeFileSync(flatBin, 'flat-bin', { mode: 0o755 });
    fs.writeFileSync(localBin, 'local-bin', { mode: 0o755 });
    fs.writeFileSync(sysBin, 'sys-bin', { mode: 0o755 });

    let resolved = resolveBinary('agent-monitor', {
      extensionPath: extDir,
      platform: 'darwin',
      arch: 'arm64',
      envPath: systemPathDir,
      includeStandardDirs: false,
    });
    assert.strictEqual(resolved?.source, 'embedded-platform');
    assert.strictEqual(resolved?.path, platformBin);
    assert.strictEqual(resolveExtensionBinary('agent-monitor', extDir), platformBin);

    // Case 2: Platform binary removed -> falls back to flat legacy bin
    fs.unlinkSync(platformBin);
    resolved = resolveBinary('agent-monitor', {
      extensionPath: extDir,
      platform: 'darwin',
      arch: 'arm64',
      envPath: systemPathDir,
      includeStandardDirs: false,
    });
    assert.strictEqual(resolved?.source, 'embedded-legacy');
    assert.strictEqual(resolved?.path, flatBin);

    // Case 3: Flat bin removed -> falls back to local repo build
    fs.unlinkSync(flatBin);
    resolved = resolveBinary('agent-monitor', {
      extensionPath: extDir,
      platform: 'darwin',
      arch: 'arm64',
      workspaceRoots: [tmpDir],
      envPath: systemPathDir,
      includeStandardDirs: false,
    });
    assert.strictEqual(resolved?.source, 'local-build');
    assert.strictEqual(resolved?.path, localBin);

    // Case 4: Local bin removed -> falls back to system PATH
    fs.unlinkSync(localBin);
    resolved = resolveBinary('agent-monitor', {
      extensionPath: extDir,
      platform: 'darwin',
      arch: 'arm64',
      envPath: systemPathDir,
      includeStandardDirs: false,
    });
    assert.strictEqual(resolved?.source, 'system-path');
    assert.strictEqual(resolved?.path, sysBin);

    // Case 5: System bin removed -> returns null
    fs.unlinkSync(sysBin);
    resolved = resolveBinary('agent-monitor', {
      extensionPath: extDir,
      platform: 'darwin',
      arch: 'arm64',
      envPath: systemPathDir,
      includeStandardDirs: false,
    });
    assert.strictEqual(resolved, null);

    // Case 6: Explicit customPath overrides all
    const customBin = path.join(tmpDir, 'custom-bin');
    fs.writeFileSync(customBin, 'custom', { mode: 0o755 });
    resolved = resolveBinary('agent-monitor', {
      extensionPath: extDir,
      customPath: customBin,
    });
    assert.strictEqual(resolved?.source, 'custom');
    assert.strictEqual(resolved?.path, customBin);
  } finally {
    fs.rmSync(tmpDir, { recursive: true, force: true });
  }
});

test('DaemonManager and HooksManager integration with cross-platform binary resolution', () => {
  const tmpDir = makeTempDir('manager-integration');
  try {
    const extDir = path.join(tmpDir, 'ext');
    const platformKey = getPlatformArchKey();
    const binDir = path.join(extDir, 'bin', platformKey);
    fs.mkdirSync(binDir, { recursive: true });

    const reporterFileName = getBinaryFileName('agent-reporter');
    const embeddedReporter = path.join(binDir, reporterFileName);
    fs.writeFileSync(embeddedReporter, 'mock-reporter', { mode: 0o755 });

    mockVscode.resetMock();
    const mockContext: any = {
      extensionPath: extDir,
    };

    const daemonManager = new DaemonManager(mockContext);
    const resolvedReporter = daemonManager.resolveBinaryPath('agent-reporter');
    assert.strictEqual(resolvedReporter, embeddedReporter);

    const hooksManager = new HooksManager(daemonManager);
    assert.strictEqual(hooksManager.getReporterCommand(), embeddedReporter);
  } finally {
    fs.rmSync(tmpDir, { recursive: true, force: true });
  }
});
