import React, { useEffect, useState, useCallback } from 'react';
import { useParams } from 'react-router-dom';
import { PerformanceView } from '../components/PerformanceView';
import { useDeviceStore } from '../stores/deviceStore';
import { shallow } from 'zustand/shallow';

interface PopOutPerformanceProps {
  windowId?: string;
}

export function PopOutPerformance({ windowId }: PopOutPerformanceProps) {
  const { deviceId } = useParams<{ deviceId: string }>();
  const [isReattaching, setIsReattaching] = useState(false);

  // Get device info from store
  const { selectedDevice, loading } = useDeviceStore(
    (state) => ({
      selectedDevice: state.selectedDevice,
      loading: state.loading,
    }),
    shallow
  );
  const fetchDevice = useDeviceStore((state) => state.fetchDevice);

  // Fetch device info on mount
  useEffect(() => {
    if (deviceId) {
      void fetchDevice(deviceId);
    }
  }, [deviceId, fetchDevice]);

  // Handle re-attach button click
  const handleReattach = useCallback(async () => {
    setIsReattaching(true);

    try {
      // Notify opener and close window
      if (window.opener) {
        // Send message to opener window
        window.opener.postMessage(
          { type: 'popout:reattach', deviceId, tab: 'performance' },
          window.location.origin
        );
      }
      // Store reattach request in localStorage for cross-tab communication
      localStorage.setItem('popout:reattach', JSON.stringify({
        deviceId,
        tab: 'performance',
        timestamp: Date.now(),
      }));
      // Close this window
      window.close();
    } catch (error) {
      console.error('Failed to reattach:', error);
      setIsReattaching(false);
    }
  }, [deviceId]);

  // Handle window close
  const handleClose = useCallback(() => {
    window.close();
  }, []);

  if (!deviceId) {
    return (
      <div className="flex items-center justify-center h-screen bg-background">
        <p className="text-text-secondary">No device specified</p>
      </div>
    );
  }

  if (loading && !selectedDevice) {
    return (
      <div className="flex items-center justify-center h-screen bg-background">
        <div className="flex items-center gap-3">
          <div className="w-6 h-6 border-2 border-primary border-t-transparent rounded-full animate-spin" />
          <p className="text-text-secondary">Loading device...</p>
        </div>
      </div>
    );
  }

  return (
    <div className="h-screen flex flex-col bg-background overflow-hidden">
      {/* Compact Header */}
      <div className="flex items-center justify-between px-4 py-2 bg-surface border-b border-border shrink-0">
        <div className="flex items-center gap-3">
          {/* Device status indicator */}
          <div className={`w-2.5 h-2.5 rounded-full ${
            selectedDevice?.status === 'online' ? 'bg-success animate-pulse' :
            selectedDevice?.status === 'warning' ? 'bg-warning' :
            selectedDevice?.status === 'critical' ? 'bg-error' : 'bg-text-secondary'
          }`} />

          {/* Device name and info */}
          <div>
            <h1 className="text-sm font-semibold text-text-primary">
              Performance - {selectedDevice?.displayName || selectedDevice?.hostname || deviceId.slice(0, 8)}
            </h1>
            <p className="text-xs text-text-secondary">
              {selectedDevice?.osType} {selectedDevice?.osVersion}
            </p>
          </div>
        </div>

        <div className="flex items-center gap-2">
          {/* Re-attach button */}
          <button
            onClick={() => { void handleReattach(); }}
            disabled={isReattaching}
            className="flex items-center gap-2 px-3 py-1.5 text-sm bg-primary/10 text-primary hover:bg-primary/20 rounded transition-colors disabled:opacity-50"
            title="Re-attach to main window"
          >
            {isReattaching ? (
              <>
                <div className="w-4 h-4 border-2 border-primary border-t-transparent rounded-full animate-spin" />
                <span>Re-attaching...</span>
              </>
            ) : (
              <>
                <ReattachIcon className="w-4 h-4" />
                <span>Re-attach</span>
              </>
            )}
          </button>

          {/* Close button */}
          <button
            onClick={handleClose}
            className="p-1.5 hover:bg-hover rounded transition-colors"
            title="Close window"
          >
            <CloseIcon className="w-4 h-4 text-text-secondary hover:text-text-primary" />
          </button>
        </div>
      </div>

      {/* Performance View */}
      <div className="flex-1 overflow-auto">
        <PerformanceView
          deviceId={deviceId}
          systemInfo={selectedDevice ? {
            cpuModel: selectedDevice.cpuModel,
            cpuCores: selectedDevice.cpuCores,
            cpuThreads: selectedDevice.cpuThreads,
            cpuSpeed: selectedDevice.cpuSpeed,
            totalMemory: selectedDevice.totalMemory,
            gpu: selectedDevice.gpu,
            storage: selectedDevice.storage,
            bootTime: selectedDevice.bootTime,
          } : undefined}
        />
      </div>
    </div>
  );
}

// Icons
function ReattachIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M11 19l-7-7 7-7m8 14l-7-7 7-7" />
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

export default PopOutPerformance;
