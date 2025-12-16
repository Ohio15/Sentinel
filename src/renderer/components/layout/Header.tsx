import React, { useState, useEffect } from 'react';
import { useAlertStore } from '../../stores/alertStore';
import { ClientSelector } from '../ClientSelector';

export function Header() {
  const [serverInfo, setServerInfo] = useState<{ port: number; agentCount: number } | null>(null);
  const { alerts } = useAlertStore();
  const openAlerts = alerts.filter(a => a.status === 'open').length;

  useEffect(() => {
    loadServerInfo();
    const interval = setInterval(loadServerInfo, 30000);
    return () => clearInterval(interval);
  }, []);

  const loadServerInfo = async () => {
    try {
      const info = await window.api.server.getInfo();
      setServerInfo(info);
    } catch (error) {
      console.error('Failed to load server info:', error);
    }
  };

  return (
    <header className="h-16 bg-surface border-b border-border flex items-center justify-between px-6">
      <div className="flex items-center gap-4">
        {/* Client selector */}
        <ClientSelector />
        <div className="h-4 w-px bg-border" />
        <div className="flex items-center gap-2">
          <div className="w-2 h-2 bg-success rounded-full animate-pulse" />
          <span className="text-sm text-text-secondary">
            Port {serverInfo?.port || '...'}
          </span>
        </div>
        <div className="h-4 w-px bg-border" />
        <span className="text-sm text-text-secondary">
          {serverInfo?.agentCount || 0} agent{serverInfo?.agentCount !== 1 ? 's' : ''} connected
        </span>
      </div>

      <div className="flex items-center gap-4">
        {/* Alert indicator */}
        <button className="relative p-2 text-text-secondary hover:text-text-primary transition-colors">
          <BellIcon className="w-5 h-5" />
          {openAlerts > 0 && (
            <span className="absolute top-0 right-0 w-4 h-4 bg-danger text-white text-xs rounded-full flex items-center justify-center">
              {openAlerts > 9 ? '9+' : openAlerts}
            </span>
          )}
        </button>

        {/* Time */}
        <div className="text-sm text-text-secondary">
          <Clock />
        </div>
      </div>
    </header>
  );
}

function Clock() {
  const [time, setTime] = useState(new Date());

  useEffect(() => {
    const timer = setInterval(() => setTime(new Date()), 1000);
    return () => clearInterval(timer);
  }, []);

  return (
    <span>
      {time.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
    </span>
  );
}

function BellIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 17h5l-1.405-1.405A2.032 2.032 0 0118 14.158V11a6.002 6.002 0 00-4-5.659V5a2 2 0 10-4 0v.341C7.67 6.165 6 8.388 6 11v3.159c0 .538-.214 1.055-.595 1.436L4 17h5m6 0v1a3 3 0 11-6 0v-1m6 0H9" />
    </svg>
  );
}
