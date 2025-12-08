import React, { useState, useEffect } from 'react';
import { Sidebar } from './components/layout/Sidebar';
import { Header } from './components/layout/Header';
import { Dashboard } from './pages/Dashboard';
import { Devices } from './pages/Devices';
import { DeviceDetail } from './pages/DeviceDetail';
import { Alerts } from './pages/Alerts';
import { Scripts } from './pages/Scripts';
import { Settings } from './pages/Settings';
import { useDeviceStore } from './stores/deviceStore';
import { useAlertStore } from './stores/alertStore';
import { UpdateNotification } from './components/UpdateNotification';

type Page = 'dashboard' | 'devices' | 'device-detail' | 'alerts' | 'scripts' | 'settings';

function App() {
  const [currentPage, setCurrentPage] = useState<Page>('dashboard');
  const [selectedDeviceId, setSelectedDeviceId] = useState<string | null>(null);
  const { fetchDevices, subscribeToUpdates } = useDeviceStore();
  const { fetchAlerts, subscribeToAlerts } = useAlertStore();

  useEffect(() => {
    // Initial data fetch
    fetchDevices();
    fetchAlerts();

    // Subscribe to real-time updates
    const unsubDevices = subscribeToUpdates();
    const unsubAlerts = subscribeToAlerts();

    return () => {
      unsubDevices();
      unsubAlerts();
    };
  }, []);

  const handleNavigate = (page: Page) => {
    setCurrentPage(page);
    if (page !== 'device-detail') {
      setSelectedDeviceId(null);
    }
  };

  const handleDeviceSelect = (deviceId: string) => {
    setSelectedDeviceId(deviceId);
    setCurrentPage('device-detail');
  };

  const handleBackToDevices = () => {
    setSelectedDeviceId(null);
    setCurrentPage('devices');
  };

  const renderPage = () => {
    switch (currentPage) {
      case 'dashboard':
        return <Dashboard onDeviceSelect={handleDeviceSelect} />;
      case 'devices':
        return <Devices onDeviceSelect={handleDeviceSelect} />;
      case 'device-detail':
        return selectedDeviceId ? (
          <DeviceDetail deviceId={selectedDeviceId} onBack={handleBackToDevices} />
        ) : (
          <Devices onDeviceSelect={handleDeviceSelect} />
        );
      case 'alerts':
        return <Alerts />;
      case 'scripts':
        return <Scripts />;
      case 'settings':
        return <Settings />;
      default:
        return <Dashboard onDeviceSelect={handleDeviceSelect} />;
    }
  };

  return (
    <div className="flex h-screen bg-background">
      <Sidebar currentPage={currentPage} onNavigate={handleNavigate} />
      <div className="flex-1 flex flex-col overflow-hidden">
        <Header />
        <main className="flex-1 overflow-auto p-6">
          {renderPage()}
        </main>
      </div>
      <UpdateNotification />
    </div>
  );
}

export default App;
