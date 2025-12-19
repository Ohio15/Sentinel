import React from 'react';
import { Clock, AlertTriangle, XCircle, CheckCircle, Pause } from 'lucide-react';

interface SLAIndicatorProps {
  firstResponseDueAt?: string;
  resolutionDueAt?: string;
  firstResponseAt?: string;
  resolvedAt?: string;
  slaResponseBreached?: boolean;
  slaResolutionBreached?: boolean;
  slaPausedAt?: string;
  status?: string;
  size?: 'sm' | 'md' | 'lg';
  showDetails?: boolean;
}

type SLAStatus = 'on-track' | 'at-risk' | 'breached' | 'paused' | 'met' | 'none';

interface SLAState {
  responseStatus: SLAStatus;
  resolutionStatus: SLAStatus;
  responseTimeRemaining?: number;
  resolutionTimeRemaining?: number;
}

function getTimeRemaining(dueAt: string): number {
  const now = new Date().getTime();
  const due = new Date(dueAt).getTime();
  return due - now;
}

function formatTimeRemaining(ms: number): string {
  const absMs = Math.abs(ms);
  const hours = Math.floor(absMs / (1000 * 60 * 60));
  const minutes = Math.floor((absMs % (1000 * 60 * 60)) / (1000 * 60));

  if (hours >= 24) {
    const days = Math.floor(hours / 24);
    const remainingHours = hours % 24;
    return `${days}d ${remainingHours}h`;
  }

  if (hours > 0) {
    return `${hours}h ${minutes}m`;
  }

  return `${minutes}m`;
}

function calculateSLAState(props: SLAIndicatorProps): SLAState {
  const {
    firstResponseDueAt,
    resolutionDueAt,
    firstResponseAt,
    resolvedAt,
    slaResponseBreached,
    slaResolutionBreached,
    slaPausedAt,
    status
  } = props;

  // Default state
  const state: SLAState = {
    responseStatus: 'none',
    resolutionStatus: 'none'
  };

  // If ticket is closed/resolved
  if (status === 'closed' || status === 'resolved') {
    state.responseStatus = slaResponseBreached ? 'breached' : (firstResponseAt ? 'met' : 'none');
    state.resolutionStatus = slaResolutionBreached ? 'breached' : 'met';
    return state;
  }

  // If SLA is paused
  if (slaPausedAt) {
    state.responseStatus = firstResponseAt ? 'met' : 'paused';
    state.resolutionStatus = 'paused';
    return state;
  }

  // Calculate response SLA status
  if (firstResponseAt) {
    state.responseStatus = slaResponseBreached ? 'breached' : 'met';
  } else if (firstResponseDueAt) {
    if (slaResponseBreached) {
      state.responseStatus = 'breached';
    } else {
      const timeRemaining = getTimeRemaining(firstResponseDueAt);
      state.responseTimeRemaining = timeRemaining;

      if (timeRemaining < 0) {
        state.responseStatus = 'breached';
      } else if (timeRemaining < 30 * 60 * 1000) { // Less than 30 minutes
        state.responseStatus = 'at-risk';
      } else {
        state.responseStatus = 'on-track';
      }
    }
  }

  // Calculate resolution SLA status
  if (resolutionDueAt) {
    if (slaResolutionBreached) {
      state.resolutionStatus = 'breached';
    } else {
      const timeRemaining = getTimeRemaining(resolutionDueAt);
      state.resolutionTimeRemaining = timeRemaining;

      if (timeRemaining < 0) {
        state.resolutionStatus = 'breached';
      } else if (timeRemaining < 60 * 60 * 1000) { // Less than 1 hour
        state.resolutionStatus = 'at-risk';
      } else {
        state.resolutionStatus = 'on-track';
      }
    }
  }

  return state;
}

const statusConfig: Record<SLAStatus, {
  icon: React.ElementType;
  color: string;
  bgColor: string;
  label: string;
  darkBgColor: string;
}> = {
  'on-track': {
    icon: CheckCircle,
    color: 'text-green-600 dark:text-green-400',
    bgColor: 'bg-green-100',
    darkBgColor: 'dark:bg-green-900/30',
    label: 'On Track'
  },
  'at-risk': {
    icon: AlertTriangle,
    color: 'text-yellow-600 dark:text-yellow-400',
    bgColor: 'bg-yellow-100',
    darkBgColor: 'dark:bg-yellow-900/30',
    label: 'At Risk'
  },
  'breached': {
    icon: XCircle,
    color: 'text-red-600 dark:text-red-400',
    bgColor: 'bg-red-100',
    darkBgColor: 'dark:bg-red-900/30',
    label: 'Breached'
  },
  'paused': {
    icon: Pause,
    color: 'text-gray-600 dark:text-gray-400',
    bgColor: 'bg-gray-100',
    darkBgColor: 'dark:bg-gray-700',
    label: 'Paused'
  },
  'met': {
    icon: CheckCircle,
    color: 'text-blue-600 dark:text-blue-400',
    bgColor: 'bg-blue-100',
    darkBgColor: 'dark:bg-blue-900/30',
    label: 'Met'
  },
  'none': {
    icon: Clock,
    color: 'text-gray-400 dark:text-gray-500',
    bgColor: 'bg-gray-50',
    darkBgColor: 'dark:bg-gray-800',
    label: 'No SLA'
  }
};

