const fs = require('fs');

// Update main.ts with installer support
let mainTs = fs.readFileSync('src/main/main.ts', 'utf8');

// Add embedConfigInInstaller function after embedConfigInBinary
const embedConfigInInstallerFn = `
// Helper function to embed configuration into installer packages (PKG/DEB)
// Uses placeholder replacement in the config.json embedded within the package
function embedConfigInInstaller(installerData: Buffer, serverUrl: string, token: string): Buffer {
  let content = installerData.toString('latin1');
  // Replace placeholder strings in the config file within the package
  content = content.replace(/__SERVERURL__/g, serverUrl);
  content = content.replace(/__TOKEN__/g, token);
  return Buffer.from(content, 'latin1');
}
`;

if (!mainTs.includes('embedConfigInInstaller')) {
  mainTs = mainTs.replace(
    /return Buffer\.from\(binaryStr, 'latin1'\);\n\}\n\nfunction getLocalIpAddress/,
    `return Buffer.from(binaryStr, 'latin1');\n}${embedConfigInInstallerFn}\nfunction getLocalIpAddress`
  );
  console.log('Added embedConfigInInstaller function');
}

// Update the agent:download handler to serve installers
const oldHandler = `ipcMain.handle('agent:download', async (_, platform: string) => {
    // Get the downloads directory - uses resources folder when packaged
    console.log('Agent download - isPackaged:', app.isPackaged);
    console.log('Agent download - resourcesPath:', process.resourcesPath);
    const downloadsDir = app.isPackaged
      ? path.join(process.resourcesPath, 'downloads')
      : path.join(__dirname, '..', '..', 'downloads');

    // Determine filename based on platform
    let filename: string;
    switch (platform.toLowerCase()) {
      case 'windows':
        filename = 'sentinel-agent.exe';
        break;
      case 'macos':
        filename = 'sentinel-agent-macos';
        break;
      case 'linux':
        filename = 'sentinel-agent-linux';
        break;
      default:
        return { success: false, error: 'Unsupported platform' };
    }

    const sourcePath = path.join(downloadsDir, filename);
    console.log('Agent download - sourcePath:', sourcePath);
    console.log('Agent download - exists:', fs.existsSync(sourcePath));

    // Check if source file exists
    if (!fs.existsSync(sourcePath)) {
      return {
        success: false,
        error: \`Agent binary not found at: \${sourcePath}\`,
      };
    }

    // Show save dialog
    const result = await dialog.showSaveDialog(mainWindow!, {
      title: 'Save Agent Executable',
      defaultPath: filename,
      filters: platform === 'windows'
        ? [{ name: 'Executable', extensions: ['exe'] }]
        : [{ name: 'All Files', extensions: ['*'] }],
    });

    if (result.canceled || !result.filePath) {
      return { success: false, canceled: true };
    }

    // Read, embed config, and save file
    try {
      const binaryData = fs.readFileSync(sourcePath);

      // Get server info for embedding
      const localIp = getLocalIpAddress();
      const serverPort = server.getPort();
      const serverUrl = \`http://\${localIp}:\${serverPort}\`;
      const enrollmentToken = server.getEnrollmentToken();

      // Embed server URL and enrollment token into binary
      const modifiedBinary = embedConfigInBinary(binaryData, serverUrl, enrollmentToken);

      // Write the modified binary
      await fs.promises.writeFile(result.filePath, modifiedBinary);

      return {
        success: true,
        filePath: result.filePath,
        size: modifiedBinary.length,
      };
    } catch (error: any) {
      return {
        success: false,
        error: \`Failed to save file: \${error.message}\`,
      };
    }
  });`;

const newHandler = `ipcMain.handle('agent:download', async (_, platform: string) => {
    // Use installer packages instead of raw binaries
    const agentDir = app.isPackaged
      ? path.join(process.resourcesPath, 'agent')
      : path.join(__dirname, '..', '..', 'release', 'agent');

    console.log('Agent installer download - platform:', platform);
    console.log('Agent installer download - agentDir:', agentDir);

    // Map platform to installer file
    interface InstallerInfo {
      file: string;
      filter: { name: string; extensions: string[] };
    }
    const installerMap: Record<string, InstallerInfo> = {
      windows: {
        file: 'sentinel-agent.msi',
        filter: { name: 'Windows Installer', extensions: ['msi'] }
      },
      macos: {
        file: 'sentinel-agent.pkg',
        filter: { name: 'macOS Installer', extensions: ['pkg'] }
      },
      linux: {
        file: 'sentinel-agent.deb',
        filter: { name: 'Debian Package', extensions: ['deb'] }
      },
    };

    const installer = installerMap[platform.toLowerCase()];
    if (!installer) {
      return { success: false, error: 'Unsupported platform' };
    }

    const sourcePath = path.join(agentDir, installer.file);
    console.log('Agent installer download - sourcePath:', sourcePath);
    console.log('Agent installer download - exists:', fs.existsSync(sourcePath));

    // Check if installer exists
    if (!fs.existsSync(sourcePath)) {
      return {
        success: false,
        error: \`Installer not found: \${installer.file}. Build installers first.\`,
      };
    }

    // Show save dialog
    const result = await dialog.showSaveDialog(mainWindow!, {
      title: 'Save Agent Installer',
      defaultPath: installer.file,
      filters: [installer.filter],
    });

    if (result.canceled || !result.filePath) {
      return { success: false, canceled: true };
    }

    // Get server info
    const localIp = getLocalIpAddress();
    const serverPort = server.getPort();
    const serverUrl = \`http://\${localIp}:\${serverPort}\`;
    const enrollmentToken = server.getEnrollmentToken();

    try {
      let installerData = fs.readFileSync(sourcePath);

      // Embed config for non-MSI installers (PKG/DEB use placeholder replacement)
      // MSI uses command-line properties instead
      if (platform.toLowerCase() !== 'windows') {
        installerData = embedConfigInInstaller(installerData, serverUrl, enrollmentToken);
      }

      await fs.promises.writeFile(result.filePath, installerData);

      // Return install command based on platform
      const installCommands: Record<string, string> = {
        windows: \`msiexec /i "\${result.filePath}" SERVERURL="\${serverUrl}" ENROLLMENTTOKEN="\${enrollmentToken}" /qn\`,
        macos: \`sudo installer -pkg "\${result.filePath}" -target /\`,
        linux: \`sudo dpkg -i "\${result.filePath}"\`,
      };

      return {
        success: true,
        filePath: result.filePath,
        size: installerData.length,
        installCommand: installCommands[platform.toLowerCase()],
      };
    } catch (error: any) {
      return {
        success: false,
        error: \`Failed to save installer: \${error.message}\`,
      };
    }
  });`;

if (mainTs.includes("filename = 'sentinel-agent.exe'")) {
  mainTs = mainTs.replace(oldHandler, newHandler);
  console.log('Updated agent:download handler to use installers');
} else {
  console.log('agent:download handler already updated or pattern not found');
}

fs.writeFileSync('src/main/main.ts', mainTs);
console.log('Updated src/main/main.ts');
