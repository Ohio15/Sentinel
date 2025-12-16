import React, { useEffect, useState, useRef, useCallback } from 'react';
import { useDeviceStore } from '../stores/deviceStore';
import { Terminal } from '../components/Terminal';
import { FileExplorer } from '../components/FileExplorer';
import { RemoteDesktop } from '../components/RemoteDesktop';
import { PerformanceView } from '../components/PerformanceView';

interface DeviceDetailProps {
  deviceId: string;
  onBack: () => void;
}


interface Command {
  id: string;
  deviceId: string;
  commandType: string;
  command: string;
  status: string;
  output: string | null;
  errorMessage: string | null;
  createdAt: string;
  startedAt: string | null;
  completedAt: string | null;
}

type Tab = 'overview' | 'performance' | 'terminal' | 'files' | 'remote' | 'commands' | 'history';

// Collapsible Section Component - Light Theme
function CollapsibleSection({
  title,
  children,
  defaultOpen = true,
  onCopy
}: {
  title: string;
  children: React.ReactNode;
  defaultOpen?: boolean;
  onCopy?: () => void;
}) {
  const [isOpen, setIsOpen] = useState(defaultOpen);
  const [copied, setCopied] = useState(false);

  const handleCopy = () => {
    if (onCopy) {
      onCopy();
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  };

  return (
    <div className="bg-surface border border-border rounded-lg overflow-hidden">
      <button
        onClick={() => setIsOpen(!isOpen)}
        className="w-full flex items-center justify-between p-4 hover:bg-gray-50 transition-colors"
      >
        <span className="text-lg font-semibold text-text-primary">{title}</span>
        <div className="flex items-center gap-2">
          {onCopy && (
            <button
              onClick={(e) => {
                e.stopPropagation();
                handleCopy();
              }}
              className="p-2 hover:bg-gray-100 rounded-lg transition-colors"
              title="Copy"
            >
              {copied ? (
                <CheckIcon className="w-4 h-4 text-success" />
              ) : (
                <CopyIcon className="w-4 h-4 text-text-secondary" />
              )}
            </button>
          )}
          {isOpen ? (
            <ChevronUpIcon className="w-5 h-5 text-text-secondary" />
          ) : (
            <ChevronDownIcon className="w-5 h-5 text-text-secondary" />
          )}
        </div>
      </button>
      {isOpen && (
        <div className="px-4 pb-4">
          {children}
        </div>
      )}
    </div>
  );
}

// Spec Row Component - Light Theme
function SpecRow({ label, value }: { label: string; value: string | undefined }) {
  return (
    <div className="flex py-2 border-b border-border last:border-0">
      <span className="w-48 text-text-secondary text-sm">{label}</span>
      <span className="text-text-primary text-sm flex-1">{value || 'N/A'}</span>
    </div>
  );
}

export function DeviceDetail({ deviceId, onBack }: DeviceDetailProps) {
  const { selectedDevice, metrics, loading, error, fetchDevice, fetchMetrics } = useDeviceStore();
  const [activeTab, setActiveTab] = useState<Tab>('overview');
  const [command, setCommand] = useState('');
  const [commandType, setCommandType] = useState('shell');
  const [commandOutput, setCommandOutput] = useState<string | null>(null);
  const [isExecuting, setIsExecuting] = useState(false);
  const [commandHistory, setCommandHistory] = useState<Command[]>([]);
  const [historyLoading, setHistoryLoading] = useState(false);
  const [expandedCommands, setExpandedCommands] = useState<Set<string>>(new Set());
  const [isEditingName, setIsEditingName] = useState(false);
  const [editedName, setEditedName] = useState('');

  useEffect(() => {
    fetchDevice(deviceId);
    fetchMetrics(deviceId, 24);
  }, [deviceId]);

  // Real-time polling for Performance tab (every 5 seconds when active)
  useEffect(() => {
    if (activeTab !== 'performance') return;
    
    // Immediately fetch on tab switch
    fetchMetrics(deviceId, 24);
    
    const interval = setInterval(() => {
      fetchMetrics(deviceId, 24);
    }, 5000);
    
    return () => clearInterval(interval);
  }, [activeTab, deviceId]);
  // Fetch command history when history tab is active
  useEffect(() => {
    if (activeTab === 'history' && deviceId) {
      setHistoryLoading(true);
      window.api.commands.getHistory(deviceId)
        .then(setCommandHistory)
        .catch(console.error)
        .finally(() => setHistoryLoading(false));
    }
  }, [activeTab, deviceId]);


  const handleSaveName = async () => {
    if (!selectedDevice || !editedName.trim()) return;
    try {
      await window.api.devices.update(selectedDevice.id, { displayName: editedName.trim() });
      await fetchDevice(deviceId); // Refresh device data
      setIsEditingName(false);
    } catch (error) {
      console.error('Failed to update device name:', error);
    }
  };

  const handleStartEdit = () => {
    setEditedName(selectedDevice?.displayName || selectedDevice?.hostname || '');
    setIsEditingName(true);
  };

  const handleCancelEdit = () => {
    setIsEditingName(false);
    setEditedName('');
  };

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

  // Helper to copy device specs to clipboard
  const copyDeviceSpecs = () => {
    if (!selectedDevice) return;
    const specs = [
      `Device name: ${selectedDevice.displayName || selectedDevice.hostname}`,
      `Processor: ${selectedDevice.cpuModel || 'N/A'}`,
      `Installed RAM: ${selectedDevice.totalMemory ? formatBytes(selectedDevice.totalMemory) : 'N/A'}`,
      `System type: ${selectedDevice.architecture}`,
    ].join("\n");
    navigator.clipboard.writeText(specs);
  };

  const copyWindowsSpecs = () => {
    if (!selectedDevice) return;
    const specs = [
      `Edition: ${selectedDevice.osType}`,
      `Version: ${selectedDevice.osVersion}`,
      `OS build: ${selectedDevice.osBuild || 'N/A'}`,
    ].join("\n");
    navigator.clipboard.writeText(specs);
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center h-full">
        <p className="text-text-secondary">Loading device...</p>
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex flex-col items-center justify-center h-full gap-4">
        <p className="text-danger">Error: {error}</p>
        <button onClick={onBack} className="btn btn-secondary">
          Back to Devices
        </button>
      </div>
    );
  }

  if (!selectedDevice) {
    return (
      <div className="flex flex-col items-center justify-center h-full gap-4">
        <p className="text-text-secondary">Device not found</p>
        <button onClick={onBack} className="btn btn-secondary">
          Back to Devices
        </button>
      </div>
    );
  }

  const tabs = [
    { id: 'overview', label: 'Overview', icon: MonitorIcon },
    { id: 'performance', label: 'Performance', icon: ChartIcon },
    { id: 'terminal', label: 'Terminal', icon: TerminalTabIcon },
    { id: 'files', label: 'Files', icon: FolderIcon },
    { id: 'remote', label: 'Remote Desktop', icon: DesktopIcon },
    { id: 'commands', label: 'Commands', icon: PlayIcon },
    { id: 'history', label: 'History', icon: HistoryIcon },
  ];

  // Extract data for summary cards - use real device fields
  const latestMetrics = metrics.length > 0 ? metrics[0] : null;

  // Calculate total storage from storage array
  const totalStorage = selectedDevice.storage?.reduce((sum, s) => sum + (s.total || 0), 0) || 0;
  const usedStorage = selectedDevice.storage?.reduce((sum, s) => sum + (s.used || 0), 0) || 0;

  // Get GPU info from device
  const gpuName = selectedDevice.gpu?.[0]?.name || 'Unknown GPU';
  const gpuMemory = selectedDevice.gpu?.[0]?.memory;

  // Get memory from device
  const totalMemory = selectedDevice.totalMemory || 0;
  const memoryUsed = latestMetrics?.memoryUsedBytes || 0;

  // CPU info
  const cpuModel = selectedDevice.cpuModel || 'Unknown CPU';

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
          {tabs.map(tab => {
            const Icon = tab.icon;
            return (
              <button
                key={tab.id}
                onClick={() => setActiveTab(tab.id as Tab)}
                className={`flex items-center gap-2 px-4 py-2 text-sm font-medium rounded-t-lg transition-colors ${
                  activeTab === tab.id
                    ? 'bg-surface border border-b-0 border-border text-primary'
                    : 'text-text-secondary hover:text-text-primary'
                }`}
              >
                <Icon className="w-4 h-4" />
                {tab.label}
              </button>
            );
          })}
        </div>
      </div>

      {/* Tab Content */}
      <div className="min-h-[500px]">
        {activeTab === 'overview' && (
          <div className="space-y-6">
            {/* Summary Cards - Gradient Style */}
            <div className="grid grid-cols-4 gap-4">
              {/* Storage Card */}
              <div className="bg-gradient-to-br from-blue-500 to-blue-700 rounded-xl p-4 text-white shadow-lg">
                <div className="flex items-center gap-3 mb-3">
                  <HardDriveIcon className="w-8 h-8" />
                  <span className="text-lg font-medium">Storage</span>
                </div>
                <div className="text-2xl font-bold">
                  {totalStorage ? `${formatBytes(usedStorage)} / ${formatBytes(totalStorage)}` : 'N/A'}
                </div>
                <div className="text-sm opacity-80 mt-1">
                  {totalStorage && latestMetrics?.diskPercent != null
                    ? `${(100 - latestMetrics.diskPercent).toFixed(0)}% free`
                    : ''}
                </div>
              </div>

              {/* GPU Card */}
              <div className="bg-gradient-to-br from-purple-500 to-purple-700 rounded-xl p-4 text-white shadow-lg">
                <div className="flex items-center gap-3 mb-3">
                  <GpuIcon className="w-8 h-8" />
                  <span className="text-lg font-medium">Graphics</span>
                </div>
                <div className="text-lg font-bold truncate" title={gpuName}>
                  {gpuName}
                </div>
                <div className="text-sm opacity-80 mt-1">
                  {gpuMemory ? formatBytes(gpuMemory) : ''}
                </div>
              </div>

              {/* RAM Card */}
              <div className="bg-gradient-to-br from-teal-500 to-teal-700 rounded-xl p-4 text-white shadow-lg">
                <div className="flex items-center gap-3 mb-3">
                  <MemoryIcon className="w-8 h-8" />
                  <span className="text-lg font-medium">RAM</span>
                </div>
                <div className="text-2xl font-bold">
                  {totalMemory ? formatBytes(totalMemory) : 'N/A'}
                </div>
                <div className="text-sm opacity-80 mt-1">
                  {latestMetrics?.memoryPercent != null
                    ? `${latestMetrics.memoryPercent.toFixed(0)}% in use`
                    : ''}
                </div>
              </div>

              {/* CPU Card */}
              <div className="bg-gradient-to-br from-orange-500 to-orange-700 rounded-xl p-4 text-white shadow-lg">
                <div className="flex items-center gap-3 mb-3">
                  <CpuIcon className="w-8 h-8" />
                  <span className="text-lg font-medium">Processor</span>
                </div>
                <div className="text-lg font-bold truncate" title={cpuModel}>
                  {cpuModel}
                </div>
                <div className="text-sm opacity-80 mt-1">
                  {latestMetrics?.cpuPercent != null
                    ? `${latestMetrics.cpuPercent.toFixed(0)}% utilization`
                    : ''}
                </div>
              </div>
            </div>

            {/* Device Header with PC Icon - Light Theme */}
            <div className="bg-surface border border-border rounded-xl p-6">
              <div className="flex items-start gap-6">
                <div className="w-24 h-24 bg-primary-light rounded-xl flex items-center justify-center">
                  <MonitorIcon className="w-16 h-16 text-primary" />
                </div>
                <div className="flex-1">
                  <div className="flex items-center gap-3 mb-2">
                    {isEditingName ? (
                      <>
                        <input
                          type="text"
                          value={editedName}
                          onChange={(e) => setEditedName(e.target.value)}
                          onKeyDown={(e) => {
                            if (e.key === 'Enter') handleSaveName();
                            if (e.key === 'Escape') handleCancelEdit();
                          }}
                          className="text-2xl font-bold text-text-primary bg-gray-100 border border-border rounded-lg px-3 py-1 focus:outline-none focus:ring-2 focus:ring-primary"
                          autoFocus
                        />
                        <button
                          onClick={handleSaveName}
                          className="p-2 hover:bg-green-100 rounded-lg transition-colors"
                          title="Save"
                        >
                          <CheckIcon className="w-4 h-4 text-success" />
                        </button>
                        <button
                          onClick={handleCancelEdit}
                          className="p-2 hover:bg-red-100 rounded-lg transition-colors"
                          title="Cancel"
                        >
                          <CloseIcon className="w-4 h-4 text-danger" />
                        </button>
                      </>
                    ) : (
                      <>
                        <h2 className="text-2xl font-bold text-text-primary">
                          {selectedDevice.displayName || selectedDevice.hostname}
                        </h2>
                        <button
                          onClick={handleStartEdit}
                          className="p-2 hover:bg-gray-100 rounded-lg transition-colors"
                          title="Rename this PC"
                        >
                          <EditIcon className="w-4 h-4 text-text-secondary" />
                        </button>
                      </>
                    )}
                  </div>
                  <p className="text-text-secondary mb-4">
                    {selectedDevice.model || selectedDevice.manufacturer || selectedDevice.osType}
                  </p>
                  <span className="text-text-secondary text-sm">
                    Last seen: {new Date(selectedDevice.lastSeen).toLocaleString()}
                  </span>
                </div>
              </div>
            </div>

            {/* Device Specifications - Collapsible */}
            <CollapsibleSection title="Device specifications" onCopy={copyDeviceSpecs}>
              <div className="space-y-1">
                <SpecRow label="Device name" value={selectedDevice.displayName || selectedDevice.hostname} />
                <SpecRow label="Processor" value={selectedDevice.cpuModel} />
                <SpecRow label="CPU Cores" value={selectedDevice.cpuCores ? `${selectedDevice.cpuCores} cores (${selectedDevice.cpuThreads || selectedDevice.cpuCores} threads)` : undefined} />
                <SpecRow label="Installed RAM" value={totalMemory ? formatBytes(totalMemory) : undefined} />
                <SpecRow label="Device ID" value={selectedDevice.id} />
                <SpecRow label="System type" value={selectedDevice.architecture} />
                <SpecRow label="Manufacturer" value={selectedDevice.manufacturer} />
                <SpecRow label="Model" value={selectedDevice.model} />
                <SpecRow label="Serial Number" value={selectedDevice.serialNumber} />
              </div>
            </CollapsibleSection>

            {/* OS Specifications - Collapsible */}
            <CollapsibleSection title="Operating System" onCopy={copyWindowsSpecs}>
              <div className="space-y-1">
                <SpecRow label="OS Type" value={selectedDevice.osType} />
                <SpecRow label="Version" value={selectedDevice.osVersion} />
                <SpecRow label="Build" value={selectedDevice.osBuild} />
                <SpecRow label="Platform" value={selectedDevice.platform} />
                <SpecRow label="Platform Family" value={selectedDevice.platformFamily} />
                <SpecRow label="Domain" value={selectedDevice.domain} />
              </div>
            </CollapsibleSection>

            {/* Network Info */}
            <CollapsibleSection title="Network">
              <div className="space-y-1">
                <SpecRow label="IP Address" value={selectedDevice.ipAddress} />
                <SpecRow label="Public IP" value={selectedDevice.publicIp} />
                <SpecRow label="MAC Address" value={selectedDevice.macAddress} />
              </div>
            </CollapsibleSection>

                        {/* Storage Info - Windows This PC Style */}
            {selectedDevice.storage && selectedDevice.storage.length > 0 && (
              <CollapsibleSection title="Devices and drives">
                <div className="grid grid-cols-2 lg:grid-cols-3 gap-4">
                  {selectedDevice.storage.map((drive, idx) => {
                    const usagePercent = drive.percent || 0;
                    const isNearFull = usagePercent > 90;
                    const driveLetter = drive.mountpoint?.match(/([A-Z]:)/)?.[1] || drive.mountpoint || drive.device;
                    const driveName = driveLetter === 'C:' ? 'Local Disk' : driveLetter === 'D:' ? 'Data' : 'Volume';
                    return (
                      <div key={idx} className="p-4 bg-white dark:bg-slate-800 border border-gray-200 dark:border-slate-600 rounded-lg hover:bg-gray-50 dark:hover:bg-slate-700 cursor-pointer transition-colors">
                        <div className="flex items-start gap-3">
                          <div className="flex-shrink-0">
                            <svg className="w-12 h-12 text-blue-500" viewBox="0 0 24 24" fill="currentColor">
                              <path d="M4 4h16v16H4V4zm2 2v12h12V6H6zm2 2h8v2H8V8zm0 4h8v2H8v-2z" />
                            </svg>
                          </div>
                          <div className="flex-1 min-w-0">
                            <div className="font-medium text-text-primary truncate">{driveName} ({driveLetter})</div>
                            <div className="w-full bg-gray-200 dark:bg-slate-600 h-4 mt-2 rounded-sm overflow-hidden">
                              <div className={`h-full transition-all ${isNearFull ? 'bg-red-500' : 'bg-blue-500'}`} style={{ width: `${usagePercent}%` }} />
                            </div>
                            <div className="text-xs text-text-secondary mt-1">{formatBytes(drive.free)} free of {formatBytes(drive.total)}</div>
                            <div className="text-xs text-text-secondary">{drive.fstype || 'NTFS'}</div>
                          </div>
                        </div>
                      </div>
                    );
                  })}
                </div>
              </CollapsibleSection>
            )}

            {/* Agent Info */}
            <CollapsibleSection title="Agent information">
              <div className="space-y-1">
                <SpecRow label="Agent Version" value={selectedDevice.agentVersion} />
                <SpecRow label="Agent ID" value={selectedDevice.agentId} />
                <SpecRow label="Enrolled" value={new Date(selectedDevice.createdAt).toLocaleDateString()} />
              </div>
            </CollapsibleSection>

            
          </div>
        )}

                {activeTab === 'performance' && (
          <div className="card overflow-hidden" style={{ height: 'calc(100vh - 250px)' }}>
            <PerformanceView
              metrics={metrics}
              systemInfo={{
                cpuModel: selectedDevice.cpuModel,
                cpuCores: selectedDevice.cpuCores,
                cpuThreads: selectedDevice.cpuThreads,
                cpuSpeed: selectedDevice.cpuSpeed,
                totalMemory: selectedDevice.totalMemory,
                gpu: selectedDevice.gpu,
                storage: selectedDevice.storage,
                bootTime: selectedDevice.bootTime,
              }}
            />
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

        {activeTab === 'history' && (
          <div className="card p-4">
            <h3 className="font-semibold text-text-primary mb-4">Command History</h3>
            {historyLoading ? (
              <p className="text-text-secondary">Loading...</p>
            ) : commandHistory.length === 0 ? (
              <p className="text-text-secondary">No commands have been executed on this device yet.</p>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full">
                  <thead>
                    <tr className="border-b border-border">
                      <th className="text-left py-2 px-3 text-sm font-medium text-text-secondary">Time</th>
                      <th className="text-left py-2 px-3 text-sm font-medium text-text-secondary">Type</th>
                      <th className="text-left py-2 px-3 text-sm font-medium text-text-secondary">Command</th>
                      <th className="text-left py-2 px-3 text-sm font-medium text-text-secondary">Status</th>
                    </tr>
                  </thead>
                  <tbody>
                    {commandHistory.map((cmd) => (
                      <React.Fragment key={cmd.id}>
                        <tr
                          className={`border-b border-border hover:bg-gray-50 cursor-pointer ${expandedCommands.has(cmd.id) ? 'bg-gray-50' : ''}`}
                          onClick={() => {
                            const newExpanded = new Set(expandedCommands);
                            if (newExpanded.has(cmd.id)) {
                              newExpanded.delete(cmd.id);
                            } else {
                              newExpanded.add(cmd.id);
                            }
                            setExpandedCommands(newExpanded);
                          }}
                        >
                          <td className="py-2 px-3 text-sm text-text-secondary">
                            <span className="inline-flex items-center gap-2">
                              <span className={`transition-transform ${expandedCommands.has(cmd.id) ? 'rotate-90' : ''}`}>▶</span>
                              {new Date(cmd.createdAt).toLocaleString()}
                            </span>
                          </td>
                          <td className="py-2 px-3 text-sm text-text-primary capitalize">{cmd.commandType}</td>
                          <td className="py-2 px-3 text-sm font-mono text-text-primary truncate max-w-xs" title={cmd.command}>
                            {cmd.command}
                          </td>
                          <td className="py-2 px-3">
                            <span className={`px-2 py-0.5 text-xs rounded-full ${
                              cmd.status === 'completed' ? 'bg-green-100 text-green-800' :
                              cmd.status === 'failed' ? 'bg-red-100 text-red-800' :
                              cmd.status === 'pending' ? 'bg-yellow-100 text-yellow-800' :
                              'bg-gray-100 text-gray-800'
                            }`}>
                              {cmd.status}
                            </span>
                          </td>
                        </tr>
                        {expandedCommands.has(cmd.id) && (
                          <tr className="bg-gray-50">
                            <td colSpan={4} className="p-4">
                              <div className="space-y-2">
                                <div className="text-xs text-text-secondary">
                                  Started: {cmd.startedAt ? new Date(cmd.startedAt).toLocaleString() : 'N/A'} |
                                  Completed: {cmd.completedAt ? new Date(cmd.completedAt).toLocaleString() : 'N/A'}
                                </div>
                                {cmd.output && (
                                  <div>
                                    <div className="text-xs font-semibold text-text-secondary mb-1">Output:</div>
                                    <pre className="bg-gray-900 text-gray-100 p-3 rounded-lg text-xs overflow-auto max-h-48 font-mono whitespace-pre-wrap">
                                      {cmd.output}
                                    </pre>
                                  </div>
                                )}
                                {cmd.errorMessage && (
                                  <div>
                                    <div className="text-xs font-semibold text-red-600 mb-1">Error:</div>
                                    <pre className="bg-red-50 text-red-800 p-3 rounded-lg text-xs overflow-auto max-h-48 font-mono whitespace-pre-wrap">
                                      {cmd.errorMessage}
                                    </pre>
                                  </div>
                                )}
                                {!cmd.output && !cmd.errorMessage && (
                                  <div className="text-xs text-text-secondary italic">No output recorded</div>
                                )}
                              </div>
                            </td>
                          </tr>
                        )}
                      </React.Fragment>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

// Helper function to format bytes
function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
}

function RefreshIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
    </svg>
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

// Icons
function BackIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
    </svg>
  );
}

function MonitorIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z" />
    </svg>
  );
}

function TerminalTabIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 9l3 3-3 3m5 0h3M5 20h14a2 2 0 002-2V6a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z" />
    </svg>
  );
}

function FolderIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-6l-2-2H5a2 2 0 00-2 2z" />
    </svg>
  );
}

function DesktopIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z" />
    </svg>
  );
}

function PlayIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z" />
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
    </svg>
  );
}

function HistoryIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
    </svg>
  );
}
function ChartIcon({ className }: { className?: string }) {  return (    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z" />    </svg>  );}

function HardDriveIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 7v10c0 2.21 3.582 4 8 4s8-1.79 8-4V7M4 7c0 2.21 3.582 4 8 4s8-1.79 8-4M4 7c0-2.21 3.582-4 8-4s8 1.79 8 4m0 5c0 2.21-3.582 4-8 4s-8-1.79-8-4" />
    </svg>
  );
}

function GpuIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 3v2m6-2v2M9 19v2m6-2v2M5 9H3m2 6H3m18-6h-2m2 6h-2M7 19h10a2 2 0 002-2V7a2 2 0 00-2-2H7a2 2 0 00-2 2v10a2 2 0 002 2zM9 9h6v6H9V9z" />
    </svg>
  );
}

function MemoryIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10" />
    </svg>
  );
}

function CpuIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 3v2m6-2v2M9 19v2m6-2v2M5 9H3m2 6H3m18-6h-2m2 6h-2M7 19h10a2 2 0 002-2V7a2 2 0 00-2-2H7a2 2 0 00-2 2v10a2 2 0 002 2zM9 9h6v6H9V9z" />
    </svg>
  );
}

function ChevronDownIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
    </svg>
  );
}

function ChevronUpIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 15l7-7 7 7" />
    </svg>
  );
}

function CopyIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" />
    </svg>
  );
}

function CheckIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
    </svg>
  );
}

function CloseIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
    </svg>
  );
}

function EditIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" />
    </svg>
  );
}
