import React, { useState, useEffect } from 'react';
import {
  BarChart,
  Bar,
  LineChart,
  Line,
  PieChart,
  Pie,
  Cell,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
  ResponsiveContainer,
  AreaChart,
  Area
} from 'recharts';
import { TicketViewSwitcher } from '../components/tickets';
import {
  TrendingUp,
  TrendingDown,
  Clock,
  CheckCircle,
  AlertTriangle,
  Users,
  Calendar,
  RefreshCw
} from 'lucide-react';

interface AnalyticsData {
  byStatus: { status: string; count: number }[];
  byPriority: { priority: string; count: number }[];
  byType: { type: string; count: number }[];
  byCategory: { category: string; count: number }[];
  overTime: { date: string; created: number; resolved: number; closed: number }[];
  technicianStats: { name: string; assigned: number; resolved: number; avgResolutionTime: number }[];
  slaStats: {
    totalTickets: number;
    responseBreached: number;
    resolutionBreached: number;
    responseOnTime: number;
    resolutionOnTime: number;
    avgResponseTime: number;
    avgResolutionTime: number;
  };
}

const STATUS_COLORS: Record<string, string> = {
  open: '#3B82F6',
  in_progress: '#A855F7',
  waiting: '#F59E0B',
  resolved: '#10B981',
  closed: '#6B7280'
};

const PRIORITY_COLORS: Record<string, string> = {
  urgent: '#EF4444',
  high: '#F97316',
  medium: '#EAB308',
  low: '#9CA3AF'
};

const TYPE_COLORS: Record<string, string> = {
  incident: '#EF4444',
  request: '#3B82F6',
  problem: '#F59E0B',
  change: '#10B981'
};

