import { useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import {
  ArrowLeft,
  Monitor,
  Cpu,
  HardDrive,
  Activity,
  Terminal as TerminalIcon,
  Play,
  Tag,
  FolderOpen,
  MonitorPlay,
  History,
  ChevronDown,
  ChevronUp,
  Copy,
  Check,
  Edit3,
  MemoryStick,
  Trash2,
  RefreshCw,
} from 'lucide-react';
import { Header } from '@/components/layout';
import { Terminal, FileBrowser, RemoteDesktop } from '@/components/device';
import { Card, CardContent, Badge, Button, Modal } from '@/components/ui';
import api from '@/services/api';
import type { Device, DeviceMetrics, Command } from '@/types';
import toast from 'react-hot-toast';
import {
  XAxis,
  YAxis,
  Tooltip,
  ResponsiveContainer,
  AreaChart,
  Area,
} from 'recharts';

// Helper function to format bytes to human readable format
function formatBytes(bytes: number, decimals = 2): string {
  if (bytes === 0) return '0 Bytes';
  const k = 1024;
  const dm = decimals < 0 ? 0 : decimals;
  const sizes = ['Bytes', 'KB', 'MB', 'GB', 'TB', 'PB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(dm)) + ' ' + sizes[i];
}

// Helper to format RAM in GB
function formatRAMInGB(bytes: number): string {
  if (bytes === 0) return '0';
  const gb = bytes / (1024 * 1024 * 1024);
  return gb.toFixed(1);
}

// Collapsible Section Component
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
    <div className="bg-[#2d2d2d] rounded-lg overflow-hidden">
      <div
        className="flex items-center justify-between p-4 cursor-pointer hover:bg-[#3d3d3d] transition-colors"
        onClick={() => setIsOpen(!isOpen)}
      >
        <div className="flex items-center gap-3">
          {isOpen ? (
            <ChevronUp className="w-5 h-5 text-gray-400" />
          ) : (
            <ChevronDown className="w-5 h-5 text-gray-400" />
          )}
          <span className="text-white font-medium">{title}</span>
        </div>
        {onCopy && (
          <button
            onClick={(e) => {
              e.stopPropagation();
              handleCopy();
            }}
            className="flex items-center gap-1 px-3 py-1 text-sm text-gray-400 hover:text-white hover:bg-[#4d4d4d] rounded transition-colors"
          >
            {copied ? (
              <>
                <Check className="w-4 h-4" />
                Copied
              </>
            ) : (
              <>
                <Copy className="w-4 h-4" />
                Copy
              </>
            )}
          </button>
        )}
      </div>
      {isOpen && (
        <div className="px-4 pb-4">
          {children}
        </div>
      )}
    </div>
  );
}

// Spec Row Component
function SpecRow({ label, value }: { label: string; value: string | undefined }) {
  if (!value) return null;
  return (
    <div className="flex justify-between py-2.5 border-b border-[#3d3d3d] last:border-b-0">
      <span className="text-gray-400">{label}</span>
      <span className="text-white text-right max-w-[60%]">{value}</span>
    </div>
  );
}

