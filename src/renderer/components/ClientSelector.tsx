import React, { useEffect, useState, useRef } from 'react';
import { useClientStore, Client } from '../stores/clientStore';

export function ClientSelector() {
  const { clients, currentClientId, loading, fetchClients, setCurrentClient, getCurrentClient } = useClientStore();
  const [isOpen, setIsOpen] = useState(false);
  const dropdownRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    fetchClients();
  }, [fetchClients]);

  useEffect(() => {
    function handleClickOutside(event: MouseEvent) {
      if (dropdownRef.current && !dropdownRef.current.contains(event.target as Node)) {
        setIsOpen(false);
      }
    }

    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  const currentClient = getCurrentClient();
  const displayName = currentClient ? currentClient.name : 'All Clients';
  const displayColor = currentClient?.color || '#6366f1';

  const handleSelect = (clientId: string | null) => {
    setCurrentClient(clientId);
    setIsOpen(false);
  };

  return (
    <div className="relative" ref={dropdownRef}>
      <button
        onClick={() => setIsOpen(!isOpen)}
        className="flex items-center gap-2 px-3 py-2 bg-surface-secondary rounded-lg border border-border hover:border-primary transition-colors min-w-[180px]"
      >
        <div
          className="w-3 h-3 rounded-full flex-shrink-0"
          style={{ backgroundColor: displayColor }}
        />
        <span className="text-sm text-text-primary truncate flex-1 text-left">
          {loading ? 'Loading...' : displayName}
        </span>
        <ChevronDownIcon className={`w-4 h-4 text-text-secondary transition-transform ${isOpen ? 'rotate-180' : ''}`} />
      </button>

      {isOpen && (
        <div className="absolute top-full left-0 mt-1 w-full min-w-[220px] bg-surface border border-border rounded-lg shadow-lg z-50 py-1 max-h-[300px] overflow-y-auto">
          {/* All Clients option */}
          <button
            onClick={() => handleSelect(null)}
            className={`w-full flex items-center gap-2 px-3 py-2 hover:bg-surface-secondary transition-colors ${
              currentClientId === null ? 'bg-primary/10' : ''
            }`}
          >
            <div className="w-3 h-3 rounded-full bg-gradient-to-br from-primary to-secondary flex-shrink-0" />
            <span className="text-sm text-text-primary">All Clients</span>
            {currentClientId === null && <CheckIcon className="w-4 h-4 text-primary ml-auto" />}
          </button>

          {clients.length > 0 && (
            <div className="h-px bg-border my-1" />
          )}

          {/* Client list */}
          {clients.map((client) => (
            <button
              key={client.id}
              onClick={() => handleSelect(client.id)}
              className={`w-full flex items-center gap-2 px-3 py-2 hover:bg-surface-secondary transition-colors ${
                currentClientId === client.id ? 'bg-primary/10' : ''
              }`}
            >
              <div
                className="w-3 h-3 rounded-full flex-shrink-0"
                style={{ backgroundColor: client.color || '#6366f1' }}
              />
              <div className="flex-1 text-left min-w-0">
                <span className="text-sm text-text-primary truncate block">{client.name}</span>
                {client.deviceCount !== undefined && (
                  <span className="text-xs text-text-secondary">
                    {client.deviceCount} device{client.deviceCount !== 1 ? 's' : ''}
                  </span>
                )}
              </div>
              {currentClientId === client.id && <CheckIcon className="w-4 h-4 text-primary flex-shrink-0" />}
            </button>
          ))}

          {clients.length === 0 && !loading && (
            <div className="px-3 py-4 text-center text-sm text-text-secondary">
              No clients yet. Create one in Settings.
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function ChevronDownIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
    </svg>
  );
}

function CheckIcon({ className }: { className?: string }) {
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
    </svg>
  );
}
