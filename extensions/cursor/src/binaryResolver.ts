import * as path from 'path';
import * as fs from 'fs';
import * as os from 'os';
import * as crypto from 'crypto';

export const SUPPORTED_PLATFORMS = [
  'darwin-arm64',
  'darwin-x64',
  'linux-x64',
  'linux-arm64',
  'win32-x64',
] as const;

export type SupportedPlatform = (typeof SUPPORTED_PLATFORMS)[number];

export type BinarySource =
  | 'custom'
  | 'embedded-platform'
  | 'embedded-legacy'
  | 'local-build'
  | 'system-path';

export interface ResolvedBinary {
  path: string;
  source: BinarySource;
  platformKey: string;
  checksumVerified?: boolean;
}

export interface ResolveBinaryOptions {
  extensionPath?: string;
  platform?: string;
  arch?: string;
  customPath?: string;
  envPath?: string;
  includeStandardDirs?: boolean;
  workspaceRoots?: string[];
  verifyChecksum?: boolean;
}

export interface ChecksumVerificationResult {
  valid: boolean;
  expected?: string;
  actual?: string;
  error?: string;
}

/**
 * Maps Node process.platform and process.arch to canonical <platform>-<arch> string.
 */
export function getPlatformArchKey(
  platform: string = process.platform,
  arch: string = process.arch
): string {
  const normArch = arch === 'amd64' ? 'x64' : arch === 'aarch64' ? 'arm64' : arch;

  if (platform === 'darwin') {
    if (normArch === 'arm64') {
      return 'darwin-arm64';
    }
    if (normArch === 'x64') {
      return 'darwin-x64';
    }
  } else if (platform === 'linux') {
    if (normArch === 'x64') {
      return 'linux-x64';
    }
    if (normArch === 'arm64') {
      return 'linux-arm64';
    }
  } else if (platform === 'win32') {
    if (normArch === 'x64') {
      return 'win32-x64';
    }
  }

  return `${platform}-${normArch}`;
}

export function isPlatformSupported(platformKey: string): boolean {
  return (SUPPORTED_PLATFORMS as readonly string[]).includes(platformKey);
}

export function getBinaryFileName(name: string, platform: string = process.platform): string {
  const ext = platform === 'win32' ? '.exe' : '';
  return name.endsWith(ext) ? name : `${name}${ext}`;
}

/**
 * Ensures a binary file has execute permissions (0o755) on POSIX platforms.
 */
export function ensureExecutable(filePath: string, platform: string = process.platform): boolean {
  if (platform === 'win32') {
    return true;
  }
  try {
    const stat = fs.statSync(filePath);
    if ((stat.mode & 0o111) === 0) {
      fs.chmodSync(filePath, 0o755);
    }
    return true;
  } catch (_) {
    return false;
  }
}

/**
 * Computes the SHA256 hex digest of a local file.
 */
export function computeFileSha256(filePath: string): string {
  const hash = crypto.createHash('sha256');
  const buffer = fs.readFileSync(filePath);
  hash.update(buffer);
  return hash.digest('hex');
}

/**
 * Verifies the binary file against checksums.json located in the extension's bin/ directory.
 */
export function verifyBinaryChecksum(
  filePath: string,
  platformKey: string,
  fileName: string,
  extensionPath: string
): ChecksumVerificationResult {
  const checksumFilePath = path.join(extensionPath, 'bin', 'checksums.json');
  if (!fs.existsSync(checksumFilePath)) {
    return { valid: true };
  }

  try {
    const content = fs.readFileSync(checksumFilePath, 'utf-8');
    const table = JSON.parse(content);
    const platformTable = table?.[platformKey];
    if (!platformTable || typeof platformTable !== 'object') {
      return { valid: true };
    }

    const baseName = fileName.replace(/\.exe$/i, '');
    const expected = platformTable[fileName] || platformTable[baseName];
    if (!expected || typeof expected !== 'string') {
      return { valid: true };
    }

    const actual = computeFileSha256(filePath);
    if (actual.toLowerCase() === expected.toLowerCase()) {
      return { valid: true, expected, actual };
    }

    return {
      valid: false,
      expected,
      actual,
      error: `Checksum mismatch for ${fileName}: expected ${expected}, got ${actual}`,
    };
  } catch (err: any) {
    return { valid: true, error: `Failed to verify checksum: ${err?.message || err}` };
  }
}

/**
 * Generates an informative installation instruction guide when binary resolution fails.
 */
export function getResolutionFailureMessage(name: string, platformKey: string): string {
  if (!isPlatformSupported(platformKey)) {
    return (
      `Current platform '${platformKey}' is not pre-packaged with embedded binaries. ` +
      `Supported pre-built targets: ${SUPPORTED_PLATFORMS.join(', ')}. ` +
      `Please build '${name}' from source ('go install github.com/Zelayan/agent-monitor@latest') ` +
      `or download a release asset from https://github.com/Zelayan/agent-monitor/releases and ensure it is available on your system PATH.`
    );
  }

  return (
    `Could not locate '${name}' executable in embedded directory (bin/${platformKey}/${name}), ` +
    `local repository build paths, or system PATH. Please verify your extension installation or place '${name}' on system PATH.`
  );
}

