const esbuild = require('esbuild');
const path = require('path');
const { spawnSync } = require('child_process');
const fs = require('fs');

async function runTests() {
  const extensionDir = path.resolve(__dirname, '..');
  const outfile = path.join(extensionDir, 'dist', 'test-bundle.js');

  const vscodeMockPath = path.join(__dirname, 'vscodeMock.ts');

  // Plugin to redirect 'vscode' imports to our mock
  const vscodeMockPlugin = {
    name: 'vscode-mock',
    setup(build) {
      build.onResolve({ filter: /^vscode$/ }, () => {
        return { path: vscodeMockPath };
      });
    },
  };

  const testFiles = fs
    .readdirSync(__dirname)
    .filter((f) => f.endsWith('.test.ts'))
    .sort()
    .map((f) => path.join(__dirname, f));

  const stdinContents = testFiles
    .map((f) => `require(${JSON.stringify(f)});`)
    .join('\n');

  try {
    await esbuild.build({
      stdin: {
        contents: stdinContents,
        resolveDir: __dirname,
        loader: 'ts',
      },
      bundle: true,
      format: 'cjs',
      platform: 'node',
      target: 'node20',
      outfile,
      plugins: [vscodeMockPlugin],
      sourcemap: 'inline',
    });
  } catch (err) {
    console.error('Failed to bundle tests:', err);
    process.exit(1);
  }

  console.log('Running tests with node:test...');
  const result = spawnSync(process.execPath, ['--test', outfile], {
    stdio: 'inherit',
    cwd: extensionDir,
  });

  // Clean up test bundle
  try {
    if (fs.existsSync(outfile)) {
      fs.unlinkSync(outfile);
    }
  } catch (_) {}

  process.exit(result.status ?? 0);
}

runTests();
