#!/usr/bin/env python3
"""Update main.ts to generate EXE installers instead of PS1 scripts."""

import re

# Read the file
with open('D:/Projects/Sentinel/src/main/main.ts', 'r', encoding='utf-8') as f:
    content = f.read()

# Find the generateWindowsInstallerScript function and add the EXE generator before it
exe_generator_function = '''
// Generate a Windows EXE installer with embedded configuration
async function generateWindowsInstallerExe(serverUrl: string, token: string): Promise<Buffer | null> {
  try {
    // Path to the installer template
    const templatePath = app.isPackaged
      ? path.join(process.resourcesPath, 'installers', 'sentinel-installer-template.exe')
      : path.join(__dirname, '..', '..', 'installers', 'sentinel-installer-template.exe');

    console.log('[Installer] Template path:', templatePath);

    if (!fs.existsSync(templatePath)) {
      console.error('[Installer] Template not found at:', templatePath);
      return null;
    }

    // Read the template binary
    const templateBuffer = await fs.promises.readFile(templatePath);
    console.log('[Installer] Template size:', templateBuffer.length, 'bytes');

    // The placeholders in the Go binary (must match exactly what's in the Go code)
    const serverPlaceholder = 'SENTINEL_CONFIG_SERVER:http://_______________________________________________:END';
    const tokenPlaceholder = 'SENTINEL_CONFIG_TOKEN:__________________________________________________________:END';

    // Pad values to match placeholder length (keeping the prefix and :END suffix)
    const serverPrefix = 'SENTINEL_CONFIG_SERVER:';
    const tokenPrefix = 'SENTINEL_CONFIG_TOKEN:';
    const suffix = ':END';

    // Calculate available space for values
    const serverValueSpace = serverPlaceholder.length - serverPrefix.length - suffix.length;
    const tokenValueSpace = tokenPlaceholder.length - tokenPrefix.length - suffix.length;

    // Pad the values with underscores to match the original length
    const paddedServerUrl = serverUrl.padEnd(serverValueSpace, '_');
    const paddedToken = token.padEnd(tokenValueSpace, '_');

    // Create replacement strings
    const serverReplacement = serverPrefix + paddedServerUrl + suffix;
    const tokenReplacement = tokenPrefix + paddedToken + suffix;

    console.log('[Installer] Server placeholder length:', serverPlaceholder.length);
    console.log('[Installer] Server replacement length:', serverReplacement.length);
    console.log('[Installer] Token placeholder length:', tokenPlaceholder.length);
    console.log('[Installer] Token replacement length:', tokenReplacement.length);

    // Convert buffer to string for replacement (using latin1 to preserve binary data)
    let binaryString = templateBuffer.toString('latin1');

    // Check if placeholders exist
    if (!binaryString.includes(serverPlaceholder)) {
      console.error('[Installer] Server placeholder not found in template!');
      return null;
    }
    if (!binaryString.includes(tokenPlaceholder)) {
      console.error('[Installer] Token placeholder not found in template!');
      return null;
    }

    // Replace placeholders
    binaryString = binaryString.replace(serverPlaceholder, serverReplacement);
    binaryString = binaryString.replace(tokenPlaceholder, tokenReplacement);

    // Convert back to buffer
    const patchedBuffer = Buffer.from(binaryString, 'latin1');
    console.log('[Installer] Patched buffer size:', patchedBuffer.length, 'bytes');

    return patchedBuffer;
  } catch (error) {
    console.error('[Installer] Error generating EXE:', error);
    return null;
  }
}

'''

# Insert the EXE generator function before the PS1 generator
pattern = r"(// Generate a Windows PowerShell installer script with embedded configuration\nfunction generateWindowsInstallerScript)"
if re.search(pattern, content):
    content = re.sub(pattern, exe_generator_function + r'\1', content)
    print("Added generateWindowsInstallerExe function")
else:
    print("Could not find insertion point for generateWindowsInstallerExe")

# Update the handler to use EXE instead of PS1
old_handler = '''      case 'windows':
        installerContent = generateWindowsInstallerScript(serverUrl, token);
        // Debug: Verify replacements worked
        const hasServer = installerContent.includes('$Server = "' + serverUrl + '"');
        const hasToken = installerContent.includes('$Token = "' + token + '"');
        console.log('[Installer] Server URL embedded:', hasServer);
        console.log('[Installer] Token embedded:', hasToken);
        if (!hasServer || !hasToken) {
          console.log('[Installer] WARNING: Replacement may have failed!');
          console.log('[Installer] Script param section:', installerContent.substring(0, 500));
        }
        filename = 'sentinel-install.ps1';
        fileFilter = { name: 'PowerShell Script', extensions: ['ps1'] };
        break;'''

new_handler = '''      case 'windows': {
        // Generate EXE installer (more reliable than PowerShell)
        const exeBuffer = await generateWindowsInstallerExe(serverUrl, token);
        if (!exeBuffer) {
          return { success: false, error: 'Failed to generate Windows installer. Template file may be missing.' };
        }

        // Show save dialog for EXE
        const exeResult = await dialog.showSaveDialog(mainWindow!, {
          title: 'Save Pre-Configured Installer',
          defaultPath: 'sentinel-install.exe',
          filters: [
            { name: 'Windows Executable', extensions: ['exe'] },
            { name: 'All Files', extensions: ['*'] }
          ],
        });

        if (exeResult.canceled || !exeResult.filePath) {
          return { success: false, canceled: true };
        }

        await fs.promises.writeFile(exeResult.filePath, exeBuffer);
        console.log('[Installer] EXE installer saved to:', exeResult.filePath);

        return {
          success: true,
          filePath: exeResult.filePath,
          size: exeBuffer.length,
          instructions: 'Double-click the installer to run it. If prompted, click "Yes" to allow administrator access.',
          note: 'This installer has the server URL and enrollment token pre-configured. Just run it!',
        };
      }'''

if old_handler in content:
    content = content.replace(old_handler, new_handler)
    print("Updated Windows handler to generate EXE")
else:
    print("Could not find Windows handler to update")
    # Try to find partial match
    if "case 'windows':" in content and "generateWindowsInstallerScript" in content:
        print("Found partial match - manual update may be needed")

# Write the updated file
with open('D:/Projects/Sentinel/src/main/main.ts', 'w', encoding='utf-8') as f:
    f.write(content)

print("Done!")
