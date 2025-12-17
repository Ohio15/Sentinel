const fs = require('fs');
const path = require('path');

const pkgPath = path.join(__dirname, '..', 'package.json');
const versionPath = path.join(__dirname, '..', 'agent', 'version.json');

const pkg = JSON.parse(fs.readFileSync(pkgPath, 'utf-8'));
const vj = JSON.parse(fs.readFileSync(versionPath, 'utf-8'));

vj.version = pkg.version;
vj.releaseDate = new Date().toISOString().split('T')[0];

fs.writeFileSync(versionPath, JSON.stringify(vj, null, 2) + '\n');
console.log('Synced agent version to', pkg.version);
