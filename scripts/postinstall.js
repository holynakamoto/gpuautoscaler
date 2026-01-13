#!/usr/bin/env node
const https = require('https');
const fs = require('fs');
const path = require('path');
const os = require('os');
const { spawnSync } = require('child_process');

function platformArch() {
  const p = os.platform();
  const a = os.arch();
  // normalize names used by goreleaser archives
  const arch = a === 'x64' ? 'amd64' : a;
  return { platform: p, arch };
}

function assetName(version, platform, arch) {
  const ext = platform === 'win32' ? 'zip' : 'tar.gz';
  return `gpuautoscaler_${version}_${platform}_${arch}.${ext}`;
}

function download(url, dest) {
  return new Promise((resolve, reject) => {
    const file = fs.createWriteStream(dest);
    https.get(url, (res) => {
      if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
        return download(res.headers.location, dest).then(resolve).catch(reject);
      }
      if (res.statusCode !== 200) return reject(new Error('Download failed: ' + res.statusCode));
      res.pipe(file);
      file.on('finish', () => file.close(resolve));
    }).on('error', (err) => reject(err));
  });
}

function extractArchive(archivePath, destDir) {
  if (archivePath.endsWith('.zip')) {
    // unzip
    spawnSync('unzip', ['-o', archivePath, '-d', destDir], { stdio: 'inherit' });
  } else {
    spawnSync('tar', ['-xzf', archivePath, '-C', destDir], { stdio: 'inherit' });
  }
}

async function main() {
  try {
    const pkg = JSON.parse(fs.readFileSync(path.join(__dirname, '..', 'package.json')));
    const version = pkg.version;
    const { platform, arch } = platformArch();
    const name = assetName(version, platform, arch);
    const owner = 'holynakamoto';
    const repo = 'gpuautoscaler';
    const url = `https://github.com/${owner}/${repo}/releases/download/v${version}/${name}`;

    const tmp = path.join(os.tmpdir(), name);
    console.log('Downloading binary:', url);
    await download(url, tmp);

    const destDir = path.join(__dirname, '..', 'bin', 'native', `${platform}-${arch}`);
    fs.mkdirSync(destDir, { recursive: true });
    extractArchive(tmp, destDir);

    // Set executable permissions for linux/darwin
    const binName = platform === 'win32' ? 'gpu-autoscaler.exe' : 'gpu-autoscaler';
    const candidate = path.join(destDir, binName);
    if (fs.existsSync(candidate)) {
      try { fs.chmodSync(candidate, 0o755); } catch (e) {}
    }

    // cleanup
    try { fs.unlinkSync(tmp); } catch (e) {}
    console.log('Binary installed to', destDir);
  } catch (err) {
    // Do not fail install entirely; just warn
    console.warn('gpuautoscaler postinstall: could not download native binary:', err.message);
  }
}

main();
