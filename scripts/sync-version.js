// Propagates the root package.json version.
//
//   npm run sync-version            -> server/internal/version/VERSION (embedded
//                                      in the backend binary, reported on /health)
//   node scripts/sync-version.js --agent
//                                   -> also agent/version.json (used by
//                                      `npm run release:agent`)
//
// agent/version.json tracks the last SIGNED agent release, not every server
// release, so it only moves when an agent release is being cut.
const fs = require('fs');
const path = require('path');

const root = path.join(__dirname, '..');
const pkg = JSON.parse(fs.readFileSync(path.join(root, 'package.json'), 'utf-8'));

if (!/^[0-9]+\.[0-9]+\.[0-9]+$/.test(pkg.version)) {
  console.error(`package.json version ${JSON.stringify(pkg.version)} is not X.Y.Z`);
  process.exit(1);
}

const serverVersionPath = path.join(root, 'server', 'internal', 'version', 'VERSION');
fs.writeFileSync(serverVersionPath, pkg.version + '\n');
console.log('Synced server version to', pkg.version);

if (process.argv.includes('--agent')) {
  const versionPath = path.join(root, 'agent', 'version.json');
  const vj = JSON.parse(fs.readFileSync(versionPath, 'utf-8'));
  vj.version = pkg.version;
  vj.releaseDate = new Date().toISOString().split('T')[0];
  fs.writeFileSync(versionPath, JSON.stringify(vj, null, 2) + '\n');
  console.log('Synced agent version to', pkg.version);
}
