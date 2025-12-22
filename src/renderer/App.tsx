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
import { TicketsKanban } from './pages/TicketsKanban';
import { TicketsCalendar } from './pages/TicketsCalendar';
import { TicketAnalytics } from './pages/TicketAnalytics';
import { KnowledgeBase } from './pages/KnowledgeBase';
import { Clients } from './pages/Clients';
import { Certificates } from './pages/Certificates';
import { useDeviceStore } from './stores/deviceStore';
import { useAlertStore } from './stores/alertStore';
import { useClientStore } from './stores/clientStore';
import { UpdateNotification } from './components/UpdateNotification';
import { ErrorBoundary } from './components/ErrorBoundary';

type Page = 'dashboard' | 'devices' | 'device-detail' | 'alerts' | 'scripts' | 'certificates' | 'settings' | 'tickets' | 'ticket-detail' | 'tickets-kanban' | 'tickets-calendar' | 'tickets-analytics' | 'knowledge-base' | 'clients';

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

  // Handle ticket view switching (table, kanban, calendar, analytics)
  const handleTicketViewChange = (view: 'table' | 'kanban' | 'calendar' | 'analytics') => {
    const viewToPage: Record<string, Page> = {
      'table': 'tickets',
      'kanban': 'tickets-kanban',
      'calendar': 'tickets-calendar',
      'analytics': 'tickets-analytics'
    };
    setCurrentPage(viewToPage[view] || 'tickets');
  };

  const renderPage = () => {
    switch (currentPage) {
      case 'dashboard':
        return (
          <ErrorBoundary key="dashboard">
            <Dashboard onDeviceSelect={handleDeviceSelect} />
          </ErrorBoundary>
        );
      case 'devices':
        return (
          <ErrorBoundary key="devices">
            <Devices onDeviceSelect={handleDeviceSelect} />
          </ErrorBoundary>
        );
      case 'device-detail':
        return selectedDeviceId ? (
          <ErrorBoundary key={`device-${selectedDeviceId}`}>
            <DeviceDetail deviceId={selectedDeviceId} onBack={handleBackToDevices} />
          </ErrorBoundary>
        ) : (
          <ErrorBoundary key="devices-fallback">
            <Devices onDeviceSelect={handleDeviceSelect} />
          </ErrorBoundary>
        );
      case 'alerts':
        return (
          <ErrorBoundary key="alerts">
            <Alerts />
          </ErrorBoundary>
        );
      case 'scripts':
        return (
          <ErrorBoundary key="scripts">
            <Scripts />
          </ErrorBoundary>
        );
      case 'settings':
        return (
          <ErrorBoundary key="settings">
            <Settings />
          </ErrorBoundary>
        );
      case 'tickets':
        return (
          <ErrorBoundary key="tickets">
            <Tickets onTicketSelect={handleTicketSelect} onViewChange={handleTicketViewChange} />
          </ErrorBoundary>
        );
      case 'ticket-detail':
        return selectedTicketId ? (
          <ErrorBoundary key={`ticket-${selectedTicketId}`}>
            <TicketDetail ticketId={selectedTicketId} onBack={handleBackToTickets} />
          </ErrorBoundary>
        ) : (
          <ErrorBoundary key="tickets-fallback">
            <Tickets onTicketSelect={handleTicketSelect} onViewChange={handleTicketViewChange} />
          </ErrorBoundary>
        );
      case 'clients':
        return (
          <ErrorBoundary key="clients">
            <Clients />
          </ErrorBoundary>
        );
      case 'certificates':
        return (
          <ErrorBoundary key="certificates">
            <Certificates />
          </ErrorBoundary>
        );
      case 'tickets-kanban':
        return (
          <ErrorBoundary key="tickets-kanban">
            <TicketsKanban onTicketSelect={handleTicketSelect} onViewChange={handleTicketViewChange} />
          </ErrorBoundary>
        );
      case 'tickets-calendar':
        return (
          <ErrorBoundary key="tickets-calendar">
            <TicketsCalendar onTicketSelect={handleTicketSelect} onViewChange={handleTicketViewChange} />
          </ErrorBoundary>
        );
      case 'tickets-analytics':
        return (
          <ErrorBoundary key="tickets-analytics">
            <TicketAnalytics onViewChange={handleTicketViewChange} />
          </ErrorBoundary>
        );
      case 'knowledge-base':
        return (
          <ErrorBoundary key="knowledge-base">
            <KnowledgeBase />
          </ErrorBoundary>
        );
      default:
        return (
          <ErrorBoundary key="dashboard-default">
            <Dashboard onDeviceSelect={handleDeviceSelect} />
          </ErrorBoundary>
        );
    }
  };

  return (
    <div className="flex h-screen bg-background">
      <ErrorBoundary>
        <Sidebar currentPage={currentPage} onNavigate={handleNavigate} />
      </ErrorBoundary>
      <div className="flex-1 flex flex-col overflow-hidden">
        <ErrorBoundary>
          <Header />
        </ErrorBoundary>
        <main className="flex-1 overflow-auto p-6">
          {renderPage()}
        </main>
      </div>
      <UpdateNotification />
    </div>
  );
}

export default App;
