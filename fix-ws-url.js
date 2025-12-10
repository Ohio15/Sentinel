const fs = require('fs');
let ws = fs.readFileSync('D:/Projects/Sentinel/frontend/src/services/websocket.ts', 'utf-8');

const old = `this.isConnecting = true;
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = \`\${protocol}//\${window.location.host}/ws/dashboard?token=\${token}\`;`;

const replacement = `this.isConnecting = true;
    // Use VITE_API_URL to get the correct server host, removing /api suffix
    const apiUrl = import.meta.env.VITE_API_URL?.replace('/api', '') || window.location.origin;
    const url = new URL(apiUrl);
    const protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = \`\${protocol}//\${url.host}/ws/dashboard?token=\${token}\`;
    console.log('[WebSocket] Connecting to:', wsUrl);`;

if (ws.includes(old)) {
  ws = ws.replace(old, replacement);
  fs.writeFileSync('D:/Projects/Sentinel/frontend/src/services/websocket.ts', ws);
  console.log('Fixed WebSocket URL to use VITE_API_URL');
} else {
  console.log('Pattern not found - checking current content');
  console.log(ws.substring(0, 1500));
}
