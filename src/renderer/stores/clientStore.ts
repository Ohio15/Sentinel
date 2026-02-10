import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import { clients as clientsService } from '../services';

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
          const clients = await clientsService.list();
          set({ clients: clients as Client[], loading: false });
        } catch (error: unknown) {
          set({ error: error instanceof Error ? error.message : 'Unknown error', loading: false });
        }
      },

      setCurrentClient: (clientId: string | null) => {
        set({ currentClientId: clientId });
      },

      createClient: async (client) => {
        try {
          const newClient = await clientsService.create(client);
          const { clients } = get();
          set({ clients: [...clients, newClient as Client] });
          return newClient as Client;
        } catch (error: unknown) {
          set({ error: error instanceof Error ? error.message : 'Unknown error' });
          throw error;
        }
      },

      updateClient: async (id, updates) => {
        try {
          const updatedClient = await clientsService.update(id, updates);
          if (updatedClient) {
            const { clients } = get();
            set({
              clients: clients.map(c => c.id === id ? updatedClient as Client : c)
            });
          }
          return updatedClient as Client | null;
        } catch (error: unknown) {
          set({ error: error instanceof Error ? error.message : 'Unknown error' });
          throw error;
        }
      },

      deleteClient: async (id) => {
        try {
          await clientsService.delete(id);
          const { clients, currentClientId } = get();
          set({
            clients: clients.filter(c => c.id !== id),
            // Reset to "All Clients" if current client is deleted
            currentClientId: currentClientId === id ? null : currentClientId
          });
        } catch (error: unknown) {
          set({ error: error instanceof Error ? error.message : 'Unknown error' });
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
