import { create } from 'zustand';
import { tickets as ticketsService } from '../services';

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
      const tickets = await ticketsService.list(appliedFilters);
      set({ tickets: tickets as Ticket[], loading: false });
    } catch (error: unknown) {
      set({ error: error instanceof Error ? error.message : 'Unknown error', loading: false });
    }
  },

  fetchTicket: async (id: string) => {
    set({ loading: true, error: null });
    try {
      const ticket = await ticketsService.get(id);
      set({ selectedTicket: ticket as Ticket, loading: false });
      // Also fetch comments and activity
      if (ticket) {
        get().fetchComments(id);
        get().fetchActivity(id);
      }
    } catch (error: unknown) {
      set({ error: error instanceof Error ? error.message : 'Unknown error', loading: false });
    }
  },

  createTicket: async (ticket: Partial<Ticket>) => {
    set({ loading: true, error: null });
    try {
      const newTicket = await ticketsService.create(ticket);
      set((state) => ({
        tickets: [newTicket as Ticket, ...state.tickets],
        loading: false,
      }));
      return newTicket as Ticket;
    } catch (error: unknown) {
      set({ error: error instanceof Error ? error.message : 'Unknown error', loading: false });
      throw error;
    }
  },

  updateTicket: async (id: string, updates: Partial<Ticket> & { actorName?: string }) => {
    set({ loading: true, error: null });
    try {
      const updatedTicket = await ticketsService.update(id, updates);
      set((state) => ({
        tickets: state.tickets.map((t) => (t.id === id ? updatedTicket as Ticket : t)),
        selectedTicket: state.selectedTicket?.id === id ? updatedTicket as Ticket : state.selectedTicket,
        loading: false,
      }));
      return updatedTicket as Ticket;
    } catch (error: unknown) {
      set({ error: error instanceof Error ? error.message : 'Unknown error', loading: false });
      throw error;
    }
  },

  deleteTicket: async (id: string) => {
    set({ loading: true, error: null });
    try {
      await ticketsService.delete(id);
      set((state) => ({
        tickets: state.tickets.filter((t) => t.id !== id),
        selectedTicket: state.selectedTicket?.id === id ? null : state.selectedTicket,
        loading: false,
      }));
    } catch (error: unknown) {
      set({ error: error instanceof Error ? error.message : 'Unknown error', loading: false });
      throw error;
    }
  },

  fetchComments: async (ticketId: string) => {
    try {
      const comments = await ticketsService.getComments(ticketId);
      set({ ticketComments: comments as TicketComment[] });
    } catch (error: unknown) {
      console.error('Failed to fetch comments:', error);
    }
  },

  addComment: async (comment: Omit<TicketComment, 'id' | 'createdAt'>) => {
    const newComment = await ticketsService.addComment(comment);
    set((state) => ({
      ticketComments: [...state.ticketComments, newComment as TicketComment],
    }));
    return newComment as TicketComment;
  },

  fetchActivity: async (ticketId: string) => {
    try {
      const activity = await ticketsService.getActivity(ticketId);
      set({ ticketActivity: activity as TicketActivity[] });
    } catch (error: unknown) {
      console.error('Failed to fetch activity:', error);
    }
  },

  fetchStats: async () => {
    try {
      const stats = await ticketsService.getStats();
      set({ stats: stats as TicketStats });
    } catch (error: unknown) {
      console.error('Failed to fetch stats:', error);
    }
  },

  fetchTemplates: async () => {
    try {
      const templates = await ticketsService.getTemplates();
      set({ templates: templates as TicketTemplate[] });
    } catch (error: unknown) {
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
