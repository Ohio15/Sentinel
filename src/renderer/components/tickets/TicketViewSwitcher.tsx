import React from 'react';
import { List, Kanban, Calendar, BarChart3 } from 'lucide-react';
import { useNavigate, useLocation } from 'react-router-dom';

type ViewType = 'table' | 'kanban' | 'calendar' | 'analytics';

interface ViewOption {
  type: ViewType;
  label: string;
  icon: React.ElementType;
  path: string;
}

const viewOptions: ViewOption[] = [
  { type: 'table', label: 'Table', icon: List, path: '/tickets' },
  { type: 'kanban', label: 'Kanban', icon: Kanban, path: '/tickets/kanban' },
  { type: 'calendar', label: 'Calendar', icon: Calendar, path: '/tickets/calendar' },
  { type: 'analytics', label: 'Analytics', icon: BarChart3, path: '/tickets/analytics' }
];

interface TicketViewSwitcherProps {
  currentView?: ViewType;
  onChange?: (view: ViewType) => void;
  useRouting?: boolean;
}

export function TicketViewSwitcher({
  currentView,
  onChange,
  useRouting = true
}: TicketViewSwitcherProps) {
  const navigate = useNavigate();
  const location = useLocation();

  // Determine current view from path if using routing
  const activeView = useRouting
    ? viewOptions.find((v) => v.path === location.pathname)?.type || 'table'
    : currentView || 'table';

  const handleViewChange = (view: ViewType) => {
    if (useRouting) {
      const option = viewOptions.find((v) => v.type === view);
      if (option) {
        navigate(option.path);
      }
    } else if (onChange) {
      onChange(view);
    }
  };

  return (
    <div className="inline-flex items-center bg-gray-100 dark:bg-gray-800 rounded-lg p-1">
      {viewOptions.map((option) => {
        const Icon = option.icon;
        const isActive = activeView === option.type;

        return (
          <button
            key={option.type}
            type="button"
            onClick={() => handleViewChange(option.type)}
            className={`
              inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md text-sm font-medium
              transition-colors duration-150
              ${isActive
                ? 'bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 shadow-sm'
                : 'text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-100'
              }
            `}
            title={option.label}
          >
            <Icon className="w-4 h-4" />
            <span className="hidden sm:inline">{option.label}</span>
          </button>
        );
      })}
    </div>
  );
}

// Compact version for smaller spaces
export function TicketViewSwitcherCompact({
  currentView,
  onChange,
  useRouting = true
}: TicketViewSwitcherProps) {
  const navigate = useNavigate();
  const location = useLocation();

  const activeView = useRouting
    ? viewOptions.find((v) => v.path === location.pathname)?.type || 'table'
    : currentView || 'table';

  const handleViewChange = (view: ViewType) => {
    if (useRouting) {
      const option = viewOptions.find((v) => v.type === view);
      if (option) {
        navigate(option.path);
      }
    } else if (onChange) {
      onChange(view);
    }
  };

  return (
    <div className="inline-flex items-center border border-gray-200 dark:border-gray-700 rounded-lg overflow-hidden">
      {viewOptions.map((option, index) => {
        const Icon = option.icon;
        const isActive = activeView === option.type;

        return (
          <button
            key={option.type}
            type="button"
            onClick={() => handleViewChange(option.type)}
            className={`
              p-2
              ${index > 0 ? 'border-l border-gray-200 dark:border-gray-700' : ''}
              ${isActive
                ? 'bg-blue-50 dark:bg-blue-900/30 text-blue-600 dark:text-blue-400'
                : 'text-gray-500 dark:text-gray-400 hover:bg-gray-50 dark:hover:bg-gray-800'
              }
            `}
            title={option.label}
          >
            <Icon className="w-4 h-4" />
          </button>
        );
      })}
    </div>
  );
}

// Dropdown version for mobile
interface TicketViewDropdownProps extends TicketViewSwitcherProps {
  className?: string;
}

export function TicketViewDropdown({
  currentView,
  onChange,
  useRouting = true,
  className = ''
}: TicketViewDropdownProps) {
  const navigate = useNavigate();
  const location = useLocation();

  const activeView = useRouting
    ? viewOptions.find((v) => v.path === location.pathname)?.type || 'table'
    : currentView || 'table';

  const activeOption = viewOptions.find((v) => v.type === activeView) || viewOptions[0];
  const Icon = activeOption.icon;

  const handleChange = (e: React.ChangeEvent<HTMLSelectElement>) => {
    const view = e.target.value as ViewType;
    if (useRouting) {
      const option = viewOptions.find((v) => v.type === view);
      if (option) {
        navigate(option.path);
      }
    } else if (onChange) {
      onChange(view);
    }
  };

  return (
    <div className={`relative ${className}`}>
      <Icon className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-500 dark:text-gray-400 pointer-events-none" />
      <select
        value={activeView}
        onChange={handleChange}
        className="appearance-none w-full pl-9 pr-8 py-2 text-sm rounded-lg border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
      >
        {viewOptions.map((option) => (
          <option key={option.type} value={option.type}>
            {option.label}
          </option>
        ))}
      </select>
      <svg
        className="absolute right-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400 pointer-events-none"
        fill="none"
        stroke="currentColor"
        viewBox="0 0 24 24"
      >
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
      </svg>
    </div>
  );
}

export default TicketViewSwitcher;
