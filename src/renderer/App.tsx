import React, { lazy, Suspense, useState, useEffect } from 'react';
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { Sidebar } from './components/layout/Sidebar';
import { Header } from './components/layout/Header';
import { Login } from './pages/Login';
import { Register } from './pages/Register';
import { SmartAppBanner } from './components/SmartAppBanner';

// Lazy-loaded pages (code splitting)
const Dashboard = lazy(() => import('./pages/Dashboard'));
const Devices = lazy(() => import('./pages/Devices'));
const DeviceDetail = lazy(() => import('./pages/DeviceDetail'));
const Alerts = lazy(() => import('./pages/Alerts'));
const Scripts = lazy(() => import('./pages/Scripts'));
const Settings = lazy(() => import('./pages/Settings'));
const Tickets = lazy(() => import('./pages/Tickets'));
const TicketDetail = lazy(() => import('./pages/TicketDetail'));
const TicketsKanban = lazy(() => import('./pages/TicketsKanban'));
const TicketsCalendar = lazy(() => import('./pages/TicketsCalendar'));
const TicketAnalytics = lazy(() => import('./pages/TicketAnalytics'));
const KnowledgeBase = lazy(() => import('./pages/KnowledgeBase'));
const Clients = lazy(() => import('./pages/Clients'));
const Certificates = lazy(() => import('./pages/Certificates'));
const Network = lazy(() => import('./pages/Network'));
const InstallationPortal = lazy(() => import('./pages/InstallationPortal'));
const PopOutPerformance = lazy(() => import('./pages/PopOutPerformance'));
const SupportPortal = lazy(() => import('./pages/SupportPortal'));
const AgentInstallations = lazy(() => import('./pages/AgentInstallations'));
import { useDeviceStore } from './stores/deviceStore';
import { useAlertStore } from './stores/alertStore';
import { useClientStore } from './stores/clientStore';
import { useAuthStore } from './stores/authStore';
import { ErrorBoundary } from './components/ErrorBoundary';
import { Loader2 } from 'lucide-react';

type Page = 'dashboard' | 'devices' | 'device-detail' | 'alerts' | 'scripts' | 'certificates' | 'settings' | 'tickets' | 'ticket-detail' | 'tickets-kanban' | 'tickets-calendar' | 'tickets-analytics' | 'knowledge-base' | 'clients' | 'network';

function RequireAuth({ children }: { children: React.ReactNode }) {
  const { isAuthenticated, isLoading, checkAuth } = useAuthStore();

  useEffect(() => {
    // checkAuth has internal flag to prevent duplicate calls
    void checkAuth();
  }, []); // Empty deps - checkAuth guards against duplicate calls internally

  if (isLoading) return (<div className="min-h-screen bg-background flex items-center justify-center"><Loader2 className="w-8 h-8 animate-spin text-primary" /></div>);
  if (!isAuthenticated) return <Navigate to="/login" replace />;
  return <>{children}</>;
}

