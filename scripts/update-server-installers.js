const fs = require('fs');

// Update server.ts with installer support
let serverTs = fs.readFileSync('src/main/server.ts', 'utf8');

// Add embedConfigInInstaller function after embedConfigInBinary if not already present
if (!serverTs.includes('embedConfigInInstaller')) {
  const embedConfigInInstallerFn = `

// Helper function to embed configuration into installer packages (PKG/DEB)
function embedConfigInInstaller(installerData: Buffer, serverUrl: string, token: string): Buffer {
  let content = installerData.toString('latin1');
  content = content.replace(/__SERVERURL__/g, serverUrl);
  content = content.replace(/__TOKEN__/g, token);
  return Buffer.from(content, 'latin1');
}
`;

  // Insert after embedConfigInBinary function
  serverTs = serverTs.replace(
    /return Buffer\.from\(binaryStr, 'latin1'\);\n\}/,
    `return Buffer.from(binaryStr, 'latin1');\n}${embedConfigInInstallerFn}`
  );
  console.log('Added embedConfigInInstaller function to server.ts');
}

// Replace the agent download endpoint to use installers
const oldEndpoint = `// Agent download - serve actual binaries
    this.app.get('/api/agent/download/:platform', (req: Request, res: Response) => {
      const { platform } = req.params;

      // Determine binary filename based on platform
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
          res.status(400).json({ error: 'Unsupported platform' });
          return;
      }

      // Look for binary in downloads directory (uses resources folder when packaged)
      const downloadsDir = electronApp.isPackaged
        ? path.join(process.resourcesPath, 'downloads')
        : path.join(__dirname, '..', '..', 'downloads');
      const binaryPath = path.join(downloadsDir, filename);

      // Check if binary exists
      if (!fs.existsSync(binaryPath)) {
        res.status(404).json({
          error: 'Agent binary not found',
          message: \`Please build the agent using: cd agent && .\\\\build.ps1 -Platform \${platform}\`,
          expectedPath: binaryPath
        });
        return;
      }

      // Read binary and embed configuration
      const binaryData = fs.readFileSync(binaryPath);
      const localIp = this.getLocalIpAddress();
      const serverUrl = \`http://\${localIp}:\${this.port}\`;

      // Embed server URL and enrollment token into binary
      const modifiedBinary = embedConfigInBinary(binaryData, serverUrl, this.enrollmentToken);

      // Set headers for binary download
      res.setHeader('Content-Type', 'application/octet-stream');
      res.setHeader('Content-Disposition', \`attachment; filename="\${filename}"\`);
      res.setHeader('Content-Length', modifiedBinary.length);

      // Send the modified binary
      res.send(modifiedBinary);
    });`;

const newEndpoint = `// Agent download - serve installer packages
    this.app.get('/api/agent/download/:platform', (req: Request, res: Response) => {
      const { platform } = req.params;

      // Map platform to installer file
      interface InstallerInfo {
        file: string;
        mimeType: string;
      }
      const installerMap: Record<string, InstallerInfo> = {
        windows: { file: 'sentinel-agent.msi', mimeType: 'application/x-msi' },
        macos: { file: 'sentinel-agent.pkg', mimeType: 'application/x-newton-compatible-pkg' },
        linux: { file: 'sentinel-agent.deb', mimeType: 'application/vnd.debian.binary-package' },
      };

      const installer = installerMap[platform.toLowerCase()];
      if (!installer) {
        res.status(400).json({ error: 'Unsupported platform' });
        return;
      }

      // Look for installer in agent directory
      const agentDir = electronApp.isPackaged
        ? path.join(process.resourcesPath, 'agent')
        : path.join(__dirname, '..', '..', 'release', 'agent');
      const installerPath = path.join(agentDir, installer.file);

      // Check if installer exists
      if (!fs.existsSync(installerPath)) {
        res.status(404).json({
          error: 'Agent installer not found',
          message: \`Please build the installer: \${installer.file}\`,
          expectedPath: installerPath
        });
        return;
      }

      // Read installer and embed configuration for PKG/DEB
      let installerData = fs.readFileSync(installerPath);
      const localIp = this.getLocalIpAddress();
      const serverUrl = \`http://\${localIp}:\${this.port}\`;

      // Embed config for non-MSI installers (PKG/DEB use placeholder replacement)
      // MSI uses command-line properties instead
      if (platform.toLowerCase() !== 'windows') {
        installerData = embedConfigInInstaller(installerData, serverUrl, this.enrollmentToken);
      }

      // Set headers for installer download
      res.setHeader('Content-Type', installer.mimeType);
      res.setHeader('Content-Disposition', \`attachment; filename="\${installer.file}"\`);
      res.setHeader('Content-Length', installerData.length);

      // For Windows MSI, also send install command in a custom header
      if (platform.toLowerCase() === 'windows') {
        const installCmd = \`msiexec /i "sentinel-agent.msi" SERVERURL="\${serverUrl}" ENROLLMENTTOKEN="\${this.enrollmentToken}" /qn\`;
        res.setHeader('X-Install-Command', Buffer.from(installCmd).toString('base64'));
      }

      // Send the installer
      res.send(installerData);
    });`;