const sizeClasses = {
  sm: {
    container: 'px-1.5 py-0.5 text-xs gap-1',
    icon: 'w-3 h-3'
  },
  md: {
    container: 'px-2 py-1 text-sm gap-1.5',
    icon: 'w-4 h-4'
  },
  lg: {
    container: 'px-3 py-1.5 text-base gap-2',
    icon: 'w-5 h-5'
  }
};

export function SLAIndicator(props: SLAIndicatorProps) {
  const { size = 'md', showDetails = false } = props;
  const state = calculateSLAState(props);

  // Use the most severe status for the badge
  const getOverallStatus = (): SLAStatus => {
    if (state.responseStatus === 'breached' || state.resolutionStatus === 'breached') {
      return 'breached';
    }
    if (state.responseStatus === 'at-risk' || state.resolutionStatus === 'at-risk') {
      return 'at-risk';
    }
    if (state.responseStatus === 'paused' || state.resolutionStatus === 'paused') {
      return 'paused';
    }
    if (state.responseStatus === 'met' && state.resolutionStatus === 'met') {
      return 'met';
    }
    if (state.responseStatus === 'on-track' || state.resolutionStatus === 'on-track') {
      return 'on-track';
    }
    return 'none';
  };

  const overallStatus = getOverallStatus();
  const config = statusConfig[overallStatus];
  const sizeClass = sizeClasses[size];
  const Icon = config.icon;

  if (!showDetails) {
    return (
      <span
        className={`inline-flex items-center rounded-full font-medium ${config.bgColor} ${config.darkBgColor} ${config.color} ${sizeClass.container}`}
        title={config.label}
      >
        <Icon className={sizeClass.icon} />
        <span>{config.label}</span>
      </span>
    );
  }

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <Icon className={`${sizeClass.icon} ${config.color}`} />
        <span className={`font-medium ${config.color}`}>{config.label}</span>
      </div>

      <div className="grid grid-cols-2 gap-4 text-sm">
        {/* Response SLA */}
        <div className="space-y-1">
          <div className="text-gray-500 dark:text-gray-400 text-xs uppercase tracking-wide">
            First Response
          </div>
          <div className="flex items-center gap-2">
            {(() => {
              const respConfig = statusConfig[state.responseStatus];
              const RespIcon = respConfig.icon;
              return (
                <>
                  <RespIcon className={`w-4 h-4 ${respConfig.color}`} />
                  <span className={respConfig.color}>{respConfig.label}</span>
                </>
              );
            })()}
          </div>
          {state.responseTimeRemaining !== undefined && state.responseStatus !== 'met' && (
            <div className={`text-xs ${state.responseTimeRemaining < 0 ? 'text-red-500' : 'text-gray-500 dark:text-gray-400'}`}>
              {state.responseTimeRemaining < 0 ? 'Overdue by ' : ''}
              {formatTimeRemaining(state.responseTimeRemaining)}
              {state.responseTimeRemaining >= 0 ? ' remaining' : ''}
            </div>
          )}
        </div>

        {/* Resolution SLA */}
        <div className="space-y-1">
          <div className="text-gray-500 dark:text-gray-400 text-xs uppercase tracking-wide">
            Resolution
          </div>
          <div className="flex items-center gap-2">
            {(() => {
              const resConfig = statusConfig[state.resolutionStatus];
              const ResIcon = resConfig.icon;
              return (
                <>
                  <ResIcon className={`w-4 h-4 ${resConfig.color}`} />
                  <span className={resConfig.color}>{resConfig.label}</span>
                </>
              );
            })()}
          </div>
          {state.resolutionTimeRemaining !== undefined && state.resolutionStatus !== 'met' && (
            <div className={`text-xs ${state.resolutionTimeRemaining < 0 ? 'text-red-500' : 'text-gray-500 dark:text-gray-400'}`}>
              {state.resolutionTimeRemaining < 0 ? 'Overdue by ' : ''}
              {formatTimeRemaining(state.resolutionTimeRemaining)}
              {state.resolutionTimeRemaining >= 0 ? ' remaining' : ''}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

// Compact version for table cells
export function SLABadge(props: SLAIndicatorProps) {
  return <SLAIndicator {...props} size="sm" showDetails={false} />;
}

// Detailed version for ticket detail view
export function SLADetails(props: SLAIndicatorProps) {
  return <SLAIndicator {...props} size="md" showDetails={true} />;
}

export default SLAIndicator;
