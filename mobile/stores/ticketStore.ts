/**
 * Ticket Store for Sentinel Mobile
 * Manages ticket state using Zustand
 */
import { create } from 'zustand';
import { api } from '@/services/api';

// Ticket types
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
  requesterName?: string;
  requesterEmail?: string;
  assignedTo?: string;
  categoryId?: string;
  categoryName?: string;
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
}

export interface TicketComment {
  id: string;
  ticketId: string;
  content: string;
  isInternal: boolean;
  authorId: string;
  authorName: string;
  authorEmail?: string;
  attachments: string[];
  createdAt: string;
}

export interface TicketFilters {
  status?: string;
  priority?: string;
  search?: string;
  assignedTo?: string;
}

export interface TicketStats {
  openCount: number;
  inProgressCount: number;
  waitingCount: number;
  resolvedCount: number;
  closedCount: number;
  totalCount: number;
}

interface TicketState {
  // Data
  tickets: Ticket[];
  selectedTicket: Ticket | null;
  comments: TicketComment[];
  stats: TicketStats | null;

  // Filters
  filters: TicketFilters;

  // Loading states
  loading: boolean;
  refreshing: boolean;
  loadingComments: boolean;
  submitting: boolean;
  error: string | null;

  // Actions
  fetchTickets: () => Promise<void>;
  refreshTickets: () => Promise<void>;
  fetchTicket: (id: string) => Promise<void>;
  createTicket: (ticket: Partial<Ticket>) => Promise<Ticket>;
  updateTicket: (id: string, updates: Partial<Ticket>) => Promise<Ticket>;
  fetchComments: (ticketId: string) => Promise<void>;
  addComment: (ticketId: string, content: string, isInternal?: boolean) => Promise<TicketComment>;
  fetchStats: () => Promise<void>;
  setFilters: (filters: TicketFilters) => void;
  setStatusFilter: (status: string | undefined) => void;
  setPriorityFilter: (priority: string | undefined) => void;
  setSearchFilter: (search: string) => void;
  clearFilters: () => void;
  clearSelectedTicket: () => void;
  clearError: () => void;
}

// API response types
interface TicketListResponse {
  tickets: Ticket[];
  total: number;
  page: number;
  pageSize: number;
}

interface CommentsResponse {
  comments: TicketComment[];
}

export const useTicketStore = create<TicketState>((set, get) => ({
  // Initial state
  tickets: [],
  selectedTicket: null,
  comments: [],
  stats: null,
  filters: {},
  loading: false,
  refreshing: false,
  loadingComments: false,
  submitting: false,
  error: null,

  // Fetch all tickets with filters
  fetchTickets: async () => {
    set({ loading: true, error: null });
    try {
      const { filters } = get();
      const params: Record<string, string> = {};

      if (filters.status) params.status = filters.status;
      if (filters.priority) params.priority = filters.priority;
      if (filters.search) params.search = filters.search;
      if (filters.assignedTo) params.assignedTo = filters.assignedTo;

      const response = await api.get<TicketListResponse>('/tickets', params);
      set({ tickets: response.tickets || [], loading: false });
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to fetch tickets';
      console.error('[TicketStore] fetchTickets error:', message);
      set({ error: message, loading: false });
    }
  },

  // Refresh tickets (for pull-to-refresh)
  refreshTickets: async () => {
    set({ refreshing: true, error: null });
    try {
      const { filters } = get();
      const params: Record<string, string> = {};

      if (filters.status) params.status = filters.status;
      if (filters.priority) params.priority = filters.priority;
      if (filters.search) params.search = filters.search;
      if (filters.assignedTo) params.assignedTo = filters.assignedTo;

      const response = await api.get<TicketListResponse>('/tickets', params);
      set({ tickets: response.tickets || [], refreshing: false });
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to refresh tickets';
      console.error('[TicketStore] refreshTickets error:', message);
      set({ error: message, refreshing: false });
    }
  },

  // Fetch single ticket by ID
  fetchTicket: async (id: string) => {
    set({ loading: true, error: null });
    try {
      const ticket = await api.get<Ticket>(`/tickets/${id}`);
      set({ selectedTicket: ticket, loading: false });
      // Also fetch comments
      get().fetchComments(id);
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to fetch ticket';
      console.error('[TicketStore] fetchTicket error:', message);
      set({ error: message, loading: false });
    }
  },

  // Create new ticket
  createTicket: async (ticketData: Partial<Ticket>) => {
    set({ submitting: true, error: null });
    try {
      const newTicket = await api.post<Ticket>('/tickets', ticketData);
      set((state) => ({
        tickets: [newTicket, ...state.tickets],
        submitting: false,
      }));
      return newTicket;
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to create ticket';
      console.error('[TicketStore] createTicket error:', message);
      set({ error: message, submitting: false });
      throw error;
    }
  },

  // Update existing ticket
  updateTicket: async (id: string, updates: Partial<Ticket>) => {
    set({ submitting: true, error: null });
    try {
      const updatedTicket = await api.put<Ticket>(`/tickets/${id}`, updates);
      set((state) => ({
        tickets: state.tickets.map((t) => (t.id === id ? updatedTicket : t)),
        selectedTicket: state.selectedTicket?.id === id ? updatedTicket : state.selectedTicket,
        submitting: false,
      }));
      return updatedTicket;
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to update ticket';
      console.error('[TicketStore] updateTicket error:', message);
      set({ error: message, submitting: false });
      throw error;
    }
  },

  // Fetch comments for a ticket
  fetchComments: async (ticketId: string) => {
    set({ loadingComments: true });
    try {
      const response = await api.get<CommentsResponse>(`/tickets/${ticketId}/comments`);
      set({ comments: response.comments || [], loadingComments: false });
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to fetch comments';
      console.error('[TicketStore] fetchComments error:', message);
      set({ comments: [], loadingComments: false });
    }
  },

  // Add comment to ticket
  addComment: async (ticketId: string, content: string, isInternal: boolean = false) => {
    set({ submitting: true, error: null });
    try {
      const newComment = await api.post<TicketComment>(`/tickets/${ticketId}/comments`, {
        ticketId,
        content,
        isInternal,
      });
      set((state) => ({
        comments: [...state.comments, newComment],
        submitting: false,
      }));
      return newComment;
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to add comment';
      console.error('[TicketStore] addComment error:', message);
      set({ error: message, submitting: false });
      throw error;
    }
  },

  // Fetch ticket statistics
  fetchStats: async () => {
    try {
      const stats = await api.get<TicketStats>('/tickets/stats');
      set({ stats });
    } catch (error) {
      console.error('[TicketStore] fetchStats error:', error);
    }
  },

  // Set filters
  setFilters: (filters: TicketFilters) => {
    set({ filters });
  },

  // Set status filter
  setStatusFilter: (status: string | undefined) => {
    set((state) => ({
      filters: { ...state.filters, status },
    }));
    get().fetchTickets();
  },

  // Set priority filter
  setPriorityFilter: (priority: string | undefined) => {
    set((state) => ({
      filters: { ...state.filters, priority },
    }));
    get().fetchTickets();
  },

  // Set search filter
  setSearchFilter: (search: string) => {
    set((state) => ({
      filters: { ...state.filters, search: search || undefined },
    }));
    get().fetchTickets();
  },

  // Clear all filters
  clearFilters: () => {
    set({ filters: {} });
    get().fetchTickets();
  },

  // Clear selected ticket
  clearSelectedTicket: () => {
    set({ selectedTicket: null, comments: [] });
  },

  // Clear error
  clearError: () => {
    set({ error: null });
  },
}));
