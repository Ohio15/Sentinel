import React, { useState, useEffect, useCallback } from 'react';
import {
  useTestCenterStore,
  ProjectStats,
  TestRun,
  TestResult,
  TestIssue,
} from '../stores/testCenterStore';

type Tab = 'dashboard' | 'runs' | 'issues';

export function TestCenter() {
  const [activeTab, setActiveTab] = useState<Tab>('dashboard');

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold text-text-primary">Test Center</h1>
      </div>

      {/* Tabs */}
      <div className="border-b border-border">
        <div className="flex gap-4">
          {(['dashboard', 'runs', 'issues'] as Tab[]).map((tab) => (
            <button
              key={tab}
              onClick={() => setActiveTab(tab)}
              className={`pb-2 px-1 text-sm font-medium border-b-2 transition-colors ${
                activeTab === tab
                  ? 'border-primary text-primary'
                  : 'border-transparent text-text-secondary hover:text-text-primary'
              }`}
            >
              {tab === 'dashboard' ? 'Dashboard' : tab === 'runs' ? 'Runs' : 'Issues'}
            </button>
          ))}
        </div>
      </div>

      {activeTab === 'dashboard' && <DashboardTab />}
      {activeTab === 'runs' && <RunsTab />}
      {activeTab === 'issues' && <IssuesTab />}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Dashboard Tab
// ---------------------------------------------------------------------------

function DashboardTab() {
  const { projectStats, statsLoading, fetchStats } = useTestCenterStore();

  useEffect(() => {
    fetchStats();
  }, []);

  if (statsLoading) {
    return (
      <div className="card p-8 text-center">
        <p className="text-text-secondary">Loading stats...</p>
      </div>
    );
  }

  if (projectStats.length === 0) {
    return (
      <div className="card p-8 text-center">
        <p className="text-text-secondary">No test data available yet. Submit test results to see stats here.</p>
      </div>
    );
  }

  return (
    <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
      {projectStats.map((project) => (
        <ProjectCard key={project.project} stats={project} />
      ))}
    </div>
  );
}

function ProjectCard({ stats }: { stats: ProjectStats }) {
  const passRate = stats.passRate != null ? stats.passRate : 0;

  return (
    <div className="card p-5">
      <div className="flex items-center justify-between mb-4">
        <h3 className="font-semibold text-text-primary text-lg">{stats.project}</h3>
        <RunStatusBadge status={stats.lastStatus} />
      </div>

      <div className="space-y-3">
        {/* Pass Rate */}
        <div>
          <div className="flex items-center justify-between text-sm mb-1">
            <span className="text-text-secondary">Pass Rate</span>
            <span className="font-medium text-text-primary">{passRate.toFixed(1)}%</span>
          </div>
          <div className="w-full bg-gray-200 dark:bg-slate-700 rounded-full h-2">
            <div
              className={`h-2 rounded-full transition-all ${
                passRate >= 90 ? 'bg-success' : passRate >= 70 ? 'bg-warning' : 'bg-danger'
              }`}
              style={{ width: `${Math.min(passRate, 100)}%` }}
            />
          </div>
        </div>

        {/* Stats Row */}
        <div className="flex items-center justify-between text-sm">
          <div className="text-text-secondary">
            <span className="font-medium text-text-primary">{stats.totalRuns}</span> total runs
          </div>
          <div className="text-text-secondary">
            <span className={`font-medium ${stats.openIssues > 0 ? 'text-danger' : 'text-success'}`}>
              {stats.openIssues}
            </span>{' '}
            open issues
          </div>
        </div>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Runs Tab
// ---------------------------------------------------------------------------

function RunsTab() {
  const {
    runs,
    runsTotal,
    runsPage,
    runsTotalPages,
    runsLoading,
    fetchRuns,
    fetchRunDetail,
    selectedRun,
    selectedRunResults,
    runDetailLoading,
    clearRunDetail,
  } = useTestCenterStore();

  const [projectFilter, setProjectFilter] = useState('');
  const [statusFilter, setStatusFilter] = useState('');
  const [branchFilter, setBranchFilter] = useState('');
  const [expandedRunId, setExpandedRunId] = useState<string | null>(null);

  const loadRuns = useCallback(
    (page?: number) => {
      fetchRuns({
        project: projectFilter || undefined,
        status: statusFilter || undefined,
        branch: branchFilter || undefined,
        page: page || 1,
      });
    },
    [projectFilter, statusFilter, branchFilter, fetchRuns]
  );

  useEffect(() => {
    loadRuns();
  }, []);

  const handleFilterApply = () => {
    loadRuns(1);
  };

  const handleExpandRun = (runId: string) => {
    if (expandedRunId === runId) {
      setExpandedRunId(null);
      clearRunDetail();
    } else {
      setExpandedRunId(runId);
      fetchRunDetail(runId);
    }
  };

  return (
    <>
      {/* Filters */}
      <div className="flex gap-3 items-end flex-wrap">
        <div>
          <label className="label">Project</label>
          <input
            type="text"
            value={projectFilter}
            onChange={(e) => setProjectFilter(e.target.value)}
            placeholder="All projects"
            className="input w-44"
          />
        </div>
        <div>
          <label className="label">Status</label>
          <select
            value={statusFilter}
            onChange={(e) => setStatusFilter(e.target.value)}
            className="input w-36"
          >
            <option value="">All</option>
            <option value="passed">Passed</option>
            <option value="failed">Failed</option>
            <option value="running">Running</option>
            <option value="error">Error</option>
          </select>
        </div>
        <div>
          <label className="label">Branch</label>
          <input
            type="text"
            value={branchFilter}
            onChange={(e) => setBranchFilter(e.target.value)}
            placeholder="All branches"
            className="input w-36"
          />
        </div>
        <button onClick={handleFilterApply} className="btn btn-primary">
          Filter
        </button>
      </div>

      {/* Table */}
      {runsLoading ? (
        <div className="card p-8 text-center">
          <p className="text-text-secondary">Loading test runs...</p>
        </div>
      ) : runs.length === 0 ? (
        <div className="card p-8 text-center">
          <p className="text-text-secondary">No test runs found</p>
        </div>
      ) : (
        <div className="card overflow-hidden flex flex-col max-h-[calc(100vh-340px)]">
          <div className="overflow-auto flex-1">
            <table>
              <thead className="sticky top-0 bg-surface z-10">
                <tr>
                  <th className="w-8"></th>
                  <th>Project</th>
                  <th>Branch</th>
                  <th>Status</th>
                  <th>Tests</th>
                  <th>Pass / Fail / Skip</th>
                  <th>Duration</th>
                  <th>Trigger</th>
                  <th>Started</th>
                </tr>
              </thead>
              <tbody>
                {runs.map((run) => (
                  <React.Fragment key={run.id}>
                    <tr
                      className="cursor-pointer"
                      onClick={() => handleExpandRun(run.id)}
                    >
                      <td>
                        <span
                          className={`inline-block transition-transform text-text-secondary ${
                            expandedRunId === run.id ? 'rotate-90' : ''
                          }`}
                        >
                          &#9654;
                        </span>
                      </td>
                      <td className="font-medium text-text-primary">{run.project}</td>
                      <td className="text-text-secondary text-sm">{run.branch}</td>
                      <td>
                        <RunStatusBadge status={run.status} />
                      </td>
                      <td className="text-text-primary">{run.totalTests}</td>
                      <td>
                        <span className="text-success">{run.passed}</span>
                        {' / '}
                        <span className="text-danger">{run.failed}</span>
                        {' / '}
                        <span className="text-text-secondary">{run.skipped}</span>
                      </td>
                      <td className="text-text-secondary text-sm">
                        {run.durationMs != null ? formatDuration(run.durationMs) : '-'}
                      </td>
                      <td className="text-text-secondary text-sm">{run.triggerType}</td>
                      <td className="text-text-secondary text-sm">
                        {new Date(run.startedAt).toLocaleString()}
                      </td>
                    </tr>
                    {expandedRunId === run.id && (
                      <tr>
                        <td colSpan={9} className="!p-0">
                          <RunDetailPanel
                            results={selectedRunResults}
                            loading={runDetailLoading}
                            run={selectedRun}
                          />
                        </td>
                      </tr>
                    )}
                  </React.Fragment>
                ))}
              </tbody>
            </table>
          </div>

          {/* Pagination */}
          {runsTotalPages > 1 && (
            <div className="flex items-center justify-between px-4 py-3 border-t border-border">
              <span className="text-sm text-text-secondary">
                {runsTotal} total runs
              </span>
              <div className="flex gap-2">
                <button
                  onClick={() => loadRuns(runsPage - 1)}
                  disabled={runsPage <= 1}
                  className="btn btn-secondary text-sm disabled:opacity-50"
                >
                  Previous
                </button>
                <span className="text-sm text-text-secondary flex items-center px-2">
                  Page {runsPage} of {runsTotalPages}
                </span>
                <button
                  onClick={() => loadRuns(runsPage + 1)}
                  disabled={runsPage >= runsTotalPages}
                  className="btn btn-secondary text-sm disabled:opacity-50"
                >
                  Next
                </button>
              </div>
            </div>
          )}
        </div>
      )}
    </>
  );
}

function RunDetailPanel({
  results,
  loading,
  run,
}: {
  results: TestResult[];
  loading: boolean;
  run: TestRun | null;
}) {
  if (loading) {
    return (
      <div className="p-4 bg-surface-alt text-center">
        <p className="text-text-secondary text-sm">Loading test results...</p>
      </div>
    );
  }

  return (
    <div className="bg-surface-alt border-t border-border">
      {run?.summary && (
        <div className="px-6 py-3 border-b border-border">
          <p className="text-sm text-text-secondary">
            <span className="font-medium text-text-primary">Summary:</span> {run.summary}
          </p>
          {run.commitSha && (
            <p className="text-xs text-text-secondary mt-1">
              Commit: <code className="bg-gray-200 dark:bg-slate-700 px-1 rounded">{run.commitSha.substring(0, 8)}</code>
              {run.environment && <> | Env: {run.environment}</>}
              {run.runner && <> | Runner: {run.runner}</>}
            </p>
          )}
        </div>
      )}
      {results.length === 0 ? (
        <div className="px-6 py-4">
          <p className="text-sm text-text-secondary">No individual test results recorded</p>
        </div>
      ) : (
        <div className="max-h-80 overflow-auto">
          <table>
            <thead className="sticky top-0 bg-surface-alt z-10">
              <tr>
                <th>Test Name</th>
                <th>Suite</th>
                <th>Status</th>
                <th>Duration</th>
                <th>Error</th>
              </tr>
            </thead>
            <tbody>
              {results.map((result) => (
                <tr key={result.id}>
                  <td className="text-text-primary text-sm font-medium">{result.testName}</td>
                  <td className="text-text-secondary text-sm">{result.suite || '-'}</td>
                  <td>
                    <TestStatusBadge status={result.status} />
                  </td>
                  <td className="text-text-secondary text-sm">
                    {result.durationMs != null ? formatDuration(result.durationMs) : '-'}
                  </td>
                  <td className="text-sm max-w-xs">
                    {result.errorMessage ? (
                      <span className="text-danger truncate block" title={result.errorMessage}>
                        {result.errorMessage.length > 80
                          ? result.errorMessage.substring(0, 80) + '...'
                          : result.errorMessage}
                      </span>
                    ) : (
                      <span className="text-text-secondary">-</span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Issues Tab
// ---------------------------------------------------------------------------

function IssuesTab() {
  const {
    issues,
    issuesTotal,
    issuesPage,
    issuesTotalPages,
    issuesLoading,
    fetchIssues,
    updateIssue,
  } = useTestCenterStore();

  const [projectFilter, setProjectFilter] = useState('');
  const [statusFilter, setStatusFilter] = useState('open');
  const [severityFilter, setSeverityFilter] = useState('');

  const loadIssues = useCallback(
    (page?: number) => {
      fetchIssues({
        project: projectFilter || undefined,
        status: statusFilter || undefined,
        severity: severityFilter || undefined,
        page: page || 1,
      });
    },
    [projectFilter, statusFilter, severityFilter, fetchIssues]
  );

  useEffect(() => {
    loadIssues();
  }, []);

  const handleFilterApply = () => {
    loadIssues(1);
  };

  const handleStatusChange = async (issueId: string, newStatus: string) => {
    await updateIssue(issueId, { status: newStatus });
  };

  const handleSeverityChange = async (issueId: string, newSeverity: string) => {
    await updateIssue(issueId, { severity: newSeverity });
  };

  const handleNotesBlur = async (issueId: string, notes: string, originalNotes: string | null) => {
    if (notes !== (originalNotes || '')) {
      await updateIssue(issueId, { notes });
    }
  };

  return (
    <>
      {/* Filters */}
      <div className="flex gap-3 items-end flex-wrap">
        <div>
          <label className="label">Project</label>
          <input
            type="text"
            value={projectFilter}
            onChange={(e) => setProjectFilter(e.target.value)}
            placeholder="All projects"
            className="input w-44"
          />
        </div>
        <div>
          <label className="label">Status</label>
          <select
            value={statusFilter}
            onChange={(e) => setStatusFilter(e.target.value)}
            className="input w-40"
          >
            <option value="">All</option>
            <option value="open">Open</option>
            <option value="acknowledged">Acknowledged</option>
            <option value="resolved">Resolved</option>
            <option value="wontfix">Won't Fix</option>
          </select>
        </div>
        <div>
          <label className="label">Severity</label>
          <select
            value={severityFilter}
            onChange={(e) => setSeverityFilter(e.target.value)}
            className="input w-36"
          >
            <option value="">All</option>
            <option value="critical">Critical</option>
            <option value="high">High</option>
            <option value="medium">Medium</option>
            <option value="low">Low</option>
          </select>
        </div>
        <button onClick={handleFilterApply} className="btn btn-primary">
          Filter
        </button>
      </div>

      {/* Issues Table */}
      {issuesLoading ? (
        <div className="card p-8 text-center">
          <p className="text-text-secondary">Loading issues...</p>
        </div>
      ) : issues.length === 0 ? (
        <div className="card p-8 text-center">
          <p className="text-text-secondary">No issues found</p>
        </div>
      ) : (
        <div className="card overflow-hidden flex flex-col max-h-[calc(100vh-340px)]">
          <div className="overflow-auto flex-1">
            <table>
              <thead className="sticky top-0 bg-surface z-10">
                <tr>
                  <th>Project</th>
                  <th>Test Name</th>
                  <th>Title</th>
                  <th>Severity</th>
                  <th>Status</th>
                  <th>Occurrences</th>
                  <th>Last Seen</th>
                  <th>Notes</th>
                </tr>
              </thead>
              <tbody>
                {issues.map((issue) => (
                  <IssueRow
                    key={issue.id}
                    issue={issue}
                    onStatusChange={handleStatusChange}
                    onSeverityChange={handleSeverityChange}
                    onNotesBlur={handleNotesBlur}
                  />
                ))}
              </tbody>
            </table>
          </div>

          {/* Pagination */}
          {issuesTotalPages > 1 && (
            <div className="flex items-center justify-between px-4 py-3 border-t border-border">
              <span className="text-sm text-text-secondary">
                {issuesTotal} total issues
              </span>
              <div className="flex gap-2">
                <button
                  onClick={() => loadIssues(issuesPage - 1)}
                  disabled={issuesPage <= 1}
                  className="btn btn-secondary text-sm disabled:opacity-50"
                >
                  Previous
                </button>
                <span className="text-sm text-text-secondary flex items-center px-2">
                  Page {issuesPage} of {issuesTotalPages}
                </span>
                <button
                  onClick={() => loadIssues(issuesPage + 1)}
                  disabled={issuesPage >= issuesTotalPages}
                  className="btn btn-secondary text-sm disabled:opacity-50"
                >
                  Next
                </button>
              </div>
            </div>
          )}
        </div>
      )}
    </>
  );
}

function IssueRow({
  issue,
  onStatusChange,
  onSeverityChange,
  onNotesBlur,
}: {
  issue: TestIssue;
  onStatusChange: (id: string, status: string) => void;
  onSeverityChange: (id: string, severity: string) => void;
  onNotesBlur: (id: string, notes: string, originalNotes: string | null) => void;
}) {
  const [localNotes, setLocalNotes] = useState(issue.notes || '');

  useEffect(() => {
    setLocalNotes(issue.notes || '');
  }, [issue.notes]);

  return (
    <tr>
      <td className="font-medium text-text-primary text-sm">{issue.project}</td>
      <td className="text-text-secondary text-sm max-w-xs truncate" title={issue.testName}>
        {issue.testName}
      </td>
      <td className="text-text-primary text-sm max-w-xs truncate" title={issue.title}>
        {issue.title}
      </td>
      <td>
        <select
          value={issue.severity}
          onChange={(e) => onSeverityChange(issue.id, e.target.value)}
          className="text-xs rounded px-2 py-1 bg-surface border border-border text-text-primary focus:outline-none focus:ring-1 focus:ring-primary"
        >
          <option value="critical">Critical</option>
          <option value="high">High</option>
          <option value="medium">Medium</option>
          <option value="low">Low</option>
        </select>
      </td>
      <td>
        <select
          value={issue.status}
          onChange={(e) => onStatusChange(issue.id, e.target.value)}
          className="text-xs rounded px-2 py-1 bg-surface border border-border text-text-primary focus:outline-none focus:ring-1 focus:ring-primary"
        >
          <option value="open">Open</option>
          <option value="acknowledged">Acknowledged</option>
          <option value="resolved">Resolved</option>
          <option value="wontfix">Won't Fix</option>
        </select>
      </td>
      <td className="text-text-primary text-sm text-center">{issue.occurrenceCount}</td>
      <td className="text-text-secondary text-sm whitespace-nowrap">
        {new Date(issue.lastSeenAt).toLocaleString()}
      </td>
      <td>
        <input
          type="text"
          value={localNotes}
          onChange={(e) => setLocalNotes(e.target.value)}
          onBlur={() => onNotesBlur(issue.id, localNotes, issue.notes)}
          placeholder="Add notes..."
          className="text-xs w-full bg-transparent border-b border-border text-text-primary py-1 focus:outline-none focus:border-primary"
        />
      </td>
    </tr>
  );
}

// ---------------------------------------------------------------------------
// Shared Badges
// ---------------------------------------------------------------------------

function RunStatusBadge({ status }: { status: string }) {
  const styles: Record<string, string> = {
    passed: 'badge-success',
    failed: 'badge-danger',
    running: 'badge-warning',
    error: 'badge-danger',
    unknown: 'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400',
  };

  return (
    <span className={`badge ${styles[status] || styles.unknown}`}>
      {status.charAt(0).toUpperCase() + status.slice(1)}
    </span>
  );
}

function TestStatusBadge({ status }: { status: string }) {
  const styles: Record<string, string> = {
    passed: 'badge-success',
    failed: 'badge-danger',
    skipped: 'badge-warning',
    error: 'badge-danger',
  };

  return (
    <span className={`badge ${styles[status] || 'badge-info'}`}>
      {status.charAt(0).toUpperCase() + status.slice(1)}
    </span>
  );
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
  const mins = Math.floor(ms / 60000);
  const secs = Math.round((ms % 60000) / 1000);
  return `${mins}m ${secs}s`;
}

export default TestCenter;
