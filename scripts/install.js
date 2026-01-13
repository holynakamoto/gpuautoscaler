#!/usr/bin/env node
const fs = require('fs');
const path = require('path');
const os = require('os');
const axios = require('axios');
const tar = require('tar');
const { spawnSync } = require('child_process');

const owner = 'holynakamoto';
const repo = 'gpuautoscaler';
const version = require('../package.json').version;

function platformName() {
  const p = process.platform === 'win32' ? 'windows' : process.platform;
  const a = process.arch === 'x64' ? 'amd64' : process.arch;
  return { p, a };
}

async function downloadAsset(url, dest) {
  const writer = fs.createWriteStream(dest);
  const res = await axios({ url, method: 'GET', responseType: 'stream' });
  return new Promise((resolve, reject) => {
    res.data.pipe(writer);
    writer.on('finish', resolve);
    writer.on('error', reject);
  });
}

async function install() {
  try {
    const { p, a } = platformName();
    const ext = p === 'windows' ? 'zip' : 'tar.gz';
    const assetName = `${repo}_${version}_${p}_${a}.${ext}`;
    const url = `https://github.com/${owner}/${repo}/releases/download/v${version}/${assetName}`;

    console.log(`Downloading ${assetName} from ${url}...`);
    const tmp = path.join(os.tmpdir(), assetName);
    await downloadAsset(url, tmp);

    const binDir = path.join(__dirname, '..', 'bin', 'native', `${p}-${a}`);
    fs.mkdirSync(binDir, { recursive: true });

    if (ext === 'tar.gz') {
      await tar.x({ file: tmp, C: binDir });
    } else {
      // write zip and try to unzip using system unzip
      const unzip = spawnSync('unzip', ['-o', tmp, '-d', binDir], { stdio: 'inherit' });
      if (unzip.status !== 0) {
        console.warn('unzip not available or failed; binary may be stored as a zip file at', tmp);
      }
    }

    // set executable bit for non-windows
    if (p !== 'windows') {
      const candidate = path.join(binDir, 'gpu-autoscaler');
      if (fs.existsSync(candidate)) {
        try { fs.chmodSync(candidate, 0o755); } catch (e) { /* ignore */ }
      }
    }

    try { fs.unlinkSync(tmp); } catch (e) { /* ignore */ }
    console.log('Installed binary to', binDir);
  } catch (err) {
    console.warn('gpuautoscaler: failed to download native binary:', err.message);
  }
}

install();
