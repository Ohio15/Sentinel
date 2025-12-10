const fs = require('fs');

let server = fs.readFileSync('D:/Projects/Sentinel/src/main/server.ts', 'utf-8');

// Fix 1: Add /ws/dashboard to accepted paths
const oldPathCheck = `// Accept both /ws and /ws/agent paths for agent connections
      if (pathname === '/ws' || pathname === '/ws/agent') {`;

const newPathCheck = `// Accept /ws, /ws/agent for agents, and /ws/dashboard for dashboard
      const isDashboard = pathname === '/ws/dashboard';
      if (pathname === '/ws' || pathname === '/ws/agent' || isDashboard) {`;

if (server.includes(oldPathCheck)) {
  server = server.replace(oldPathCheck, newPathCheck);
  console.log('Fixed path check');
} else {
  console.log('Path check pattern not found');
}

// Fix 2: Add dashboard connection handling and message routing
const oldConnectionHandler = `this.wss.on('connection', (ws: WebSocket, req) => {
      console.log('New WebSocket connection from path:', req.url);

      let agentId: string | null = null;
      let authenticated = false;`;

const newConnectionHandler = `this.wss.on('connection', (ws: WebSocket, req) => {
      const url = new URL(req.url || '', \`http://\${req.headers.host}\`);
      const pathname = url.pathname;
      const isDashboardConnection = pathname === '/ws/dashboard';

      console.log('New WebSocket connection from path:', req.url, 'isDashboard:', isDashboardConnection);

      let agentId: string | null = null;
      let authenticated = isDashboardConnection; // Dashboard connections are authenticated via token in URL

      // For dashboard connections, store the ws for sending responses
      let dashboardWs: WebSocket | null = isDashboardConnection ? ws : null;`;

if (server.includes(oldConnectionHandler)) {
  server = server.replace(oldConnectionHandler, newConnectionHandler);
  console.log('Fixed connection handler');
} else {
  console.log('Connection handler pattern not found');
}

// Fix 3: Handle dashboard messages before the auth check
const oldAuthMessageHandler = `ws.on('message', async (data: Buffer) => {
        try {
          const message = JSON.parse(data.toString());

          // Handle authentication - support both direct fields and payload format
          if (message.type === 'auth') {`;

const newAuthMessageHandler = `ws.on('message', async (data: Buffer) => {
        try {
          const message = JSON.parse(data.toString());

          // Handle dashboard messages
          if (isDashboardConnection) {
            await this.handleDashboardMessage(message, ws);
            return;
          }

          // Handle authentication - support both direct fields and payload format
          if (message.type === 'auth') {`;

if (server.includes(oldAuthMessageHandler)) {
  server = server.replace(oldAuthMessageHandler, newAuthMessageHandler);
  console.log('Fixed auth message handler');
} else {
  console.log('Auth message handler pattern not found');
}

// Fix 4: Add handleDashboardMessage method before getLocalIpAddress
const insertBeforeMethod = `private getLocalIpAddress(): string {`;

const dashboardMethod = `private async handleDashboardMessage(message: any, ws: WebSocket): Promise<void> {
    console.log('Dashboard message received:', message.type, message.payload);

    try {
      switch (message.type) {
        case 'start_terminal': {
          const { deviceId, agentId, sessionId, cols, rows } = message.payload || {};
          console.log('Starting terminal for device:', deviceId, 'agent:', agentId);

          // Check if agent is connected
          if (!this.agentManager.isAgentConnected(agentId)) {
            console.log('Agent not connected:', agentId);
            ws.send(JSON.stringify({
              type: 'error',
              payload: { error: 'Agent not connected', sessionId }
            }));
            return;
          }

          // Forward to agent
          this.agentManager.sendToAgent(agentId, {
            type: 'start_terminal',
            sessionId,
            cols,
            rows,
          });

          // Register this dashboard ws for terminal output
          this.agentManager.registerDashboardSession(sessionId, ws);

          ws.send(JSON.stringify({
            type: 'terminal_started',
            payload: { sessionId }
          }));
          break;
        }

        case 'terminal_input': {
          const { agentId, sessionId, data } = message.payload || {};
          if (this.agentManager.isAgentConnected(agentId)) {
            this.agentManager.sendToAgent(agentId, {
              type: 'terminal_input',
              sessionId,
              data,
            });
          }
          break;
        }

        case 'terminal_resize': {
          const { agentId, sessionId, cols, rows } = message.payload || {};
          if (this.agentManager.isAgentConnected(agentId)) {
            this.agentManager.sendToAgent(agentId, {
              type: 'terminal_resize',
              sessionId,
              cols,
              rows,
            });
          }
          break;
        }

        case 'close_terminal': {
          const { agentId, sessionId } = message.payload || {};
          this.agentManager.unregisterDashboardSession(sessionId);
          if (this.agentManager.isAgentConnected(agentId)) {
            this.agentManager.sendToAgent(agentId, {
              type: 'close_terminal',
              sessionId,
            });
          }
          break;
        }

        default:
          console.log('Unknown dashboard message type:', message.type);
      }
    } catch (error) {
      console.error('Error handling dashboard message:', error);
      ws.send(JSON.stringify({
        type: 'error',
        payload: { error: String(error) }
      }));
    }
  }

  private getLocalIpAddress(): string {`;

if (server.includes(insertBeforeMethod)) {
  server = server.replace(insertBeforeMethod, dashboardMethod);
  console.log('Added handleDashboardMessage method');
} else {
  console.log('Could not find insertion point for handleDashboardMessage');
}

fs.writeFileSync('D:/Projects/Sentinel/src/main/server.ts', server);
console.log('server.ts updated');
