import { create } from 'zustand';

export interface CertificateInfo {
  name: string;
  type: 'ca' | 'server';
  path: string;
  exists: boolean;
  subject?: string;
  issuer?: string;
  validFrom?: string;
  validTo?: string;
  fingerprint?: string;
  serialNumber?: string;
  daysUntilExpiry?: number;
  status: 'valid' | 'expiring_soon' | 'expired' | 'missing';
}

export interface AgentCertStatus {
  agentId: string;
  agentName?: string;
  caCertHash: string;
  distributedAt: string | null;
  confirmedAt: string | null;
}

interface CertificateState {
  certificates: CertificateInfo[];
  agentStatuses: AgentCertStatus[];
  currentCertHash: string | null;
  loading: boolean;
  renewing: boolean;
  distributing: boolean;
  error: string | null;

  fetchCertificates: () => Promise<void>;
  fetchAgentStatuses: () => Promise<void>;
  renewCertificates: () => Promise<void>;
  distributeCertificates: () => Promise<{ success: number; failed: number }>;
  subscribeToEvents: () => () => void;
}

export const useCertificateStore = create<CertificateState>((set, get) => ({
  certificates: [],
  agentStatuses: [],
  currentCertHash: null,
  loading: false,
  renewing: false,
  distributing: false,
  error: null,

  fetchCertificates: async () => {
    set({ loading: true, error: null });
    try {
      const result = await window.api.certs.list();
      // listCertificates returns { certificates, certsDir, caCertHash }
      const certificates = result?.certificates || [];
      const caCertHash = result?.caCertHash || null;
      set({
        certificates,
        currentCertHash: caCertHash,
        loading: false,
      });
    } catch (error: any) {
      set({ error: error.message, loading: false });
    }
  },

  fetchAgentStatuses: async () => {
    try {
      const agentStatuses = await window.api.certs.getAgentStatus();
      set({ agentStatuses });
    } catch (error: any) {
      console.error('Failed to fetch agent cert statuses:', error);
    }
  },

  renewCertificates: async () => {
    set({ renewing: true, error: null });
    try {
      const result = await window.api.certs.renew();
      if (!result.success) {
        throw new Error(result.error || 'Failed to renew certificates');
      }
      // Refresh the certificates list after renewal
      await get().fetchCertificates();
      set({ renewing: false });
    } catch (error: any) {
      set({ error: error.message, renewing: false });
      throw error;
    }
  },

  distributeCertificates: async () => {
    set({ distributing: true, error: null });
    try {
      const result = await window.api.certs.distribute();
      // Update will come via event subscription
      set({ distributing: false });
      return result;
    } catch (error: any) {
      set({ error: error.message, distributing: false });
      throw error;
    }
  },

  subscribeToEvents: () => {
    const unsubDistributed = window.api.certs.onDistributed((result) => {
      console.log('[Certs] Distribution result:', result);
      // Refresh agent statuses after distribution
      get().fetchAgentStatuses();
    });

    const unsubConfirmed = window.api.certs.onAgentConfirmed((data) => {
      console.log('[Certs] Agent confirmed:', data);
      // Update the specific agent's status
      set((state) => ({
        agentStatuses: state.agentStatuses.map((status) =>
          status.agentId === data.agentId
            ? { ...status, caCertHash: data.certHash, confirmedAt: new Date().toISOString() }
            : status
        ),
      }));
    });

    return () => {
      unsubDistributed();
      unsubConfirmed();
    };
  },
}));
