const fs = require('fs');

// Clean up unused code in Devices.tsx
let devicesTsx = fs.readFileSync('src/renderer/pages/Devices.tsx', 'utf8');

// Remove MsiResult interface
devicesTsx = devicesTsx.replace(
  `interface MsiResult {
  type: 'success' | 'error';
  message: string;
  installCommand?: string;
}

`,
  ''
);
console.log('Removed MsiResult interface');

// Remove unused state variables
devicesTsx = devicesTsx.replace(
  `const [msiDownloading, setMsiDownloading] = useState(false);
  const [msiResult, setMsiResult] = useState<MsiResult | null>(null);
  `,
  ''
);
console.log('Removed unused msiDownloading and msiResult state');

// Remove handleMsiDownload function
const handleMsiDownloadStart = devicesTsx.indexOf('const handleMsiDownload = async () => {');
const handleMsiDownloadEnd = devicesTsx.indexOf('const handlePowerShellInstall = async () => {');

if (handleMsiDownloadStart !== -1 && handleMsiDownloadEnd !== -1) {
  const before = devicesTsx.substring(0, handleMsiDownloadStart);
  const after = devicesTsx.substring(handleMsiDownloadEnd);
  devicesTsx = before + after;
  console.log('Removed handleMsiDownload function');
}

fs.writeFileSync('src/renderer/pages/Devices.tsx', devicesTsx);
console.log('Cleaned up src/renderer/pages/Devices.tsx');