export function DeviceDetail() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [activeTab, setActiveTab] = useState<'overview' | 'metrics' | 'commands'>('overview');
  const [showCommandModal, setShowCommandModal] = useState(false);
  const [commandInput, setCommandInput] = useState('');
  const [showTerminal, setShowTerminal] = useState(false);
  const [showFileBrowser, setShowFileBrowser] = useState(false);
  const [showRemoteDesktop, setShowRemoteDesktop] = useState(false);
  const [showUninstallModal, setShowUninstallModal] = useState(false);

  const { data: device, isLoading } = useQuery<Device>({
    queryKey: ['device', id],
    queryFn: () => api.getDevice(id!),
    enabled: !!id,
  });

  const { data: metricsData } = useQuery<DeviceMetrics[]>({
    queryKey: ['device-metrics', id],
    queryFn: () => api.getDeviceMetrics(id!, { limit: 60 }),
    enabled: !!id,
    refetchInterval: 30000,
  });

  const { data: commandsData } = useQuery({
    queryKey: ['device-commands', id],
    queryFn: () => api.getDeviceCommands(id!, { pageSize: 10 }),
    enabled: !!id,
  });

  const { data: versionHistory } = useQuery({
    queryKey: ['device-version-history', id],
    queryFn: () => api.getDeviceVersionHistory(id!),
    enabled: !!id,
  });

  const executeCommandMutation = useMutation({
    mutationFn: (command: string) =>
      api.executeCommand(id!, command, 'shell'),
    onSuccess: () => {
      toast.success('Command sent to device');
      setShowCommandModal(false);
      setCommandInput('');
      queryClient.invalidateQueries({ queryKey: ['device-commands', id] });
    },
    onError: () => {
      toast.error('Failed to execute command');
    },
  });

  const uninstallAgentMutation = useMutation({
    mutationFn: () => api.uninstallAgent(id!),
    onSuccess: () => {
      toast.success('Uninstall command sent to agent');
      setShowUninstallModal(false);
      queryClient.invalidateQueries({ queryKey: ['device', id] });
    },
    onError: (error: any) => {
      toast.error(error.response?.data?.error || 'Failed to uninstall agent');
    },
  });


  const pingAgentMutation = useMutation({
    mutationFn: () => api.pingAgent(id!),
    onSuccess: (data) => {
      if (data.online) {
        toast.success(data.message || 'Agent is online and responsive');
      } else {
        toast.error(data.message || 'Agent is offline');
      }
      queryClient.invalidateQueries({ queryKey: ['device', id] });
    },
    onError: (error: any) => {
      toast.error(error.response?.data?.message || 'Failed to ping agent');
    },
  });

  const copyDeviceSpecs = () => {
    if (!device) return;
    const specs = [
      'Device name: ' + device.hostname,
      device.manufacturer ? 'Manufacturer: ' + device.manufacturer : null,
      device.model ? 'Model: ' + device.model : null,
      'Processor: ' + (device.cpuModel || 'N/A'),
      'Installed RAM: ' + (device.totalMemory ? formatBytes(device.totalMemory) : 'N/A'),
      'System type: ' + device.architecture,
      'Agent Version: v' + device.agentVersion,
    ].filter(Boolean).join("\n");
    navigator.clipboard.writeText(specs);
    toast.success('Device specifications copied');
  };

  const copyWindowsSpecs = () => {
    if (!device) return;
    const specs = [
      'Edition: ' + (device.platform || device.osType),
      'Version: ' + device.osVersion,
      device.osBuild ? 'OS build: ' + device.osBuild : null,
      'IP Address: ' + (device.ipAddress || 'N/A'),
      'MAC Address: ' + (device.macAddress || 'N/A'),
    ].filter(Boolean).join("\n");
    navigator.clipboard.writeText(specs);
    toast.success('Windows specifications copied');
  };

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-screen bg-[#1a1a1a]">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-500" />
      </div>
    );
  }

  if (!device) {
    return (
      <div className="p-6 bg-[#1a1a1a] min-h-screen">
        <Card className="bg-[#2d2d2d] border-none">
          <CardContent>
            <div className="text-center py-12">
              <p className="text-gray-400">Device not found</p>
              <Button className="mt-4" onClick={() => navigate('/devices')}>
                Back to Devices
              </Button>
            </div>
          </CardContent>
        </Card>
      </div>
    );
  }

  const metrics = metricsData || [];
  const commands: Command[] = commandsData?.commands || [];
  const latestMetrics = metrics[metrics.length - 1];

  const chartData = metrics.map((m) => ({
    time: new Date(m.timestamp).toLocaleTimeString(),
    cpu: m.cpuPercent,
    memory: m.memoryPercent,
    disk: m.diskPercent,
  }));

  const isOnline = device.status === 'online';

  const primaryDisk = device.storage?.[0];
  const storageUsedPercent = primaryDisk?.percent || 0;
  const storageTotal = primaryDisk ? formatBytes(primaryDisk.total) : 'N/A';
  const storageUsed = primaryDisk ? formatBytes(primaryDisk.used) : 'N/A';
  const primaryGPU = device.gpu?.[0];

  return (
    <div className="bg-[#1a1a1a] min-h-screen">
      <Header
        title={device.displayName || device.hostname}
        subtitle={device.osType + ' ' + device.osVersion + ' - ' + device.ipAddress}
      />

      <div className="p-6 space-y-6">
        <button
          onClick={() => navigate('/devices')}
          className="flex items-center gap-2 text-gray-400 hover:text-white transition-colors"
        >
          <ArrowLeft className="w-4 h-4" />
          Back to Devices
        </button>

        <div className="flex gap-1 bg-[#2d2d2d] p-1 rounded-lg w-fit">
          <button
            onClick={() => setActiveTab('overview')}
            className={'px-4 py-2 rounded-md text-sm font-medium transition-colors ' +
              (activeTab === 'overview'
                ? 'bg-[#3d3d3d] text-white'
                : 'text-gray-400 hover:text-white hover:bg-[#3d3d3d]/50')}
          >
            Overview
          </button>
          <button
            onClick={() => setActiveTab('metrics')}
            className={'px-4 py-2 rounded-md text-sm font-medium transition-colors ' +
              (activeTab === 'metrics'
                ? 'bg-[#3d3d3d] text-white'
                : 'text-gray-400 hover:text-white hover:bg-[#3d3d3d]/50')}
          >
            Metrics
          </button>
          <button
            onClick={() => setActiveTab('commands')}
            className={'px-4 py-2 rounded-md text-sm font-medium transition-colors ' +
              (activeTab === 'commands'
                ? 'bg-[#3d3d3d] text-white'
                : 'text-gray-400 hover:text-white hover:bg-[#3d3d3d]/50')}
          >
            Commands
          </button>
        </div>

        {activeTab === 'overview' && (
          <div className="space-y-6">
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
              <div className="bg-gradient-to-br from-blue-600 to-blue-800 rounded-xl p-4 text-white">
                <div className="flex items-center gap-3 mb-3">
                  <HardDrive className="w-6 h-6" />
                  <span className="font-medium">Storage</span>
                </div>
                <div className="text-2xl font-bold mb-2">{storageTotal}</div>
                <div className="w-full bg-blue-900/50 rounded-full h-2 mb-2">
                  <div
                    className="bg-white h-2 rounded-full transition-all"
                    style={{ width: storageUsedPercent + '%' }}
                  />
                </div>
                <div className="text-sm text-blue-200">
                  {storageUsed} used ({storageUsedPercent.toFixed(0)}%)
                </div>
              </div>

              <div className="bg-gradient-to-br from-purple-600 to-purple-800 rounded-xl p-4 text-white">
                <div className="flex items-center gap-3 mb-3">
                  <Monitor className="w-6 h-6" />
                  <span className="font-medium">Graphics Card</span>
                </div>
                <div className="text-lg font-bold truncate" title={primaryGPU?.name}>
                  {primaryGPU?.name || 'N/A'}
                </div>
                {primaryGPU?.memory && primaryGPU.memory > 0 && (
                  <div className="text-sm text-purple-200 mt-1">
                    {formatBytes(primaryGPU.memory)} VRAM
                  </div>
                )}
              </div>

              <div className="bg-gradient-to-br from-teal-600 to-teal-800 rounded-xl p-4 text-white">
                <div className="flex items-center gap-3 mb-3">
                  <MemoryStick className="w-6 h-6" />
                  <span className="font-medium">Installed RAM</span>
                </div>
                <div className="text-2xl font-bold">
                  {device.totalMemory ? formatRAMInGB(device.totalMemory) : '0'} GB
                </div>
                <div className="text-sm text-teal-200 mt-1">
                  {latestMetrics?.memoryPercent?.toFixed(0) || 0}% in use
                </div>
              </div>

              <div className="bg-gradient-to-br from-orange-600 to-orange-800 rounded-xl p-4 text-white">
                <div className="flex items-center gap-3 mb-3">
                  <Cpu className="w-6 h-6" />
                  <span className="font-medium">Processor</span>
                </div>
                <div className="text-sm font-bold truncate" title={device.cpuModel}>
                  {device.cpuModel || 'N/A'}
                </div>
                <div className="text-sm text-orange-200 mt-1">
                  {device.cpuCores} cores, {device.cpuThreads} threads
                </div>
              </div>
            </div>

            <div className="bg-[#2d2d2d] rounded-lg p-6">
              <div className="flex items-start justify-between">
                <div className="flex items-center gap-4">
                  <div className="w-16 h-16 bg-[#3d3d3d] rounded-lg flex items-center justify-center">
                    <Monitor className="w-10 h-10 text-gray-400" />
                  </div>
                  <div>
                    <h1 className="text-2xl font-semibold text-white">
                      {device.displayName || device.hostname}
                    </h1>
                    <p className="text-gray-400">
                      {device.manufacturer && device.model
                        ? device.manufacturer + ' ' + device.model
                        : device.model || device.manufacturer || 'Desktop PC'}
                    </p>
                    <div className="flex items-center gap-2 mt-2">
                      <Badge
                        variant={isOnline ? 'success' : 'default'}
                        size="sm"
                      >
                        {device.status}
                      </Badge>
                      <span className="text-gray-500 text-sm">•</span>
                      <span className="text-gray-400 text-sm">{device.ipAddress}</span>
                      {!isOnline && (
                        <Button
                          size="sm"
                          variant="secondary"
                          onClick={() => pingAgentMutation.mutate()}
                          disabled={pingAgentMutation.isPending}
                          className="ml-2 bg-blue-600 hover:bg-blue-700 text-white"
                        >
                          <RefreshCw className={'w-3 h-3 mr-1' + (pingAgentMutation.isPending ? ' animate-spin' : '')} />
                          {pingAgentMutation.isPending ? 'Checking...' : 'Check Connection'}
                        </Button>
                      )}

                    </div>
                  </div>
                </div>
                <button className="flex items-center gap-2 px-4 py-2 bg-[#3d3d3d] hover:bg-[#4d4d4d] text-white rounded-lg transition-colors">
                  <Edit3 className="w-4 h-4" />
                  Rename this PC
                </button>
              </div>
            </div>

            <div className="flex gap-2">
              <Button
                variant="secondary"
                onClick={() => setShowTerminal(true)}
                disabled={!isOnline}
                className="bg-[#2d2d2d] border-none hover:bg-[#3d3d3d]"
              >
                <TerminalIcon className="w-4 h-4 mr-2" />
                Terminal
              </Button>
              <Button
                variant="secondary"
                onClick={() => setShowFileBrowser(true)}
                disabled={!isOnline}
                className="bg-[#2d2d2d] border-none hover:bg-[#3d3d3d]"
              >
                <FolderOpen className="w-4 h-4 mr-2" />
                Files
              </Button>
              <Button
                variant="secondary"
                onClick={() => setShowRemoteDesktop(true)}
                disabled={!isOnline}
                className="bg-[#2d2d2d] border-none hover:bg-[#3d3d3d]"
              >
                <MonitorPlay className="w-4 h-4 mr-2" />
                Remote
              </Button>
              <Button
                variant="secondary"
                onClick={() => setShowCommandModal(true)}
                disabled={!isOnline}
                className="bg-[#2d2d2d] border-none hover:bg-[#3d3d3d]"
              >
                <Play className="w-4 h-4 mr-2" />
                Run Command
              </Button>
              <Button
                variant="secondary"
                onClick={() => setShowUninstallModal(true)}
                disabled={!isOnline}
                className="bg-red-900/50 border-none hover:bg-red-800/50 text-red-400 hover:text-red-300"
              >
                <Trash2 className="w-4 h-4 mr-2" />
                Uninstall Agent
              </Button>
            </div>

            {showTerminal && device.agentId && (
              <Terminal
                deviceId={id!}
                agentId={device.agentId}
                onClose={() => setShowTerminal(false)}
              />
            )}
            {showFileBrowser && device.agentId && (
              <FileBrowser
                deviceId={id!}
                agentId={device.agentId}
                onClose={() => setShowFileBrowser(false)}
              />
            )}
            {showRemoteDesktop && device.agentId && (
              <RemoteDesktop
                deviceId={id!}
                agentId={device.agentId}
                onClose={() => setShowRemoteDesktop(false)}
              />
            )}

            <CollapsibleSection title="Device specifications" onCopy={copyDeviceSpecs}>
              <div className="space-y-0">
                <SpecRow label="Device name" value={device.hostname} />
                <SpecRow label="Manufacturer" value={device.manufacturer} />
                <SpecRow label="Model" value={device.model} />
                <SpecRow label="Processor" value={device.cpuModel} />
                <SpecRow
                  label="Installed RAM"
                  value={device.totalMemory ? formatBytes(device.totalMemory) : undefined}
                />
                <SpecRow label="Device ID" value={device.id} />
                <SpecRow label="System type" value={device.architecture + ' operating system'} />
                <SpecRow label="Agent Version" value={'v' + device.agentVersion} />
              </div>
            </CollapsibleSection>

            <CollapsibleSection title="Windows specifications" onCopy={copyWindowsSpecs}>
              <div className="space-y-0">
                <SpecRow label="Edition" value={device.platform || device.osType} />
                <SpecRow label="Version" value={device.osVersion} />
                <SpecRow label="OS build" value={device.osBuild} />
                <SpecRow label="IP Address" value={device.ipAddress} />
                <SpecRow label="MAC Address" value={device.macAddress} />
                <SpecRow label="Domain" value={device.domain} />
                <SpecRow label="Last seen" value={new Date(device.lastSeen).toLocaleString()} />
              </div>
            </CollapsibleSection>

            {device.gpu && device.gpu.length > 0 && (
              <CollapsibleSection title="Display" defaultOpen={false}>
                <div className="space-y-4">
                  {device.gpu.map((gpu, index) => (
                    <div key={index} className="space-y-0">
                      {device.gpu && device.gpu.length > 1 && (
                        <p className="text-sm font-medium text-gray-400 mb-2">GPU {index + 1}</p>
                      )}
                      <SpecRow label="Name" value={gpu.name} />
                      <SpecRow label="Vendor" value={gpu.vendor} />
                      <SpecRow
                        label="Dedicated memory"
                        value={gpu.memory > 0 ? formatBytes(gpu.memory) : undefined}
                      />
                      <SpecRow label="Driver version" value={gpu.driver_version} />
                    </div>
                  ))}
                </div>
              </CollapsibleSection>
            )}

            {device.storage && device.storage.length > 0 && (
              <CollapsibleSection title="Storage" defaultOpen={false}>
                <div className="space-y-4">
                  {device.storage.map((disk, index) => (
                    <div key={index} className="bg-[#3d3d3d] rounded-lg p-4">
                      <div className="flex justify-between items-center mb-3">
                        <span className="font-medium text-white">
                          {disk.mountpoint} ({disk.device})
                        </span>
                        <span className="text-sm text-gray-400">{disk.fstype}</span>
                      </div>
                      <div className="w-full bg-[#4d4d4d] rounded-full h-2 mb-2">
                        <div
                          className="bg-blue-500 h-2 rounded-full"
                          style={{ width: disk.percent + '%' }}
                        />
                      </div>
                      <div className="flex justify-between text-sm text-gray-400">
                        <span>{formatBytes(disk.used)} of {formatBytes(disk.total)} used</span>
                        <span>{formatBytes(disk.free)} free ({disk.percent.toFixed(1)}%)</span>
                      </div>
                    </div>
                  ))}
                </div>
              </CollapsibleSection>
            )}

            <CollapsibleSection title="Version history" defaultOpen={false}>
              <div className="space-y-0">
                <SpecRow label="Current Version" value={'v' + device.agentVersion} />
                {device.previousAgentVersion && device.previousAgentVersion !== device.agentVersion && (
                  <SpecRow label="Previous Version" value={'v' + device.previousAgentVersion} />
                )}
                {device.lastUpdateCheck && (
                  <SpecRow
                    label="Last Update Check"
                    value={new Date(device.lastUpdateCheck).toLocaleString()}
                  />
                )}
              </div>
              {versionHistory && versionHistory.length > 0 && (
                <div className="mt-4 space-y-2">
                  {versionHistory.slice(0, 5).map((update: any) => (
                    <div
                      key={update.id}
                      className="flex items-center justify-between p-3 bg-[#3d3d3d] rounded-lg"
                    >
                      <div className="flex items-center gap-3">
                        <History className="w-4 h-4 text-gray-400" />
                        <div>
                          <p className="text-sm font-medium text-white">
                            {update.fromVersion
                              ? 'v' + update.fromVersion + ' → v' + update.toVersion
                              : 'v' + update.toVersion}
                          </p>
                          <p className="text-xs text-gray-400">
                            {new Date(update.createdAt).toLocaleString()}
                          </p>
                        </div>
                      </div>
                      <Badge
                        variant={
                          update.status === 'completed'
                            ? 'success'
                            : update.status === 'failed'
                            ? 'danger'
                            : update.status === 'downloading'
                            ? 'warning'
                            : 'default'
                        }
                      >
                        {update.status}
                      </Badge>
                    </div>
                  ))}
                </div>
              )}
            </CollapsibleSection>

            {device.tags && device.tags.length > 0 && (
              <div className="bg-[#2d2d2d] rounded-lg p-4">
                <div className="flex items-center gap-2 mb-3">
                  <Tag className="w-4 h-4 text-gray-400" />
                  <h3 className="font-medium text-white">Tags</h3>
                </div>
                <div className="flex flex-wrap gap-2">
                  {device.tags.map((tag) => (
                    <Badge key={tag} variant="info">
                      {tag}
                    </Badge>
                  ))}
                </div>
              </div>
            )}
          </div>
        )}

        {activeTab === 'metrics' && (
          <div className="space-y-6">
            <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
              <div className="bg-[#2d2d2d] rounded-lg p-4">
                <div className="flex items-center gap-3">
                  <div className="p-2 bg-green-500/20 text-green-500 rounded-lg">
                    <Cpu className="w-5 h-5" />
                  </div>
                  <div>
                    <p className="text-sm text-gray-400">CPU Usage</p>
                    <p className="text-xl font-semibold text-white">
                      {latestMetrics?.cpuPercent?.toFixed(1) || '0'}%
                    </p>
                  </div>
                </div>
              </div>

              <div className="bg-[#2d2d2d] rounded-lg p-4">
                <div className="flex items-center gap-3">
                  <div className="p-2 bg-purple-500/20 text-purple-500 rounded-lg">
                    <Activity className="w-5 h-5" />
                  </div>
                  <div>
                    <p className="text-sm text-gray-400">Memory Usage</p>
                    <p className="text-xl font-semibold text-white">
                      {latestMetrics?.memoryPercent?.toFixed(1) || '0'}%
                    </p>
                  </div>
                </div>
              </div>

              <div className="bg-[#2d2d2d] rounded-lg p-4">
                <div className="flex items-center gap-3">
                  <div className="p-2 bg-amber-500/20 text-amber-500 rounded-lg">
                    <HardDrive className="w-5 h-5" />
                  </div>
                  <div>
                    <p className="text-sm text-gray-400">Disk Usage</p>
                    <p className="text-xl font-semibold text-white">
                      {latestMetrics?.diskPercent?.toFixed(1) || '0'}%
                    </p>
                  </div>
                </div>
              </div>
            </div>

            <div className="bg-[#2d2d2d] rounded-lg p-6">
              <h3 className="text-lg font-semibold text-white mb-4">Resource Usage Over Time</h3>
              <div className="h-80">
                <ResponsiveContainer width="100%" height="100%">
                  <AreaChart data={chartData}>
                    <XAxis
                      dataKey="time"
                      tick={{ fontSize: 10, fill: '#9ca3af' }}
                      stroke="#4b5563"
                    />
                    <YAxis
                      domain={[0, 100]}
                      tick={{ fontSize: 10, fill: '#9ca3af' }}
                      stroke="#4b5563"
                    />
                    <Tooltip
                      contentStyle={{
                        backgroundColor: '#2d2d2d',
                        border: 'none',
                        borderRadius: '8px',
                        color: '#fff'
                      }}
                    />
                    <Area
                      type="monotone"
                      dataKey="cpu"
                      stroke="#22c55e"
                      fill="#22c55e"
                      fillOpacity={0.2}
                      name="CPU %"
                    />
                    <Area
                      type="monotone"
                      dataKey="memory"
                      stroke="#8b5cf6"
                      fill="#8b5cf6"
                      fillOpacity={0.2}
                      name="Memory %"
                    />
                  </AreaChart>
                </ResponsiveContainer>
              </div>
            </div>
          </div>
        )}

        {activeTab === 'commands' && (
          <div className="space-y-6">
            <div className="flex justify-between items-center">
              <h3 className="text-lg font-semibold text-white">Recent Commands</h3>
              <Button
                onClick={() => setShowCommandModal(true)}
                disabled={!isOnline}
              >
                <Play className="w-4 h-4 mr-2" />
                Run Command
              </Button>
            </div>

            <div className="bg-[#2d2d2d] rounded-lg overflow-hidden">
              {commands.length > 0 ? (
                <div className="divide-y divide-[#3d3d3d]">
                  {commands.map((cmd) => (
                    <div
                      key={cmd.id}
                      className="flex items-center justify-between p-4 hover:bg-[#3d3d3d] transition-colors"
                    >
                      <div className="flex items-center gap-3">
                        <Play className="w-4 h-4 text-gray-400" />
                        <div>
                          <p className="text-sm font-medium text-white">
                            {cmd.commandType}
                          </p>
                          <p className="text-xs text-gray-400">
                            {new Date(cmd.createdAt).toLocaleString()}
                          </p>
                        </div>
                      </div>
                      <Badge
                        variant={
                          cmd.status === 'completed'
                            ? 'success'
                            : cmd.status === 'failed'
                            ? 'danger'
                            : cmd.status === 'running'
                            ? 'warning'
                            : 'default'
                        }
                      >
                        {cmd.status}
                      </Badge>
                    </div>
                  ))}
                </div>
              ) : (
                <p className="text-sm text-gray-400 text-center py-12">
                  No commands executed yet
                </p>
              )}
            </div>
          </div>
        )}
      </div>

      <Modal
        isOpen={showCommandModal}
        onClose={() => {
          setShowCommandModal(false);
          setCommandInput('');
        }}
        title="Run Command"
        size="lg"
      >
        <div className="space-y-4">
          <p className="text-sm text-gray-400">
            Execute a shell command on{' '}
            <strong className="text-white">{device.displayName || device.hostname}</strong>
          </p>
          <textarea
            value={commandInput}
            onChange={(e) => setCommandInput(e.target.value)}
            placeholder="Enter command..."
            className="w-full h-32 px-3 py-2 bg-[#3d3d3d] border border-[#4d4d4d] rounded-lg font-mono text-sm text-white placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500 resize-none"
          />
          <div className="flex gap-3 justify-end">
            <Button
              variant="secondary"
              onClick={() => {
                setShowCommandModal(false);
                setCommandInput('');
              }}
            >
              Cancel
            </Button>
            <Button
              onClick={() => executeCommandMutation.mutate(commandInput)}
              disabled={!commandInput.trim() || executeCommandMutation.isPending}
              isLoading={executeCommandMutation.isPending}
            >
              Execute
            </Button>
          </div>
        </div>
</Modal>
      <Modal
        isOpen={showUninstallModal}
        onClose={() => setShowUninstallModal(false)}
        title="Uninstall Agent"
        size="md"
      >
        <div className="space-y-4">
          <div className="bg-red-900/20 border border-red-900/50 rounded-lg p-4">
            <p className="text-red-400 font-medium mb-2">Warning: This action cannot be undone</p>
            <p className="text-sm text-gray-400">
              This will permanently uninstall the Sentinel agent from{' '}
              <strong className="text-white">{device.displayName || device.hostname}</strong>.
              The agent service will be stopped and removed from the system.
            </p>
          </div>
          <p className="text-sm text-gray-400">
            After uninstallation, this device will no longer be monitored and you will need to
            manually reinstall the agent to reconnect it.
          </p>
          <div className="flex gap-3 justify-end">
            <Button
              variant="secondary"
              onClick={() => setShowUninstallModal(false)}
            >
              Cancel
            </Button>
            <Button
              variant="danger"
              onClick={() => uninstallAgentMutation.mutate()}
              disabled={uninstallAgentMutation.isPending}
              isLoading={uninstallAgentMutation.isPending}
              className="bg-red-600 hover:bg-red-700"
            >
              Uninstall Agent
            </Button>
          </div>
        </div>
      </Modal>
    </div>
  );
}
