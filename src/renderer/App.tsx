import React, { useState, useEffect } from 'react';
import { Sidebar } from './components/layout/Sidebar';
import { Header } from './components/layout/Header';
import { Dashboard } from './pages/Dashboard';
import { Devices } from './pages/Devices';
import { DeviceDetail } from './pages/DeviceDetail';
import { Alerts } from './pages/Alerts';
import { Scripts } from './pages/Scripts';
import { Settings } from './pages/Settings';
import { Tickets } from './pages/Tickets';
import { TicketDetail } from './pages/TicketDetail';
import { Clients } from './pages/Clients';
import { useDeviceStore } from './stores/deviceStore';
import { useAlertStore } from './stores/alertStore';
import { useClientStore } from './stores/clientStore';
import { UpdateNotification } from './components/UpdateNotification';

type Page = 'dashboard' | 'devices' | 'device-detail' | 'alerts' | 'scripts' | 'settings' | 'tickets' | 'ticket-detail' | 'clients';

function App() {
  const [currentPage, setCurrentPage] = useState<Page>('dashboard');
  const [selectedDeviceId, setSelectedDeviceId] = useState<string | null>(null);
  const [selectedTicketId, setSelectedTicketId] = useState<string | null>(null);
  const { fetchDevices, subscribeToUpdates } = useDeviceStore();
  const { fetchAlerts, subscribeToAlerts } = useAlertStore();
  const { currentClientId, fetchClients } = useClientStore();

  // Initial setup - fetch clients and subscribe to updates
  useEffect(() => {
    fetchClients();

    // Subscribe to real-time updates
    const unsubDevices = subscribeToUpdates();
    const unsubAlerts = subscribeToAlerts();

    return () => {
      unsubDevices();
      unsubAlerts();
    };
  }, []);

  // Re-fetch data when client context changes
  useEffect(() => {
    fetchDevices(currentClientId);
    fetchAlerts();
  }, [currentClientId]);

  const handleNavigate = (page: Page) => {
    setCurrentPage(page);
    if (page !== 'device-detail') {
      setSelectedDeviceId(null);
    }
    if (page !== 'ticket-detail') {
      setSelectedTicketId(null);
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

  const handleTicketSelect = (ticketId: string) => {
    setSelectedTicketId(ticketId);
    setCurrentPage('ticket-detail');
  };

  const handleBackToTickets = () => {
    setSelectedTicketId(null);
    setCurrentPage('tickets');
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
      case 'tickets':
        return <Tickets onTicketSelect={handleTicketSelect} />;
      case 'ticket-detail':
        return selectedTicketId ? (
          <TicketDetail ticketId={selectedTicketId} onBack={handleBackToTickets} />
        ) : (
          <Tickets onTicketSelect={handleTicketSelect} />
        );
      case 'clients':
        return <Clients />;
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
