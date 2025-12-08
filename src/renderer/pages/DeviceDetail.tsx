import React, { useEffect, useState } from 'react';
import { useDeviceStore } from '../stores/deviceStore';
import { Terminal } from '../components/Terminal';
import { FileExplorer } from '../components/FileExplorer';
import { RemoteDesktop } from '../components/RemoteDesktop';
import { MetricsChart } from '../components/MetricsChart';

interface DeviceDetailProps {
  deviceId: string;
  onBack: () => void;
}

type Tab = 'overview' | 'terminal' | 'files' | 'remote' | 'commands';

export function DeviceDetail({ deviceId, onBack }: DeviceDetailProps) {
  const { selectedDevice, metrics, fetchDevice, fetchMetrics } = useDeviceStore();
  const [activeTab, setActiveTab] = useState<Tab>('overview');
  const [command, setCommand] = useState('');
  const [commandType, setCommandType] = useState('shell');
  const [commandOutput, setCommandOutput] = useState<string | null>(null);
  const [isExecuting, setIsExecuting] = useState(false);

  useEffect(() => {
    fetchDevice(deviceId);
    fetchMetrics(deviceId, 24);
    const interval = setInterval(() => fetchMetrics(deviceId, 24), 60000);
    return () => clearInterval(interval);
  }, [deviceId]);

  const executeCommand = async () => {
    if (!command.trim() || !selectedDevice) return;

    setIsExecuting(true);
    setCommandOutput(null);

    try {
      const result = await window.api.commands.execute(deviceId, command, commandType);
      setCommandOutput(result.output || 'Command executed successfully');
    } catch (error: any) {
      setCommandOutput(`Error: ${error.message}`);
    } finally {
      setIsExecuting(false);
    }
  };

  if (!selectedDevice) {
    return (
      <div className="flex items-center justify-center h-full">
        <p className="text-text-secondary">Loading device...</p>
      </div>
    );
  }

  const tabs = [
    { id: 'overview', label: 'Overview' },
    { id: 'terminal', label: 'Terminal' },
    { id: 'files', label: 'Files' },
    { id: 'remote', label: 'Remote Desktop' },
    { id: 'commands', label: 'Commands' },
  ];

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center gap-4">
        <button onClick={onBack} className="btn btn-secondary">
          <BackIcon className="w-4 h-4 mr-2" />
          Back
        </button>
        <div className="flex-1">
          <h1 className="text-2xl font-bold text-text-primary">
            {selectedDevice.displayName || selectedDevice.hostname}
          </h1>
          <p className="text-text-secondary">
            {selectedDevice.osType} {selectedDevice.osVersion} - {selectedDevice.ipAddress}
          </p>
        </div>
        <div className={`status-indicator ${
          selectedDevice.status === 'online' ? 'status-online' :
          selectedDevice.status === 'warning' ? 'status-warning' :
          selectedDevice.status === 'critical' ? 'status-critical' : 'status-offline'
        }`} />
        <span className={`badge ${
          selectedDevice.status === 'online' ? 'badge-success' :
          selectedDevice.status === 'warning' ? 'badge-warning' :
          selectedDevice.status === 'critical' ? 'badge-danger' : 'bg-gray-100 text-gray-600'
        }`}>
          {selectedDevice.status.charAt(0).toUpperCase() + selectedDevice.status.slice(1)}
        </span>
      </div>

      {/* Tabs */}
      <div className="border-b border-border">
        <div className="flex gap-1">
          {tabs.map(tab => (
            <button
              key={tab.id}
              onClick={() => setActiveTab(tab.id as Tab)}
              className={`px-4 py-2 text-sm font-medium rounded-t-lg transition-colors ${
                activeTab === tab.id
                  ? 'bg-surface border border-b-0 border-border text-primary'
                  : 'text-text-secondary hover:text-text-primary'
              }`}
            >
              {tab.label}
            </button>
          ))}
        </div>
      </div>

      {/* Tab Content */}
      <div className="min-h-[500px]">
        {activeTab === 'overview' && (
          <div className="space-y-6">
            {/* Device Info */}
            <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
              <div className="card p-4">
                <h3 className="font-semibold text-text-primary mb-4">System Information</h3>
                <dl className="space-y-2">
                  <InfoRow label="Hostname" value={selectedDevice.hostname} />
                  <InfoRow label="OS" value={`${selectedDevice.osType} ${selectedDevice.osVersion}`} />
                  <InfoRow label="Architecture" value={selectedDevice.architecture} />
                  <InfoRow label="Agent Version" value={selectedDevice.agentVersion} />
                  <InfoRow label="IP Address" value={selectedDevice.ipAddress} />
                  <InfoRow label="MAC Address" value={selectedDevice.macAddress} />
                  <InfoRow label="Last Seen" value={new Date(selectedDevice.lastSeen).toLocaleString()} />
                </dl>
              </div>

              {/* Current Metrics */}
              {metrics.length > 0 && (
                <div className="card p-4">
                  <h3 className="font-semibold text-text-primary mb-4">Current Metrics</h3>
                  <div className="space-y-4">
                    <MetricBar
                      label="CPU"
                      value={metrics[0].cpuPercent}
                      color="blue"
                    />
                    <MetricBar
                      label="Memory"
                      value={metrics[0].memoryPercent}
                      color="green"
                    />
                    <MetricBar
                      label="Disk"
                      value={metrics[0].diskPercent}
                      color="yellow"
                    />
                  </div>
                </div>
              )}
            </div>

            {/* Metrics Chart */}
            {metrics.length > 0 && (
              <div className="card p-4">
                <h3 className="font-semibold text-text-primary mb-4">Resource Usage (24h)</h3>
                <MetricsChart metrics={metrics} />
              </div>
            )}
          </div>
        )}

        {activeTab === 'terminal' && (
          <div className="card overflow-hidden">
            <Terminal deviceId={deviceId} isOnline={selectedDevice.status === 'online'} />
          </div>
        )}

        {activeTab === 'files' && (
          <div className="card overflow-hidden">
            <FileExplorer deviceId={deviceId} isOnline={selectedDevice.status === 'online'} />
          </div>
        )}

        {activeTab === 'remote' && (
          <div className="card overflow-hidden">
            <RemoteDesktop deviceId={deviceId} isOnline={selectedDevice.status === 'online'} />
          </div>
        )}

        {activeTab === 'commands' && (
          <div className="space-y-4">
            <div className="card p-4">
              <h3 className="font-semibold text-text-primary mb-4">Execute Command</h3>
              <div className="flex gap-4 mb-4">
                <select
                  value={commandType}
                  onChange={e => setCommandType(e.target.value)}
                  className="input w-40"
                >
                  <option value="shell">Shell</option>
                  <option value="powershell">PowerShell</option>
                  <option value="bash">Bash</option>
                </select>
                <input
                  type="text"
                  value={command}
                  onChange={e => setCommand(e.target.value)}
                  onKeyDown={e => e.key === 'Enter' && executeCommand()}
                  placeholder="Enter command..."
                  className="input flex-1"
                  disabled={selectedDevice.status !== 'online'}
                />
                <button
                  onClick={executeCommand}
                  disabled={isExecuting || selectedDevice.status !== 'online' || !command.trim()}
                  className="btn btn-primary"
                >
                  {isExecuting ? 'Executing...' : 'Execute'}
                </button>
              </div>
              {commandOutput && (
                <pre className="bg-gray-900 text-gray-100 p-4 rounded-lg text-sm overflow-auto max-h-64 font-mono">
                  {commandOutput}
                </pre>
              )}
            </div>

            <div className="card p-4">
              <h3 className="font-semibold text-text-primary mb-4">Quick Actions</h3>
              <div className="flex flex-wrap gap-2">
                <QuickAction
                  label="System Info"
                  command={selectedDevice.osType.toLowerCase().includes('windows') ? 'systeminfo' : 'uname -a'}
                  onExecute={setCommand}
                />
                <QuickAction
                  label="Disk Usage"
                  command={selectedDevice.osType.toLowerCase().includes('windows') ? 'wmic logicaldisk get size,freespace,caption' : 'df -h'}
                  onExecute={setCommand}
                />
                <QuickAction
                  label="Running Processes"
                  command={selectedDevice.osType.toLowerCase().includes('windows') ? 'tasklist' : 'ps aux'}
                  onExecute={setCommand}
                />
                <QuickAction
                  label="Network Info"
                  command={selectedDevice.osType.toLowerCase().includes('windows') ? 'ipconfig /all' : 'ifconfig'}
                  onExecute={setCommand}
                />
                <QuickAction
                  label="Uptime"
                  command={selectedDevice.osType.toLowerCase().includes('windows') ? 'net statistics workstation' : 'uptime'}
                  onExecute={setCommand}
                />
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

function InfoRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex justify-between">
      <dt className="text-text-secondary">{label}</dt>
      <dd className="text-text-primary font-medium">{value}</dd>
    </div>
  );
}

function MetricBar({ label, value, color }: { label: string; value: number; color: string }) {
  const colors = {
    blue: 'bg-blue-500',
    green: 'bg-green-500',
    yellow: 'bg-yellow-500',
    red: 'bg-red-500',
  };

  const bgColor = value > 90 ? colors.red : colors[color as keyof typeof colors];

  return (
    <div>
      <div className="flex justify-between mb-1">
        <span className="text-sm text-text-secondary">{label}</span>
        <span className="text-sm font-medium">{value.toFixed(1)}%</span>
      </div>
      <div className="h-2 bg-gray-200 rounded-full overflow-hidden">
        <div
          className={`h-full ${bgColor} transition-all duration-500`}
          style={{ width: `${Math.min(100, value)}%` }}
        />
      </div>
    </div>
  );
}

function QuickAction({ label, command, onExecute }: { label: string; command: string; onExecute: (cmd: string) => void }) {
  return (
    <button
      onClick={() => onExecute(command)}
      className="btn btn-secondary text-sm"
    >
      {label}
    </button>
  );
}

function BackIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
    </svg>
  );
}