function MainLayout() {
  const [currentPage, setCurrentPage] = useState<Page>('dashboard');
  const [selectedDeviceId, setSelectedDeviceId] = useState<string | null>(null);
  const [selectedTicketId, setSelectedTicketId] = useState<string | null>(null);
  const { fetchDevices, subscribeToUpdates } = useDeviceStore();
  const { fetchAlerts, subscribeToAlerts } = useAlertStore();
  const { currentClientId, fetchClients } = useClientStore();

  useEffect(() => {
    void fetchClients();
    const unsubDevices = subscribeToUpdates();
    const unsubAlerts = subscribeToAlerts();
    return () => { unsubDevices(); unsubAlerts(); };
  }, []);

  useEffect(() => { void fetchDevices(currentClientId); void fetchAlerts(); }, [currentClientId]);

  const handleNavigate = (page: Page) => {
    setCurrentPage(page);
    if (page !== 'device-detail') setSelectedDeviceId(null);
    if (page !== 'ticket-detail') setSelectedTicketId(null);
  };

  const handleDeviceSelect = (deviceId: string) => { setSelectedDeviceId(deviceId); setCurrentPage('device-detail'); };
  const handleBackToDevices = () => { setSelectedDeviceId(null); setCurrentPage('devices'); };
  const handleTicketSelect = (ticketId: string) => { setSelectedTicketId(ticketId); setCurrentPage('ticket-detail'); };
  const handleBackToTickets = () => { setSelectedTicketId(null); setCurrentPage('tickets'); };
  const handleTicketViewChange = (view: 'table' | 'kanban' | 'calendar' | 'analytics') => {
    const viewToPage: Record<string, Page> = { 'table': 'tickets', 'kanban': 'tickets-kanban', 'calendar': 'tickets-calendar', 'analytics': 'tickets-analytics' };
    setCurrentPage(viewToPage[view] || 'tickets');
  };

  const renderPage = () => {
    switch (currentPage) {
      case 'dashboard': return <ErrorBoundary key="dashboard"><Dashboard onDeviceSelect={handleDeviceSelect} /></ErrorBoundary>;
      case 'devices': return <ErrorBoundary key="devices"><Devices onDeviceSelect={handleDeviceSelect} /></ErrorBoundary>;
      case 'device-detail': return selectedDeviceId ? <ErrorBoundary key={`device-${selectedDeviceId}`}><DeviceDetail deviceId={selectedDeviceId} onBack={handleBackToDevices} /></ErrorBoundary> : <ErrorBoundary key="devices-fallback"><Devices onDeviceSelect={handleDeviceSelect} /></ErrorBoundary>;
      case 'alerts': return <ErrorBoundary key="alerts"><Alerts /></ErrorBoundary>;
      case 'scripts': return <ErrorBoundary key="scripts"><Scripts /></ErrorBoundary>;
      case 'settings': return <ErrorBoundary key="settings"><Settings /></ErrorBoundary>;
      case 'tickets': return <ErrorBoundary key="tickets"><Tickets onTicketSelect={handleTicketSelect} onViewChange={handleTicketViewChange} /></ErrorBoundary>;
      case 'ticket-detail': return selectedTicketId ? <ErrorBoundary key={`ticket-${selectedTicketId}`}><TicketDetail ticketId={selectedTicketId} onBack={handleBackToTickets} /></ErrorBoundary> : <ErrorBoundary key="tickets-fallback"><Tickets onTicketSelect={handleTicketSelect} onViewChange={handleTicketViewChange} /></ErrorBoundary>;
      case 'clients': return <ErrorBoundary key="clients"><Clients /></ErrorBoundary>;
      case 'certificates': return <ErrorBoundary key="certificates"><Certificates /></ErrorBoundary>;
      case 'tickets-kanban': return <ErrorBoundary key="tickets-kanban"><TicketsKanban onTicketSelect={handleTicketSelect} onViewChange={handleTicketViewChange} /></ErrorBoundary>;
      case 'tickets-calendar': return <ErrorBoundary key="tickets-calendar"><TicketsCalendar onTicketSelect={handleTicketSelect} onViewChange={handleTicketViewChange} /></ErrorBoundary>;
      case 'tickets-analytics': return <ErrorBoundary key="tickets-analytics"><TicketAnalytics onViewChange={handleTicketViewChange} /></ErrorBoundary>;
      case 'knowledge-base': return <ErrorBoundary key="knowledge-base"><KnowledgeBase /></ErrorBoundary>;
      case 'network': return <ErrorBoundary key="network"><Network /></ErrorBoundary>;
      default: return <ErrorBoundary key="dashboard-default"><Dashboard onDeviceSelect={handleDeviceSelect} /></ErrorBoundary>;
    }
  };

  return (
    <div className="flex h-screen bg-background">
      <ErrorBoundary><Sidebar currentPage={currentPage} onNavigate={handleNavigate} /></ErrorBoundary>
      <div className="flex-1 flex flex-col overflow-hidden">
        <ErrorBoundary><Header /></ErrorBoundary>
        <main className="flex-1 overflow-auto p-6">
          <Suspense fallback={<div className="flex items-center justify-center h-full"><div className="animate-spin h-8 w-8 border-2 border-primary border-t-transparent rounded-full" /></div>}>
            {renderPage()}
          </Suspense>
        </main>
      </div>
    </div>
  );
}

function App() {
  return (
    <>
      <SmartAppBanner
        androidPackage="com.sentinel.rmm"
        expoProjectUrl="https://expo.dev/@sentinel/sentinel-mobile"
      />
      <BrowserRouter>
        <Suspense fallback={<div className="flex items-center justify-center h-screen bg-background"><div className="animate-spin h-8 w-8 border-2 border-primary border-t-transparent rounded-full" /></div>}>
          <Routes>
            <Route path="/login" element={<Login />} />
            <Route path="/register" element={<Register />} />
            <Route path="/install/:downloadToken" element={<InstallationPortal />} />
            <Route path="/popout/performance/:deviceId" element={<RequireAuth><PopOutPerformance /></RequireAuth>} />
            <Route path="/portal" element={<SupportPortal />} />
            <Route path="/*" element={<RequireAuth><MainLayout /></RequireAuth>} />
          </Routes>
        </Suspense>
      </BrowserRouter>
    </>
  );
}

export default App;
