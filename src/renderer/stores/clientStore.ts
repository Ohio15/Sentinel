import { create } from 'zustand';
import { persist } from 'zustand/middleware';

export interface Client {
  id: string;
  name: string;
  description?: string;
  color?: string;
  logoUrl?: string;
  logoWidth?: number;
  logoHeight?: number;
  deviceCount?: number;
  openTicketCount?: number;
  createdAt: string;
  updatedAt: string;
}

interface ClientState {
  clients: Client[];
  currentClientId: string | null; // null = "All Clients" view
  loading: boolean;
  error: string | null;

  // Actions
  fetchClients: () => Promise<void>;
  setCurrentClient: (clientId: string | null) => void;
  createClient: (client: Omit<Client, 'id' | 'createdAt' | 'updatedAt' | 'deviceCount' | 'openTicketCount'>) => Promise<Client>;
  updateClient: (id: string, client: Partial<Client>) => Promise<Client | null>;
  deleteClient: (id: string) => Promise<void>;

  // Computed helpers
  getCurrentClient: () => Client | null;
}

export const useClientStore = create<ClientState>()(
  persist(
    (set, get) => ({
      clients: [],
      currentClientId: null,
      loading: false,
      error: null,

      fetchClients: async () => {
        set({ loading: true, error: null });
        try {
          const clients = await window.api.clients.list();
          set({ clients, loading: false });
        } catch (error: any) {
          set({ error: error.message, loading: false });
        }
      },

      setCurrentClient: (clientId: string | null) => {
        set({ currentClientId: clientId });
      },

      createClient: async (client) => {
        try {
          const newClient = await window.api.clients.create(client);
          const { clients } = get();
          set({ clients: [...clients, newClient] });
          return newClient;
        } catch (error: any) {
          set({ error: error.message });
          throw error;
        }
      },

      updateClient: async (id, updates) => {
        try {
          const updatedClient = await window.api.clients.update(id, updates);
          if (updatedClient) {
            const { clients } = get();
            set({
              clients: clients.map(c => c.id === id ? updatedClient : c)
            });
          }
          return updatedClient;
        } catch (error: any) {
          set({ error: error.message });
          throw error;
        }
      },

      deleteClient: async (id) => {
        try {
          await window.api.clients.delete(id);
          const { clients, currentClientId } = get();
          set({
            clients: clients.filter(c => c.id !== id),
            // Reset to "All Clients" if current client is deleted
            currentClientId: currentClientId === id ? null : currentClientId
          });
        } catch (error: any) {
          set({ error: error.message });
          throw error;
        }
      },

      getCurrentClient: () => {
        const { clients, currentClientId } = get();
        if (!currentClientId) return null;
        return clients.find(c => c.id === currentClientId) || null;
      },
    }),
    {
      name: 'sentinel-client-store',
      partialize: (state) => ({ currentClientId: state.currentClientId }),
    }
  )
);
