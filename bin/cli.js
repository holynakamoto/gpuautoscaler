#!/usr/bin/env node
const { spawn } = require('child_process');
const path = require('path');
const fs = require('fs');

function exitWithHelp() {
  console.error('gpuautoscaler: native binary not found for your platform.');
  console.error('This package provides a small shim to invoke a native binary if present.');
  console.error('Options:');
  console.error('  - Install the native binary and ensure it is available as gpu-autoscaler in your PATH');
  console.error('  - Build the binary from source (see the repo README) and place it in this package under bin/native/<platform>-<arch>/gpu-autoscaler');
  console.error('  - Use the Helm chart in the charts directory for deployment');
  process.exit(1);
}

const platform = process.platform; // darwin, linux, win32
const arch = process.arch; // x64, arm64, etc.
const binName = platform === 'win32' ? 'gpu-autoscaler.exe' : 'gpu-autoscaler';
const bundledPath = path.join(__dirname, 'native', `${platform}-${arch}`, binName);

if (fs.existsSync(bundledPath)) {
  const child = spawn(bundledPath, process.argv.slice(2), { stdio: 'inherit' });
  child.on('exit', function(code) { process.exit(code); });
  child.on('error', function() { exitWithHelp(); });
} else {
  // Try to fallback to system PATH binary name
  const which = process.platform === 'win32' ? 'where' : 'which';
  const check = spawn(which, [binName], { stdio: 'pipe' });
  let out = '';
  check.stdout.on('data', d => out += d.toString());
  check.on('close', code => {
    if (out.trim()) {
      const systemBin = out.split(/\r?\n/)[0].trim();
      const fallback = spawn(systemBin, process.argv.slice(2), { stdio: 'inherit' });
      fallback.on('exit', c => process.exit(c));
      fallback.on('error', () => exitWithHelp());
    } else {
      exitWithHelp();
    }
  });
}
