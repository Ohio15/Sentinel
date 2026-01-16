import { useState, useCallback, memo } from 'react';
import { Terminal as TerminalIcon, FolderOpen, MonitorPlay, Play, Trash2 } from 'lucide-react';
import { Terminal } from './Terminal';
import { FileBrowser } from './FileBrowser';
import { RemoteDesktop } from './RemoteDesktop';
import { Button, Modal } from '@/components/ui';
import api from '@/services/api';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import toast from 'react-hot-toast';

interface DeviceActionToolsProps {
  deviceId: string;
  agentId: string;
  hostname: string;
  displayName?: string;
  isOnline: boolean;
}

// This component is completely isolated from metrics updates
// It manages its own state and doesn't re-render when parent metrics change
export const DeviceActionTools = memo(function DeviceActionTools({
  deviceId,
  agentId,
  hostname,
  displayName,
  isOnline,
}: DeviceActionToolsProps) {
  const [showTerminal, setShowTerminal] = useState(false);
  const [showFileBrowser, setShowFileBrowser] = useState(false);
  const [showRemoteDesktop, setShowRemoteDesktop] = useState(false);
  const [showCommandModal, setShowCommandModal] = useState(false);
  const [showUninstallModal, setShowUninstallModal] = useState(false);
  const [commandInput, setCommandInput] = useState('');
  const queryClient = useQueryClient();

  // Stable callbacks
  const handleCloseTerminal = useCallback(() => setShowTerminal(false), []);
  const handleCloseFileBrowser = useCallback(() => setShowFileBrowser(false), []);
  const handleCloseRemoteDesktop = useCallback(() => setShowRemoteDesktop(false), []);

  const executeCommandMutation = useMutation({
    mutationFn: (command: string) => api.executeCommand(deviceId, command, 'shell'),
    onSuccess: () => {
      toast.success('Command sent to device');
      setShowCommandModal(false);
      setCommandInput('');
      queryClient.invalidateQueries({ queryKey: ['device-commands', deviceId] });
    },
    onError: () => {
      toast.error('Failed to execute command');
    },
  });

  const uninstallAgentMutation = useMutation({
    mutationFn: () => api.uninstallAgent(deviceId),
    onSuccess: () => {
      toast.success('Uninstall command sent to agent');
      setShowUninstallModal(false);
      queryClient.invalidateQueries({ queryKey: ['device', deviceId] });
    },
    onError: (error: any) => {
      toast.error(error.response?.data?.error || 'Failed to uninstall agent');
    },
  });

  const deviceName = displayName || hostname;

  return (
    <>
      {/* Action Buttons */}
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

      {/* Terminal Panel */}
      {showTerminal && (
        <Terminal
          deviceId={deviceId}
          agentId={agentId}
          onClose={handleCloseTerminal}
        />
      )}

      {/* File Browser Panel */}
      {showFileBrowser && (
        <FileBrowser
          deviceId={deviceId}
          agentId={agentId}
          onClose={handleCloseFileBrowser}
        />
      )}

      {/* Remote Desktop Panel */}
      {showRemoteDesktop && (
        <RemoteDesktop
          deviceId={deviceId}
          agentId={agentId}
          onClose={handleCloseRemoteDesktop}
        />
      )}

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
          <p className="text-sm text-gray-400">
            Execute a shell command on{' '}
            <strong className="text-white">{deviceName}</strong>
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

      {/* Uninstall Modal */}
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
              <strong className="text-white">{deviceName}</strong>.
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
    </>
  );
});
