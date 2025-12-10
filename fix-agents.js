const fs = require('fs');

let agents = fs.readFileSync('D:/Projects/Sentinel/src/main/agents.ts', 'utf-8');

// Add dashboardSessions property to the class
const oldProperties = `private terminalSessions: Map<string, TerminalSession> = new Map();`;
const newProperties = `private terminalSessions: Map<string, TerminalSession> = new Map();
  private dashboardSessions: Map<string, WebSocket> = new Map();`;

if (agents.includes(oldProperties)) {
  agents = agents.replace(oldProperties, newProperties);
  console.log('Added dashboardSessions property');
} else {
  console.log('Properties pattern not found');
}

// Add registerDashboardSession and unregisterDashboardSession methods
// Insert before handleTerminalOutput method
const insertBefore = `private handleTerminalOutput(message: any): void {`;

const newMethods = `registerDashboardSession(sessionId: string, ws: WebSocket): void {
    this.dashboardSessions.set(sessionId, ws);
    console.log('Registered dashboard session:', sessionId);
  }

  unregisterDashboardSession(sessionId: string): void {
    this.dashboardSessions.delete(sessionId);
    console.log('Unregistered dashboard session:', sessionId);
  }

  private handleTerminalOutput(message: any): void {`;

if (agents.includes(insertBefore)) {
  agents = agents.replace(insertBefore, newMethods);
  console.log('Added dashboard session methods');
} else {
  console.log('Insert point for dashboard methods not found');
}

// Update handleTerminalOutput to send to dashboard WebSocket instead of IPC
const oldTerminalOutput = `private handleTerminalOutput(message: any): void {
    this.notifyRenderer('terminal:data', {
      sessionId: message.sessionId,
      data: message.data,
    });
  }`;

const newTerminalOutput = `private handleTerminalOutput(message: any): void {
    const dashboardWs = this.dashboardSessions.get(message.sessionId);
    if (dashboardWs && dashboardWs.readyState === 1) {
      dashboardWs.send(JSON.stringify({
        type: 'terminal_output',
        payload: {
          sessionId: message.sessionId,
          data: message.data,
        }
      }));
    }
    // Also notify renderer via IPC for backward compatibility
    this.notifyRenderer('terminal:data', {
      sessionId: message.sessionId,
      data: message.data,
    });
  }`;

if (agents.includes(oldTerminalOutput)) {
  agents = agents.replace(oldTerminalOutput, newTerminalOutput);
  console.log('Updated handleTerminalOutput');
} else {
  console.log('handleTerminalOutput pattern not found');
}

fs.writeFileSync('D:/Projects/Sentinel/src/main/agents.ts', agents);
console.log('agents.ts updated');
