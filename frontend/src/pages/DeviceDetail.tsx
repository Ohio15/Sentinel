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

export function DeviceDetail() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [showCommandModal, setShowCommandModal] = useState(false);
  const [commandInput, setCommandInput] = useState('');
  const [showTerminal, setShowTerminal] = useState(false);
  const [showFileBrowser, setShowFileBrowser] = useState(false);
  const [showRemoteDesktop, setShowRemoteDesktop] = useState(false);

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

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-screen">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary" />
      </div>
    );
  }

  if (!device) {
    return (
      <div className="p-6">
        <Card>
          <CardContent>
            <div className="text-center py-12">
              <p className="text-text-secondary">Device not found</p>
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
    disk: m.diskPercent,  }));

  const isOnline = device.status === 'online';

  return (
    <div>
      <Header
        title={device.displayName || device.hostname}
        subtitle={`${device.osType} ${device.osVersion} - ${device.ipAddress}`}
      />

      <div className="p-6 space-y-6">
        {/* Back button and actions */}
        <div className="flex items-center justify-between">
          <button
            onClick={() => navigate('/devices')}
            className="flex items-center gap-2 text-text-secondary hover:text-text-primary"
          >
            <ArrowLeft className="w-4 h-4" />
            Back to Devices
          </button>

          <div className="flex gap-2">
            <Button
              variant="secondary"
              onClick={() => setShowTerminal(true)}
              disabled={!isOnline}
              title={!isOnline ? 'Device is offline' : 'Open Terminal'}
            >
              <TerminalIcon className="w-4 h-4 mr-2" />
              Terminal
            </Button>
            <Button
              variant="secondary"
              onClick={() => setShowFileBrowser(true)}
              disabled={!isOnline}
              title={!isOnline ? 'Device is offline' : 'Browse Files'}
            >
              <FolderOpen className="w-4 h-4 mr-2" />
              Files
            </Button>
            <Button
              variant="secondary"
              onClick={() => setShowRemoteDesktop(true)}
              disabled={!isOnline}
              title={!isOnline ? 'Device is offline' : 'Remote Desktop'}
            >
              <MonitorPlay className="w-4 h-4 mr-2" />
              Remote
            </Button>
            <Button
              variant="secondary"
              onClick={() => setShowCommandModal(true)}
              disabled={!isOnline}
              title={!isOnline ? 'Device is offline' : 'Run Command'}
            >
              <Play className="w-4 h-4 mr-2" />
              Run Command
            </Button>
          </div>
        </div>

        {/* Terminal Panel */}
        {showTerminal && device.agentId && (
          <Terminal
            deviceId={id!}
            agentId={device.agentId}
            onClose={() => setShowTerminal(false)}
          />
        )}

        {/* File Browser Panel */}
        {showFileBrowser && device.agentId && (
          <FileBrowser
            deviceId={id!}
            agentId={device.agentId}
            onClose={() => setShowFileBrowser(false)}
          />
        )}

        {/* Remote Desktop Panel */}
        {showRemoteDesktop && device.agentId && (
          <RemoteDesktop
            deviceId={id!}
            agentId={device.agentId}
            onClose={() => setShowRemoteDesktop(false)}
          />
        )}

        {/* Device Info Cards */}
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
          <Card>
            <CardContent>
              <div className="flex items-center gap-3">
                <div className="p-2 bg-blue-100 text-blue-600 rounded-lg">
                  <Monitor className="w-5 h-5" />
                </div>
                <div>
                  <p className="text-sm text-text-secondary">Status</p>
                  <Badge
                    variant={
                      device.status === 'online'
                        ? 'success'
                        : device.status === 'warning'
                        ? 'warning'
                        : device.status === 'critical'
                        ? 'danger'
                        : 'default'
                    }
                    size="md"
                  >
                    {device.status}
                  </Badge>
                </div>
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardContent>
              <div className="flex items-center gap-3">
                <div className="p-2 bg-green-100 text-green-600 rounded-lg">
                  <Cpu className="w-5 h-5" />
                </div>
                <div>
                  <p className="text-sm text-text-secondary">CPU Usage</p>
                  <p className="text-xl font-semibold text-text-primary">
                    {latestMetrics?.cpuPercent?.toFixed(1) || '0'}%
                  </p>
                </div>
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardContent>
              <div className="flex items-center gap-3">
                <div className="p-2 bg-purple-100 text-purple-600 rounded-lg">
                  <Activity className="w-5 h-5" />
                </div>
                <div>
                  <p className="text-sm text-text-secondary">Memory Usage</p>
                  <p className="text-xl font-semibold text-text-primary">
                    {latestMetrics?.memoryPercent?.toFixed(1) || '0'}%
                  </p>
                </div>
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardContent>
              <div className="flex items-center gap-3">
                <div className="p-2 bg-amber-100 text-amber-600 rounded-lg">
                  <HardDrive className="w-5 h-5" />
                </div>
                <div>
                  <p className="text-sm text-text-secondary">Disk Usage</p>
                  <p className="text-xl font-semibold text-text-primary">
                    {latestMetrics?.diskPercent?.toFixed(1) || '0'}%
                  </p>
                </div>
              </div>
            </CardContent>
          </Card>
        </div>

        {/* Charts */}
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
          <Card>
            <CardContent>
              <h3 className="text-lg font-semibold text-text-primary mb-4">
                Resource Usage
              </h3>
              <div className="h-64">
                <ResponsiveContainer width="100%" height="100%">
                  <AreaChart data={chartData}>
                    <XAxis dataKey="time" tick={{ fontSize: 10 }} />
                    <YAxis domain={[0, 100]} tick={{ fontSize: 10 }} />
                    <Tooltip />
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
            </CardContent>
          </Card>

          <Card>
            <CardContent>
              <h3 className="text-lg font-semibold text-text-primary mb-4">
                Device Information
              </h3>
              <div className="space-y-3">
                <div className="flex justify-between py-2 border-b border-border">
                  <span className="text-text-secondary">Hostname</span>
                  <span className="text-text-primary font-medium">
                    {device.hostname}
                  </span>
                </div>
                <div className="flex justify-between py-2 border-b border-border">
                  <span className="text-text-secondary">Operating System</span>
                  <span className="text-text-primary font-medium capitalize">
                    {device.osType} {device.osVersion}
                  </span>
                </div>
                <div className="flex justify-between py-2 border-b border-border">
                  <span className="text-text-secondary">Architecture</span>
                  <span className="text-text-primary font-medium">
                    {device.architecture}
                  </span>
                </div>
                <div className="flex justify-between py-2 border-b border-border">
                  <span className="text-text-secondary">Agent Version</span>
                  <span className="text-text-primary font-medium">
                    v{device.agentVersion}
                  </span>
                </div>
                <div className="flex justify-between py-2 border-b border-border">
                  <span className="text-text-secondary">IP Address</span>
                  <span className="text-text-primary font-medium">
                    {device.ipAddress}
                  </span>
                </div>
                <div className="flex justify-between py-2 border-b border-border">
                  <span className="text-text-secondary">MAC Address</span>
                  <span className="text-text-primary font-medium">
                    {device.macAddress || 'N/A'}
                  </span>
                </div>
                <div className="flex justify-between py-2">
                  <span className="text-text-secondary">Last Seen</span>
                  <span className="text-text-primary font-medium">
                    {new Date(device.lastSeen).toLocaleString()}
                  </span>
                </div>
              </div>
            </CardContent>
          </Card>
        </div>

        {/* Recent Commands */}
        <Card>
          <CardContent>
            <h3 className="text-lg font-semibold text-text-primary mb-4">
              Recent Commands
            </h3>
            {commands.length > 0 ? (
              <div className="space-y-2">
                {commands.map((cmd) => (
                  <div
                    key={cmd.id}
                    className="flex items-center justify-between p-3 bg-gray-50 rounded-lg"
                  >
                    <div className="flex items-center gap-3">
                      <Play className="w-4 h-4 text-text-secondary" />
                      <div>
                        <p className="text-sm font-medium text-text-primary">
                          {cmd.commandType}
                        </p>
                        <p className="text-xs text-text-secondary">
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
              <p className="text-sm text-text-secondary text-center py-4">
                No commands executed yet
              </p>
            )}
          </CardContent>
        </Card>

        {/* Tags */}
        {device.tags && device.tags.length > 0 && (
          <Card>
            <CardContent>
              <div className="flex items-center gap-2 mb-3">
                <Tag className="w-4 h-4 text-text-secondary" />
                <h3 className="font-semibold text-text-primary">Tags</h3>
              </div>
              <div className="flex flex-wrap gap-2">
                {device.tags.map((tag) => (
                  <Badge key={tag} variant="info">
                    {tag}
                  </Badge>
                ))}
              </div>
            </CardContent>
          </Card>
        )}
      </div>

      {/* Command Modal */}
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
          <p className="text-sm text-text-secondary">
            Execute a shell command on{' '}
            <strong>{device.displayName || device.hostname}</strong>
          </p>
          <textarea
            value={commandInput}
            onChange={(e) => setCommandInput(e.target.value)}
            placeholder="Enter command..."
            className="w-full h-32 px-3 py-2 border border-border rounded-lg font-mono text-sm focus:outline-none focus:ring-2 focus:ring-primary resize-none"
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
    </div>
  );
}
