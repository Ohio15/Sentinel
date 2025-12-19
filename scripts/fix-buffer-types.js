const fs = require('fs');

// Fix Buffer type errors in main.ts and server.ts
// The issue is that reassigning installerData causes a type mismatch

// Fix main.ts
let mainTs = fs.readFileSync('src/main/main.ts', 'utf8');

// Find and fix the problematic pattern in main.ts
const mainOldPattern = `let installerData = fs.readFileSync(sourcePath);

      // Embed config for non-MSI installers (PKG/DEB use placeholder replacement)
      // MSI uses command-line properties instead
      if (platform.toLowerCase() !== 'windows') {
        installerData = embedConfigInInstaller(installerData, serverUrl, enrollmentToken);
      }`;

const mainNewPattern = `const rawInstallerData = fs.readFileSync(sourcePath);

      // Embed config for non-MSI installers (PKG/DEB use placeholder replacement)
      // MSI uses command-line properties instead
      const installerData = platform.toLowerCase() !== 'windows'
        ? embedConfigInInstaller(rawInstallerData, serverUrl, enrollmentToken)
        : rawInstallerData;`;

if (mainTs.includes(mainOldPattern)) {
  mainTs = mainTs.replace(mainOldPattern, mainNewPattern);
  fs.writeFileSync('src/main/main.ts', mainTs);
  console.log('Fixed Buffer type issue in main.ts');
} else {
  console.log('Pattern not found in main.ts - may already be fixed or different pattern');
}

// Fix server.ts
let serverTs = fs.readFileSync('src/main/server.ts', 'utf8');

const serverOldPattern = `let installerData = fs.readFileSync(installerPath);
      const localIp = this.getLocalIpAddress();
      const serverUrl = \`http://\${localIp}:\${this.port}\`;

      // Embed config for non-MSI installers (PKG/DEB use placeholder replacement)
      // MSI uses command-line properties instead
      if (platform.toLowerCase() !== 'windows') {
        installerData = embedConfigInInstaller(installerData, serverUrl, this.enrollmentToken);
      }`;

const serverNewPattern = `const rawInstallerData = fs.readFileSync(installerPath);
      const localIp = this.getLocalIpAddress();
      const serverUrl = \`http://\${localIp}:\${this.port}\`;

      // Embed config for non-MSI installers (PKG/DEB use placeholder replacement)
      // MSI uses command-line properties instead
      const installerData = platform.toLowerCase() !== 'windows'
        ? embedConfigInInstaller(rawInstallerData, serverUrl, this.enrollmentToken)
        : rawInstallerData;`;

if (serverTs.includes(serverOldPattern)) {
  serverTs = serverTs.replace(serverOldPattern, serverNewPattern);
  fs.writeFileSync('src/main/server.ts', serverTs);
  console.log('Fixed Buffer type issue in server.ts');
} else {
  console.log('Pattern not found in server.ts - may already be fixed or different pattern');
}

console.log('Done!');
