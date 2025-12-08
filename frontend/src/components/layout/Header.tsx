import { Bell, Search, Wifi, WifiOff } from 'lucide-react';
import { useState, useEffect } from 'react';
import wsService from '@/services/websocket';

interface HeaderProps {
  title: string;
  subtitle?: string;
}

export function Header({ title, subtitle }: HeaderProps) {
  const [isConnected, setIsConnected] = useState(wsService.isConnected);
  const [searchQuery, setSearchQuery] = useState('');

  useEffect(() => {
    const unsubConnected = wsService.on('connected', () => setIsConnected(true));
    const unsubDisconnected = wsService.on('disconnected', () => setIsConnected(false));

    return () => {
      unsubConnected();
      unsubDisconnected();
    };
  }, []);

  return (
    <header className="bg-surface border-b border-border px-6 py-4">
      <div className="flex items-center justify-between">
        {/* Title */}
        <div>
          <h1 className="text-2xl font-semibold text-text-primary">{title}</h1>
          {subtitle && <p className="text-sm text-text-secondary mt-0.5">{subtitle}</p>}
        </div>

        {/* Actions */}
        <div className="flex items-center gap-4">
          {/* Search */}
          <div className="relative">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-text-secondary" />
            <input
              type="text"
              placeholder="Search..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="pl-9 pr-4 py-2 w-64 bg-background border border-border rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-primary focus:border-transparent"
            />
          </div>

          {/* Connection status */}
          <div
            className={`flex items-center gap-2 px-3 py-1.5 rounded-full text-xs font-medium ${
              isConnected
                ? 'bg-green-100 text-green-700'
                : 'bg-red-100 text-red-700'
            }`}
          >
            {isConnected ? (
              <>
                <Wifi className="w-3.5 h-3.5" />
                <span>Connected</span>
              </>
            ) : (
              <>
                <WifiOff className="w-3.5 h-3.5" />
                <span>Disconnected</span>
              </>
            )}
          </div>

          {/* Notifications */}
          <button className="relative p-2 text-text-secondary hover:text-text-primary hover:bg-background rounded-lg transition-colors">
            <Bell className="w-5 h-5" />
            <span className="absolute top-1 right-1 w-2 h-2 bg-danger rounded-full" />
          </button>
        </div>
      </div>
    </header>
  );
}
