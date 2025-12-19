const fs = require('fs');

// Update Devices.tsx to reflect installer-based downloads
let devicesTsx = fs.readFileSync('src/renderer/pages/Devices.tsx', 'utf8');

// Update button labels to show installer file types
devicesTsx = devicesTsx.replace(
  "{downloadingPlatform === 'windows' ? 'Saving...' : 'sentinel-agent.exe'}",
  "{downloadingPlatform === 'windows' ? 'Saving...' : 'sentinel-agent.msi'}"
);
console.log('Updated Windows button label to show .msi');

devicesTsx = devicesTsx.replace(
  "{downloadingPlatform === 'macos' ? 'Saving...' : 'sentinel-agent'}",
  "{downloadingPlatform === 'macos' ? 'Saving...' : 'sentinel-agent.pkg'}"
);
console.log('Updated macOS button label to show .pkg');

devicesTsx = devicesTsx.replace(
  "{downloadingPlatform === 'linux' ? 'Saving...' : 'sentinel-agent'}",
  "{downloadingPlatform === 'linux' ? 'Saving...' : 'sentinel-agent.deb'}"
);
console.log('Updated Linux button label to show .deb');

// Update the section header from "Download Agent" to "Download Agent Installer"
devicesTsx = devicesTsx.replace(
  '<h2 className="text-lg font-semibold text-text-primary mb-4">Download Agent</h2>',
  '<h2 className="text-lg font-semibold text-text-primary mb-4">Download Agent Installer</h2>'
);
console.log('Updated section header');

// Update the description text
devicesTsx = devicesTsx.replace(
  'Download the agent executable for your platform, then run the install command below.',
  'Download the platform-specific installer. Installation is automatic - just run the downloaded file.'
);
console.log('Updated description text');

// Update the download result message to include install command
// The result now includes installCommand from the IPC handler
devicesTsx = devicesTsx.replace(
  `setDownloadResult({
          type: 'success',
          message: \`Agent saved (\${sizeMB} MB). Run as Admin with: --install --server=<URL> --token=<TOKEN>\`
        });`,
  `setDownloadResult({
          type: 'success',
          message: result.installCommand
            ? \`Installer saved (\${sizeMB} MB). Install command: \${result.installCommand}\`
            : \`Installer saved (\${sizeMB} MB). Double-click to install.\`
        });`
);
console.log('Updated success message to show install command');

// Remove the Enterprise Deployment MSI section since MSI is now the default Windows option
// Find and remove the entire MSI section
const msiSectionStart = devicesTsx.indexOf('{/* Enterprise Deployment - MSI */}');
const msiSectionEnd = devicesTsx.indexOf('{/* Quick Install - PowerShell */}');

if (msiSectionStart !== -1 && msiSectionEnd !== -1) {
  const before = devicesTsx.substring(0, msiSectionStart);
  const after = devicesTsx.substring(msiSectionEnd);
  devicesTsx = before + '\n          ' + after;
  console.log('Removed duplicate MSI section');
}

// Also remove msiDownloading and msiResult state since they're no longer needed
// Keep the state declarations but they'll become unused - TypeScript will catch that

fs.writeFileSync('src/renderer/pages/Devices.tsx', devicesTsx);
console.log('Updated src/renderer/pages/Devices.tsx');