if (serverTs.includes("filename = 'sentinel-agent.exe'")) {
  serverTs = serverTs.replace(oldEndpoint, newEndpoint);
  console.log('Updated /api/agent/download/:platform endpoint to use installers');
} else {
  console.log('Agent download endpoint already updated or pattern not found');
}

// Update the /api/agent/downloads endpoint to list installers instead of binaries
const oldListEndpoint = `// List available agent downloads (requires authentication)
    this.app.get('/api/agent/downloads', this.requireAuth.bind(this), (req: Request, res: Response) => {
      const downloadsDir = electronApp.isPackaged
        ? path.join(process.resourcesPath, 'downloads')
        : path.join(__dirname, '..', '..', 'downloads');

      if (!fs.existsSync(downloadsDir)) {
        res.json({ agents: [], message: 'No agents built yet. Run: cd agent && .\\\\build.ps1 -Platform all' });
        return;
      }

      const files = fs.readdirSync(downloadsDir)
        .filter(f => f.startsWith('sentinel-agent'))
        .map(filename => {
          const filePath = path.join(downloadsDir, filename);
          const stats = fs.statSync(filePath);
          return {
            filename,
            size: stats.size,
            modified: stats.mtime,
            platform: filename.includes('macos') ? 'macos' :
                     filename.includes('linux') ? 'linux' : 'windows'
          };`;

const newListEndpoint = `// List available agent installers (requires authentication)
    this.app.get('/api/agent/downloads', this.requireAuth.bind(this), (req: Request, res: Response) => {
      const agentDir = electronApp.isPackaged
        ? path.join(process.resourcesPath, 'agent')
        : path.join(__dirname, '..', '..', 'release', 'agent');

      if (!fs.existsSync(agentDir)) {
        res.json({ agents: [], message: 'No agent installers built yet.' });
        return;
      }

      // Look for installer files
      const installerFiles = ['sentinel-agent.msi', 'sentinel-agent.pkg', 'sentinel-agent.deb'];
      const files = installerFiles
        .filter(f => fs.existsSync(path.join(agentDir, f)))
        .map(filename => {
          const filePath = path.join(agentDir, filename);
          const stats = fs.statSync(filePath);
          return {
            filename,
            size: stats.size,
            modified: stats.mtime,
            platform: filename.includes('.pkg') ? 'macos' :
                     filename.includes('.deb') ? 'linux' : 'windows'
          };`;

if (serverTs.includes("path.join(process.resourcesPath, 'downloads')") && serverTs.includes("f.startsWith('sentinel-agent')")) {
  serverTs = serverTs.replace(oldListEndpoint, newListEndpoint);
  console.log('Updated /api/agent/downloads endpoint to list installers');
} else {
  console.log('Downloads list endpoint already updated or pattern not found');
}

fs.writeFileSync('src/main/server.ts', serverTs);
console.log('Updated src/main/server.ts');
