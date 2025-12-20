const fs = require('fs');
const path = 'D:/Projects/Sentinel/src/renderer/pages/Settings.tsx';
let content = fs.readFileSync(path, 'utf8');

// Add External Backend UI section after Server Status section
const serverStatusEnd = `          </div>

          <div className="flex justify-end">
            <button onClick={handleSave} disabled={saving} className="btn btn-primary">`;

const externalBackendSection = `          </div>

          {/* External Backend */}
          <div className="card p-6">
            <h2 className="text-lg font-semibold text-text-primary mb-4">External Backend</h2>
            <p className="text-sm text-text-secondary mb-4">
              Connect to a Docker or standalone Sentinel backend to manage agents connected to that server.
              This enables commands, ping, and other operations for remotely-connected agents.
            </p>
            <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
              <div className="md:col-span-2">
                <label className="label">Backend URL</label>
                <input
                  type="url"
                  value={backendUrl}
                  onChange={e => setBackendUrl(e.target.value)}
                  className="input"
                  placeholder="http://localhost:8090"
                />
                <p className="text-xs text-text-secondary mt-1">
                  The URL of your Docker or standalone Sentinel server
                </p>
              </div>
              <div>
                <label className="label">Email</label>
                <input
                  type="email"
                  value={backendEmail}
                  onChange={e => setBackendEmail(e.target.value)}
                  className="input"
                  placeholder="admin@sentinel.local"
                />
              </div>
              <div>
                <label className="label">Password</label>
                <input
                  type="password"
                  value={backendPassword}
                  onChange={e => setBackendPassword(e.target.value)}
                  className="input"
                  placeholder="Enter password"
                />
              </div>
            </div>
            {backendError && (
              <div className="mt-4 p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg">
                <p className="text-sm text-danger">{backendError}</p>
              </div>
            )}
            <div className="flex items-center gap-4 mt-4">
              <button
                onClick={handleConnectBackend}
                disabled={backendConnecting}
                className="btn btn-primary"
              >
                {backendConnecting ? 'Connecting...' : 'Connect'}
              </button>
              {backendConnected && (
                <div className="flex items-center gap-2 text-success">
                  <svg className="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
                  </svg>
                  <span className="text-sm font-medium">Connected</span>
                </div>
              )}
            </div>
          </div>

          <div className="flex justify-end">
            <button onClick={handleSave} disabled={saving} className="btn btn-primary">`;

if (!content.includes('External Backend')) {
  content = content.replace(serverStatusEnd, externalBackendSection);
  fs.writeFileSync(path, content, 'utf8');
  console.log('Added External Backend UI section');
} else {
  console.log('External Backend section already exists');
}