export function TicketAnalytics() {
  const [analytics, setAnalytics] = useState<AnalyticsData | null>(null);
  const [loading, setLoading] = useState(true);
  const [dateRange, setDateRange] = useState('30');
  const [refreshing, setRefreshing] = useState(false);

  useEffect(() => {
    loadAnalytics();
  }, [dateRange]);

  const loadAnalytics = async () => {
    setLoading(true);
    try {
      const data = await window.api.analytics.tickets({
        days: parseInt(dateRange)
      });
      setAnalytics(data);
    } catch (error) {
      console.error('Failed to load analytics:', error);
    } finally {
      setLoading(false);
    }
  };

  const handleRefresh = async () => {
    setRefreshing(true);
    await loadAnalytics();
    setRefreshing(false);
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center h-full">
        <p className="text-text-secondary">Loading analytics...</p>
      </div>
    );
  }

  if (!analytics) {
    return (
      <div className="flex items-center justify-center h-full">
        <p className="text-text-secondary">Failed to load analytics data</p>
      </div>
    );
  }

  // Calculate SLA compliance rates
  const responseComplianceRate = analytics.slaStats.totalTickets > 0
    ? ((analytics.slaStats.responseOnTime / analytics.slaStats.totalTickets) * 100).toFixed(1)
    : '0';
  const resolutionComplianceRate = analytics.slaStats.totalTickets > 0
    ? ((analytics.slaStats.resolutionOnTime / analytics.slaStats.totalTickets) * 100).toFixed(1)
    : '0';

  // Format time in hours
  const formatTime = (minutes: number) => {
    if (minutes < 60) return `${Math.round(minutes)}m`;
    const hours = Math.floor(minutes / 60);
    const mins = Math.round(minutes % 60);
    return hours > 24 ? `${(hours / 24).toFixed(1)}d` : `${hours}h ${mins}m`;
  };

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-4">
          <h1 className="text-2xl font-bold text-text-primary">Tickets</h1>
          <TicketViewSwitcher />
        </div>
        <div className="flex items-center gap-3">
          <select
            value={dateRange}
            onChange={(e) => setDateRange(e.target.value)}
            className="input w-auto text-sm"
          >
            <option value="7">Last 7 days</option>
            <option value="14">Last 14 days</option>
            <option value="30">Last 30 days</option>
            <option value="90">Last 90 days</option>
          </select>
          <button
            onClick={handleRefresh}
            disabled={refreshing}
            className="btn btn-secondary flex items-center gap-2"
          >
            <RefreshCw className={`w-4 h-4 ${refreshing ? 'animate-spin' : ''}`} />
            Refresh
          </button>
        </div>
      </div>

      {/* KPI Cards */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        <KPICard
          title="Total Tickets"
          value={analytics.slaStats.totalTickets}
          icon={<Calendar className="w-5 h-5 text-blue-500" />}
          color="blue"
        />
        <KPICard
          title="Response Compliance"
          value={`${responseComplianceRate}%`}
          subtitle={`${analytics.slaStats.responseBreached} breached`}
          icon={<Clock className="w-5 h-5 text-green-500" />}
          color={parseFloat(responseComplianceRate) >= 90 ? 'green' : parseFloat(responseComplianceRate) >= 70 ? 'yellow' : 'red'}
        />
        <KPICard
          title="Resolution Compliance"
          value={`${resolutionComplianceRate}%`}
          subtitle={`${analytics.slaStats.resolutionBreached} breached`}
          icon={<CheckCircle className="w-5 h-5 text-green-500" />}
          color={parseFloat(resolutionComplianceRate) >= 90 ? 'green' : parseFloat(resolutionComplianceRate) >= 70 ? 'yellow' : 'red'}
        />
        <KPICard
          title="Avg Resolution Time"
          value={formatTime(analytics.slaStats.avgResolutionTime)}
          icon={<TrendingUp className="w-5 h-5 text-purple-500" />}
          color="purple"
        />
      </div>

      {/* Charts Row 1 */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        {/* Tickets Over Time */}
        <div className="card p-6">
          <h3 className="text-lg font-semibold text-text-primary mb-4">Tickets Over Time</h3>
          <ResponsiveContainer width="100%" height={300}>
            <AreaChart data={analytics.overTime}>
              <CartesianGrid strokeDasharray="3 3" className="opacity-30" />
              <XAxis dataKey="date" tick={{ fontSize: 12 }} />
              <YAxis tick={{ fontSize: 12 }} />
              <Tooltip
                contentStyle={{
                  backgroundColor: 'var(--color-surface)',
                  border: '1px solid var(--color-border)'
                }}
              />
              <Legend />
              <Area
                type="monotone"
                dataKey="created"
                name="Created"
                stroke="#3B82F6"
                fill="#3B82F680"
                stackId="1"
              />
              <Area
                type="monotone"
                dataKey="resolved"
                name="Resolved"
                stroke="#10B981"
                fill="#10B98180"
                stackId="2"
              />
              <Area
                type="monotone"
                dataKey="closed"
                name="Closed"
                stroke="#6B7280"
                fill="#6B728080"
                stackId="3"
              />
            </AreaChart>
          </ResponsiveContainer>
        </div>

        {/* Status Distribution */}
        <div className="card p-6">
          <h3 className="text-lg font-semibold text-text-primary mb-4">Status Distribution</h3>
          <ResponsiveContainer width="100%" height={300}>
            <PieChart>
              <Pie
                data={analytics.byStatus}
                dataKey="count"
                nameKey="status"
                cx="50%"
                cy="50%"
                outerRadius={100}
                label={({ status, percent }) => `${status} (${(percent * 100).toFixed(0)}%)`}
              >
                {analytics.byStatus.map((entry, index) => (
                  <Cell key={`cell-${index}`} fill={STATUS_COLORS[entry.status] || '#6B7280'} />
                ))}
              </Pie>
              <Tooltip />
            </PieChart>
          </ResponsiveContainer>
        </div>
      </div>

      {/* Charts Row 2 */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        {/* Priority Distribution */}
        <div className="card p-6">
          <h3 className="text-lg font-semibold text-text-primary mb-4">By Priority</h3>
          <ResponsiveContainer width="100%" height={250}>
            <BarChart data={analytics.byPriority} layout="vertical">
              <CartesianGrid strokeDasharray="3 3" className="opacity-30" />
              <XAxis type="number" tick={{ fontSize: 12 }} />
              <YAxis dataKey="priority" type="category" tick={{ fontSize: 12 }} width={60} />
              <Tooltip />
              <Bar dataKey="count" name="Tickets">
                {analytics.byPriority.map((entry, index) => (
                  <Cell key={`cell-${index}`} fill={PRIORITY_COLORS[entry.priority] || '#6B7280'} />
                ))}
              </Bar>
            </BarChart>
          </ResponsiveContainer>
        </div>

        {/* Type Distribution */}
        <div className="card p-6">
          <h3 className="text-lg font-semibold text-text-primary mb-4">By Type</h3>
          <ResponsiveContainer width="100%" height={250}>
            <BarChart data={analytics.byType} layout="vertical">
              <CartesianGrid strokeDasharray="3 3" className="opacity-30" />
              <XAxis type="number" tick={{ fontSize: 12 }} />
              <YAxis dataKey="type" type="category" tick={{ fontSize: 12 }} width={60} />
              <Tooltip />
              <Bar dataKey="count" name="Tickets">
                {analytics.byType.map((entry, index) => (
                  <Cell key={`cell-${index}`} fill={TYPE_COLORS[entry.type] || '#6B7280'} />
                ))}
              </Bar>
            </BarChart>
          </ResponsiveContainer>
        </div>

        {/* Category Distribution */}
        <div className="card p-6">
          <h3 className="text-lg font-semibold text-text-primary mb-4">By Category</h3>
          <ResponsiveContainer width="100%" height={250}>
            <PieChart>
              <Pie
                data={analytics.byCategory}
                dataKey="count"
                nameKey="category"
                cx="50%"
                cy="50%"
                innerRadius={40}
                outerRadius={80}
                label={({ category }) => category}
              >
                {analytics.byCategory.map((entry, index) => (
                  <Cell
                    key={`cell-${index}`}
                    fill={`hsl(${(index * 360) / analytics.byCategory.length}, 70%, 60%)`}
                  />
                ))}
              </Pie>
              <Tooltip />
            </PieChart>
          </ResponsiveContainer>
        </div>
      </div>

      {/* Technician Performance */}
      {analytics.technicianStats.length > 0 && (
        <div className="card p-6">
          <h3 className="text-lg font-semibold text-text-primary mb-4 flex items-center gap-2">
            <Users className="w-5 h-5" />
            Technician Performance
          </h3>
          <div className="overflow-x-auto">
            <table className="w-full">
              <thead>
                <tr className="bg-gray-50 dark:bg-gray-800">
                  <th className="text-left px-4 py-2 text-text-secondary font-medium">Technician</th>
                  <th className="text-center px-4 py-2 text-text-secondary font-medium">Assigned</th>
                  <th className="text-center px-4 py-2 text-text-secondary font-medium">Resolved</th>
                  <th className="text-center px-4 py-2 text-text-secondary font-medium">Resolution Rate</th>
                  <th className="text-center px-4 py-2 text-text-secondary font-medium">Avg Resolution Time</th>
                </tr>
              </thead>
              <tbody>
                {analytics.technicianStats.map((tech, index) => {
                  const resolutionRate = tech.assigned > 0
                    ? ((tech.resolved / tech.assigned) * 100).toFixed(1)
                    : '0';
                  return (
                    <tr key={index} className="border-t border-gray-100 dark:border-gray-700">
                      <td className="px-4 py-3 text-text-primary font-medium">{tech.name}</td>
                      <td className="px-4 py-3 text-center text-text-primary">{tech.assigned}</td>
                      <td className="px-4 py-3 text-center text-text-primary">{tech.resolved}</td>
                      <td className="px-4 py-3 text-center">
                        <span className={`
                          px-2 py-0.5 rounded-full text-sm
                          ${parseFloat(resolutionRate) >= 80 ? 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400' :
                            parseFloat(resolutionRate) >= 50 ? 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400' :
                            'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400'}
                        `}>
                          {resolutionRate}%
                        </span>
                      </td>
                      <td className="px-4 py-3 text-center text-text-secondary">
                        {formatTime(tech.avgResolutionTime)}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* SLA Summary */}
      <div className="card p-6">
        <h3 className="text-lg font-semibold text-text-primary mb-4 flex items-center gap-2">
          <AlertTriangle className="w-5 h-5" />
          SLA Summary
        </h3>
        <div className="grid grid-cols-2 md:grid-cols-4 gap-6">
          <SLAStat
            label="Response On Time"
            value={analytics.slaStats.responseOnTime}
            total={analytics.slaStats.totalTickets}
            color="green"
          />
          <SLAStat
            label="Response Breached"
            value={analytics.slaStats.responseBreached}
            total={analytics.slaStats.totalTickets}
            color="red"
          />
          <SLAStat
            label="Resolution On Time"
            value={analytics.slaStats.resolutionOnTime}
            total={analytics.slaStats.totalTickets}
            color="green"
          />
          <SLAStat
            label="Resolution Breached"
            value={analytics.slaStats.resolutionBreached}
            total={analytics.slaStats.totalTickets}
            color="red"
          />
        </div>
      </div>
    </div>
  );
}

// KPI Card Component
function KPICard({
  title,
  value,
  subtitle,
  icon,
  color
}: {
  title: string;
  value: string | number;
  subtitle?: string;
  icon: React.ReactNode;
  color: 'blue' | 'green' | 'yellow' | 'red' | 'purple';
}) {
  const colorClasses = {
    blue: 'border-l-blue-500',
    green: 'border-l-green-500',
    yellow: 'border-l-yellow-500',
    red: 'border-l-red-500',
    purple: 'border-l-purple-500'
  };

  return (
    <div className={`card p-4 border-l-4 ${colorClasses[color]}`}>
      <div className="flex items-center justify-between">
        <div>
          <p className="text-sm text-text-secondary">{title}</p>
          <p className="text-2xl font-bold text-text-primary mt-1">{value}</p>
          {subtitle && (
            <p className="text-xs text-text-secondary mt-1">{subtitle}</p>
          )}
        </div>
        <div className="opacity-50">{icon}</div>
      </div>
    </div>
  );
}

// SLA Stat Component
function SLAStat({
  label,
  value,
  total,
  color
}: {
  label: string;
  value: number;
  total: number;
  color: 'green' | 'red';
}) {
  const percentage = total > 0 ? ((value / total) * 100).toFixed(1) : '0';

  return (
    <div className="text-center">
      <p className="text-sm text-text-secondary mb-2">{label}</p>
      <p className={`text-3xl font-bold ${color === 'green' ? 'text-green-600' : 'text-red-600'}`}>
        {value}
      </p>
      <p className="text-sm text-text-secondary">
        {percentage}% of total
      </p>
      <div className="w-full bg-gray-200 dark:bg-gray-700 rounded-full h-2 mt-2">
        <div
          className={`h-2 rounded-full ${color === 'green' ? 'bg-green-500' : 'bg-red-500'}`}
          style={{ width: `${percentage}%` }}
        />
      </div>
    </div>
  );
}

export default TicketAnalytics;
