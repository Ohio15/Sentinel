import { create } from 'zustand';

export interface Ticket {
  id: string;
  ticketNumber: number;
  subject: string;
  description?: string;
  status: 'open' | 'in_progress' | 'waiting' | 'resolved' | 'closed';
  priority: 'low' | 'medium' | 'high' | 'urgent';
  type: 'incident' | 'request' | 'problem' | 'change';
  deviceId?: string;
  deviceName?: string;
  deviceDisplayName?: string;
  requesterName?: string;
  requesterEmail?: string;
  assignedTo?: string;
  tags: string[];
  dueDate?: string;
  resolvedAt?: string;
  closedAt?: string;
  createdAt: string;
  updatedAt: string;
  // SLA fields
  slaPolicyId?: string;
  firstResponseAt?: string;
  firstResponseDueAt?: string;
  resolutionDueAt?: string;
  slaResponseBreached?: boolean;
  slaResolutionBreached?: boolean;
  slaPausedAt?: string;
  slaPausedDurationMinutes?: number;
  // Category field
  categoryId?: string;
  categoryName?: string;
  // Custom fields
  customFields?: Record<string, any>;
}

export interface TicketComment {
  id: string;
  ticketId: string;
  content: string;
  isInternal: boolean;
  authorName: string;
  authorEmail?: string;
  attachments: string[];
  createdAt: string;
}

export interface TicketActivity {
  id: string;
  ticketId: string;
  action: string;
  fieldName?: string;
  oldValue?: string;
  newValue?: string;
  actorName: string;
  createdAt: string;
}

export interface TicketTemplate {
  id: string;
  name: string;
  subject?: string;
  content: string;
  isActive: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface TicketStats {
  openCount: number;
  inProgressCount: number;
  waitingCount: number;
  resolvedCount: number;
  closedCount: number;
  totalCount: number;
}

export interface TicketFilters {
  status?: string;
  priority?: string;
  assignedTo?: string;
  deviceId?: string;
}

interface TicketState {
  tickets: Ticket[];
  selectedTicket: Ticket | null;
  ticketComments: TicketComment[];
  ticketActivity: TicketActivity[];
  templates: TicketTemplate[];
  stats: TicketStats | null;
  filters: TicketFilters;
  loading: boolean;
  error: string | null;

  // Actions
  fetchTickets: (filters?: TicketFilters) => Promise<void>;
  fetchTicket: (id: string) => Promise<void>;
  createTicket: (ticket: Partial<Ticket>) => Promise<Ticket>;
  updateTicket: (id: string, updates: Partial<Ticket> & { actorName?: string }) => Promise<Ticket>;
  deleteTicket: (id: string) => Promise<void>;
  fetchComments: (ticketId: string) => Promise<void>;
  addComment: (comment: Omit<TicketComment, 'id' | 'createdAt'>) => Promise<TicketComment>;
  fetchActivity: (ticketId: string) => Promise<void>;
  fetchStats: () => Promise<void>;
  fetchTemplates: () => Promise<void>;
  setFilters: (filters: TicketFilters) => void;
  clearSelectedTicket: () => void;
}

export const useTicketStore = create<TicketState>((set, get) => ({
  tickets: [],
  selectedTicket: null,
  ticketComments: [],
  ticketActivity: [],
  templates: [],
  stats: null,
  filters: {},
  loading: false,
  error: null,

  fetchTickets: async (filters?: TicketFilters) => {
    set({ loading: true, error: null });
    try {
      const appliedFilters = filters || get().filters;
      const tickets = await window.api.tickets.list(appliedFilters);
      set({ tickets, loading: false });
    } catch (error: any) {
      set({ error: error.message, loading: false });
    }
  },

  fetchTicket: async (id: string) => {
    set({ loading: true, error: null });
    try {
      const ticket = await window.api.tickets.get(id);
      set({ selectedTicket: ticket, loading: false });
      // Also fetch comments and activity
      if (ticket) {
        get().fetchComments(id);
        get().fetchActivity(id);
      }
    } catch (error: any) {
      set({ error: error.message, loading: false });
    }
  },

  createTicket: async (ticket: Partial<Ticket>) => {
    set({ loading: true, error: null });
    try {
      const newTicket = await window.api.tickets.create(ticket as any);
      set((state) => ({
        tickets: [newTicket, ...state.tickets],
        loading: false,
      }));
      return newTicket;
    } catch (error: any) {
      set({ error: error.message, loading: false });
      throw error;
    }
  },

  updateTicket: async (id: string, updates: Partial<Ticket> & { actorName?: string }) => {
    set({ loading: true, error: null });
    try {
      const updatedTicket = await window.api.tickets.update(id, updates);
      set((state) => ({
        tickets: state.tickets.map((t) => (t.id === id ? updatedTicket : t)),
        selectedTicket: state.selectedTicket?.id === id ? updatedTicket : state.selectedTicket,
        loading: false,
      }));
      return updatedTicket;
    } catch (error: any) {
      set({ error: error.message, loading: false });
      throw error;
    }
  },

  deleteTicket: async (id: string) => {
    set({ loading: true, error: null });
    try {
      await window.api.tickets.delete(id);
      set((state) => ({
        tickets: state.tickets.filter((t) => t.id !== id),
        selectedTicket: state.selectedTicket?.id === id ? null : state.selectedTicket,
        loading: false,
      }));
    } catch (error: any) {
      set({ error: error.message, loading: false });
      throw error;
    }
  },

  fetchComments: async (ticketId: string) => {
    try {
      const comments = await window.api.tickets.getComments(ticketId);
      set({ ticketComments: comments });
    } catch (error: any) {
      console.error('Failed to fetch comments:', error);
    }
  },

  addComment: async (comment: Omit<TicketComment, 'id' | 'createdAt'>) => {
    try {
      const newComment = await window.api.tickets.addComment(comment);
      set((state) => ({
        ticketComments: [...state.ticketComments, newComment],
      }));
      return newComment;
    } catch (error: any) {
      throw error;
    }
  },

  fetchActivity: async (ticketId: string) => {
    try {
      const activity = await window.api.tickets.getActivity(ticketId);
      set({ ticketActivity: activity });
    } catch (error: any) {
      console.error('Failed to fetch activity:', error);
    }
  },

  fetchStats: async () => {
    try {
      const stats = await window.api.tickets.getStats();
      set({ stats });
    } catch (error: any) {
      console.error('Failed to fetch stats:', error);
    }
  },

  fetchTemplates: async () => {
    try {
      const templates = await window.api.tickets.getTemplates();
      set({ templates });
    } catch (error: any) {
      console.error('Failed to fetch templates:', error);
    }
  },

  setFilters: (filters: TicketFilters) => {
    set({ filters });
    get().fetchTickets(filters);
  },

  clearSelectedTicket: () => {
    set({ selectedTicket: null, ticketComments: [], ticketActivity: [] });
  },
}));