/**
 * Resolves the executable path for a given binary name ('agent-monitor' or 'agent-reporter')
 * following a multi-tier fallback hierarchy:
 *  1. customPath (if explicitly provided and exists)
 *  2. Embedded platform-specific directory: <extensionPath>/bin/<platform-arch>/<binName>
 *  3. Embedded flat directory: <extensionPath>/bin/<binName>
 *  4. Local repo/workspace build directories (e.g. <extensionPath>/../../bin/<binName>)
 *  5. System PATH and standard OS binaries directories (/usr/local/bin, /opt/homebrew/bin, etc.)
 */
export function resolveBinary(
  name: string,
  options: ResolveBinaryOptions = {}
): ResolvedBinary | null {
  const platform = options.platform || process.platform;
  const arch = options.arch || process.arch;
  const platformKey = getPlatformArchKey(platform, arch);
  const fileName = getBinaryFileName(name, platform);

  // 1. Explicit custom path
  if (options.customPath && fs.existsSync(options.customPath)) {
    ensureExecutable(options.customPath, platform);
    return {
      path: options.customPath,
      source: 'custom',
      platformKey,
    };
  }

  const extPath = options.extensionPath;

  // 2. Embedded platform-specific directory: <extensionPath>/bin/<platform-arch>/<fileName>
  if (extPath) {
    const platformBinPath = path.join(extPath, 'bin', platformKey, fileName);
    if (fs.existsSync(platformBinPath)) {
      ensureExecutable(platformBinPath, platform);
      let checksumVerified: boolean | undefined;
      if (options.verifyChecksum !== false) {
        const verifyRes = verifyBinaryChecksum(platformBinPath, platformKey, fileName, extPath);
        checksumVerified = verifyRes.valid;
      }
      return {
        path: platformBinPath,
        source: 'embedded-platform',
        platformKey,
        checksumVerified,
      };
    }

    // 3. Embedded flat directory: <extensionPath>/bin/<fileName>
    const legacyBinPath = path.join(extPath, 'bin', fileName);
    if (fs.existsSync(legacyBinPath)) {
      ensureExecutable(legacyBinPath, platform);
      return {
        path: legacyBinPath,
        source: 'embedded-legacy',
        platformKey,
      };
    }

    // 4. Local repository build directories (relative to extensionPath)
    const localRepoBin = path.resolve(extPath, '..', '..', 'bin', fileName);
    if (fs.existsSync(localRepoBin)) {
      ensureExecutable(localRepoBin, platform);
      return {
        path: localRepoBin,
        source: 'local-build',
        platformKey,
      };
    }
  }

  // Check workspace roots if provided
  if (options.workspaceRoots && options.workspaceRoots.length > 0) {
    for (const wsRoot of options.workspaceRoots) {
      const wsBin = path.join(wsRoot, 'bin', fileName);
      if (fs.existsSync(wsBin)) {
        ensureExecutable(wsBin, platform);
        return {
          path: wsBin,
          source: 'local-build',
          platformKey,
        };
      }
    }
  }

  // 5. System PATH and standard OS locations
  const pathEnv = options.envPath !== undefined ? options.envPath : process.env.PATH || '';
  const delimiter = platform === 'win32' ? ';' : ':';
  const envDirs = pathEnv.split(delimiter).filter(Boolean);

  const standardDirs: string[] = [];
  if (options.includeStandardDirs !== false) {
    const home = process.env.HOME || os.homedir();

    if (platform !== 'win32') {
      standardDirs.push(
        '/opt/homebrew/bin',
        '/usr/local/bin',
        '/usr/bin',
        '/bin',
        path.join(home, '.local', 'bin'),
        path.join(home, 'go', 'bin')
      );
    } else {
      const localAppData = process.env.LOCALAPPDATA || '';
      const userProfile = process.env.USERPROFILE || home;
      if (localAppData) {
        standardDirs.push(path.join(localAppData, 'Programs'));
      }
      if (userProfile) {
        standardDirs.push(path.join(userProfile, 'go', 'bin'));
      }
    }
  }

  const searchDirs = Array.from(new Set([...envDirs, ...standardDirs]));

  for (const dir of searchDirs) {
    const candidate = path.join(dir, fileName);
    if (fs.existsSync(candidate)) {
      ensureExecutable(candidate, platform);
      return {
        path: candidate,
        source: 'system-path',
        platformKey,
      };
    }
  }

  return null;
}

export function getPlatformIdentifier(platform?: string, arch?: string): string {
  const key = getPlatformArchKey(platform, arch);
  return isPlatformSupported(key) ? key : 'unknown';
}

export function resolveExtensionBinary(name: string, extensionPath: string): string | null {
  const res = resolveBinary(name, { extensionPath });
  return res ? res.path : null;
}

