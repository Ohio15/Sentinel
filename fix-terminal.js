const fs = require('fs');
const path = 'D:/Projects/Sentinel/src/renderer/components/Terminal.tsx';
let content = fs.readFileSync(path, 'utf-8');

// Check if already modified
if (content.includes("console.log('[Terminal]")) {
  console.log('Debug logging already present');
  process.exit(0);
}

// Use regex to find and replace the handleConnect function
const oldPattern = /const handleConnect = async \(\) => \{\s*if \(!isOnline\) return;\s*setConnecting\(true\);\s*try \{\s*const result = await window\.api\.terminal\.start\(deviceId\);/;

const newCode = `const handleConnect = async () => {
    console.log('[Terminal] handleConnect called, deviceId:', deviceId, 'isOnline:', isOnline);
    if (!isOnline) {
      console.log('[Terminal] Device is offline, aborting');
      return;
    }

    setConnecting(true);
    try {
      console.log('[Terminal] Calling window.api.terminal.start...');
      const result = await window.api.terminal.start(deviceId);
      console.log('[Terminal] terminal.start result:', result);`;

if (oldPattern.test(content)) {
  content = content.replace(oldPattern, newCode);
  fs.writeFileSync(path, content);
  console.log('Added debug logging to Terminal.tsx');
} else {
  console.log('Pattern not found, showing handleConnect area:');
  const match = content.match(/const handleConnect[\s\S]{0,500}/);
  console.log(match ? match[0] : 'handleConnect not found');
}
