import { create } from 'zustand';
import { api } from '../services/api';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface ProjectStats {
  project: string;
  totalRuns: number;
  lastStatus: string;
  openIssues: number;
  passRate: number | null;
}

export interface TestRun {
  id: string;
  organizationId: number;
  project: string;
  branch: string;
  commitSha: string | null;
  triggerType: string;
  status: string;
  totalTests: number;
  passed: number;
  failed: number;
  skipped: number;
  durationMs: number | null;
  environment: string | null;
  runner: string | null;
  summary: string | null;
  startedAt: string;
  finishedAt: string | null;
  createdAt: string;
}

export interface TestResult {
  id: string;
  runId: string;
  testName: string;
  suite: string | null;
  status: string;
  durationMs: number | null;
  errorMessage: string | null;
  stackTrace: string | null;
  retryCount: number;
  createdAt: string;
}

export interface TestIssue {
  id: string;
  organizationId: number;
  project: string;
  testName: string;
  title: string;
  status: string;
  severity: string;
  firstSeenAt: string;
  lastSeenAt: string;
  occurrenceCount: number;
  firstRunId: string | null;
  lastRunId: string | null;
  assignedTo: string | null;
  resolvedBy: string | null;
  resolvedAt: string | null;
  notes: string | null;
  createdAt: string;
  updatedAt: string;
  assignedToEmail?: string;
  resolvedByEmail?: string;
}

interface RunsResponse {
  runs: TestRun[];
  total: number;
  page: number;
  pageSize: number;
  totalPages: number;
}

interface IssuesResponse {
  issues: TestIssue[];
  total: number;
  page: number;
  pageSize: number;
  totalPages: number;
}

interface RunDetailResponse {
  run: TestRun;
  results: TestResult[];
}

interface StatsResponse {
  projects: ProjectStats[];
}

// ---------------------------------------------------------------------------
// Store
// ---------------------------------------------------------------------------

interface TestCenterState {
  // Dashboard stats
  projectStats: ProjectStats[];
  statsLoading: boolean;

  // Runs
  runs: TestRun[];
  runsTotal: number;
  runsPage: number;
  runsTotalPages: number;
  runsLoading: boolean;

  // Run detail
  selectedRun: TestRun | null;
  selectedRunResults: TestResult[];
  runDetailLoading: boolean;

  // Issues
  issues: TestIssue[];
  issuesTotal: number;
  issuesPage: number;
  issuesTotalPages: number;
  issuesLoading: boolean;

  // Error
  error: string | null;

  // Actions
  fetchStats: () => Promise<void>;
  fetchRuns: (params?: { project?: string; status?: string; branch?: string; page?: number; pageSize?: number }) => Promise<void>;
  fetchRunDetail: (id: string) => Promise<void>;
  clearRunDetail: () => void;
  fetchIssues: (params?: { project?: string; status?: string; severity?: string; page?: number; pageSize?: number }) => Promise<void>;
  updateIssue: (id: string, updates: { status?: string; severity?: string; assignedTo?: string; notes?: string }) => Promise<void>;
}

export const useTestCenterStore = create<TestCenterState>((set, get) => ({
  projectStats: [],
  statsLoading: false,

  runs: [],
  runsTotal: 0,
  runsPage: 1,
  runsTotalPages: 1,
  runsLoading: false,

  selectedRun: null,
  selectedRunResults: [],
  runDetailLoading: false,

  issues: [],
  issuesTotal: 0,
  issuesPage: 1,
  issuesTotalPages: 1,
  issuesLoading: false,

  error: null,

  fetchStats: async () => {
    set({ statsLoading: true, error: null });
    try {
      const data = await api.makeRequest<StatsResponse>('GET', '/admin/test-center/stats');
      set({ projectStats: data.projects || [], statsLoading: false });
    } catch (error: unknown) {
      set({ error: error instanceof Error ? error.message : 'Failed to fetch stats', statsLoading: false });
    }
  },

  fetchRuns: async (params) => {
    set({ runsLoading: true, error: null });
    try {
      const queryParams: Record<string, string> = {};
      if (params?.project) queryParams.project = params.project;
      if (params?.status) queryParams.status = params.status;
      if (params?.branch) queryParams.branch = params.branch;
      if (params?.page) queryParams.page = String(params.page);
      if (params?.pageSize) queryParams.pageSize = String(params.pageSize);

      const data = await api.makeRequest<RunsResponse>(
        'GET', '/admin/test-center/runs', undefined,
        Object.keys(queryParams).length ? queryParams : undefined
      );
      set({
        runs: data.runs || [],
        runsTotal: data.total,
        runsPage: data.page,
        runsTotalPages: data.totalPages,
        runsLoading: false,
      });
    } catch (error: unknown) {
      set({ error: error instanceof Error ? error.message : 'Failed to fetch runs', runsLoading: false });
    }
  },

  fetchRunDetail: async (id: string) => {
    set({ runDetailLoading: true, error: null });
    try {
      const data = await api.makeRequest<RunDetailResponse>('GET', `/admin/test-center/runs/${id}`);
      set({
        selectedRun: data.run,
        selectedRunResults: data.results || [],
        runDetailLoading: false,
      });
    } catch (error: unknown) {
      set({ error: error instanceof Error ? error.message : 'Failed to fetch run detail', runDetailLoading: false });
    }
  },

  clearRunDetail: () => {
    set({ selectedRun: null, selectedRunResults: [] });
  },

  fetchIssues: async (params) => {
    set({ issuesLoading: true, error: null });
    try {
      const queryParams: Record<string, string> = {};
      if (params?.project) queryParams.project = params.project;
      if (params?.status) queryParams.status = params.status;
      if (params?.severity) queryParams.severity = params.severity;
      if (params?.page) queryParams.page = String(params.page);
      if (params?.pageSize) queryParams.pageSize = String(params.pageSize);

      const data = await api.makeRequest<IssuesResponse>(
        'GET', '/admin/test-center/issues', undefined,
        Object.keys(queryParams).length ? queryParams : undefined
      );
      set({
        issues: data.issues || [],
        issuesTotal: data.total,
        issuesPage: data.page,
        issuesTotalPages: data.totalPages,
        issuesLoading: false,
      });
    } catch (error: unknown) {
      set({ error: error instanceof Error ? error.message : 'Failed to fetch issues', issuesLoading: false });
    }
  },

  updateIssue: async (id: string, updates) => {
    try {
      const updatedIssue = await api.makeRequest<TestIssue>('PATCH', `/admin/test-center/issues/${id}`, updates);
      const { issues } = get();
      set({
        issues: issues.map(i => i.id === id ? { ...i, ...updatedIssue } : i),
      });
    } catch (error: unknown) {
      set({ error: error instanceof Error ? error.message : 'Failed to update issue' });
    }
  },
}));
