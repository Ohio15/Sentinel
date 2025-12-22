import React, { useState, useEffect, useRef } from 'react';
import { useNavigate } from 'react-router-dom';
import FullCalendar from '@fullcalendar/react';
import dayGridPlugin from '@fullcalendar/daygrid';
import timeGridPlugin from '@fullcalendar/timegrid';
import interactionPlugin from '@fullcalendar/interaction';
import { EventClickArg, EventContentArg, EventInput } from '@fullcalendar/core';
import { useTicketStore, Ticket } from '../stores/ticketStore';
import { TicketViewSwitcher } from '../components/tickets';
import { Plus, AlertTriangle, Clock, CheckCircle } from 'lucide-react';

type ViewType = 'table' | 'kanban' | 'calendar' | 'analytics';

interface TicketsCalendarProps {
  onTicketSelect?: (ticketId: string) => void;
}

// Map ticket status to colors
const statusColors: Record<string, { bg: string; border: string; text: string }> = {
  open: { bg: '#DBEAFE', border: '#3B82F6', text: '#1E40AF' },
  in_progress: { bg: '#F3E8FF', border: '#A855F7', text: '#6B21A8' },
  waiting: { bg: '#FEF3C7', border: '#F59E0B', text: '#92400E' },
  resolved: { bg: '#D1FAE5', border: '#10B981', text: '#065F46' },
  closed: { bg: '#F3F4F6', border: '#6B7280', text: '#374151' }
};

// Map priority to indicator colors
const priorityIndicators: Record<string, string> = {
  urgent: '#EF4444',
  high: '#F97316',
  medium: '#EAB308',
  low: '#9CA3AF'
};

export function TicketsCalendar({ onTicketSelect, onViewChange }: TicketsCalendarProps) {
  const navigate = useNavigate();
  const { tickets, fetchTickets, loading } = useTicketStore();
  const [view, setView] = useState<'dayGridMonth' | 'timeGridWeek' | 'timeGridDay'>('dayGridMonth');
  const [showLegend, setShowLegend] = useState(true);
  const [filterType, setFilterType] = useState<'all' | 'due' | 'created' | 'sla'>('all');
  const calendarRef = useRef<FullCalendar>(null);

  useEffect(() => {
    fetchTickets();
  }, []);

  // Convert tickets to calendar events
  const getEvents = () => {
    const events: EventInput[] = [];

    tickets.forEach((ticket) => {
      const colors = statusColors[ticket.status] || statusColors.open;

      // Created date event
      if (filterType === 'all' || filterType === 'created') {
        events.push({
          id: `${ticket.id}-created`,
          title: ticket.subject,
          start: ticket.createdAt,
          allDay: true,
          extendedProps: {
            ticket,
            eventType: 'created'
          },
          backgroundColor: colors.bg,
          borderColor: colors.border,
          textColor: colors.text
        });
      }

      // Resolution due date event (SLA)
      if ((filterType === 'all' || filterType === 'sla' || filterType === 'due') && ticket.resolutionDueAt) {
        const isDue = new Date(ticket.resolutionDueAt) < new Date() && ticket.status !== 'resolved' && ticket.status !== 'closed';
        events.push({
          id: `${ticket.id}-due`,
          title: `DUE: ${ticket.subject}`,
          start: ticket.resolutionDueAt,
          allDay: true,
          extendedProps: {
            ticket,
            eventType: 'due',
            isOverdue: isDue
          },
          backgroundColor: isDue ? '#FEE2E2' : '#FEF3C7',
          borderColor: isDue ? '#EF4444' : '#F59E0B',
          textColor: isDue ? '#991B1B' : '#92400E'
        });
      }

      // First response due date event
      if ((filterType === 'all' || filterType === 'sla') && ticket.firstResponseDueAt && !ticket.firstResponseAt) {
        const isOverdue = new Date(ticket.firstResponseDueAt) < new Date();
        events.push({
          id: `${ticket.id}-response`,
          title: `RESPONSE: ${ticket.subject}`,
          start: ticket.firstResponseDueAt,
          allDay: true,
          extendedProps: {
            ticket,
            eventType: 'response',
            isOverdue
          },
          backgroundColor: isOverdue ? '#FEE2E2' : '#E0E7FF',
          borderColor: isOverdue ? '#EF4444' : '#6366F1',
          textColor: isOverdue ? '#991B1B' : '#3730A3'
        });
      }
    });

    return events;
  };

  const handleEventClick = (info: EventClickArg) => {
    const ticket = info.event.extendedProps.ticket as Ticket;
    if (onTicketSelect) {
      onTicketSelect(ticket.id);
    } else {
      navigate(`/tickets/${ticket.id}`);
    }
  };

  // Custom event content renderer
  const renderEventContent = (eventInfo: EventContentArg) => {
    const ticket = eventInfo.event.extendedProps.ticket as Ticket;
    const eventType = eventInfo.event.extendedProps.eventType;
    const isOverdue = eventInfo.event.extendedProps.isOverdue;

    return (
      <div className="flex items-center gap-1 px-1 py-0.5 overflow-hidden w-full">
        {/* Priority indicator */}
        <span
          className="w-2 h-2 rounded-full flex-shrink-0"
          style={{ backgroundColor: priorityIndicators[ticket.priority] || '#9CA3AF' }}
        />

        {/* Event type icon */}
        {eventType === 'due' && (
          <Clock className={`w-3 h-3 flex-shrink-0 ${isOverdue ? 'text-red-600' : 'text-yellow-600'}`} />
        )}
        {eventType === 'response' && (
          <AlertTriangle className={`w-3 h-3 flex-shrink-0 ${isOverdue ? 'text-red-600' : 'text-indigo-600'}`} />
        )}
        {eventType === 'created' && ticket.status === 'resolved' && (
          <CheckCircle className="w-3 h-3 flex-shrink-0 text-green-600" />
        )}

        {/* Title */}
        <span className="text-xs truncate">{eventInfo.event.title}</span>

        {/* Ticket number */}
        <span className="text-xs opacity-60 flex-shrink-0 hidden sm:inline">
          #{ticket.ticketNumber}
        </span>
      </div>
    );
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center h-full">
        <p className="text-text-secondary">Loading tickets...</p>
      </div>
    );
  }

  return (
    <div className="space-y-6 h-full">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-4">
          <h1 className="text-2xl font-bold text-text-primary">Tickets</h1>
          <TicketViewSwitcher useRouting={false} currentView="calendar" onChange={onViewChange} />
        </div>
        <div className="flex items-center gap-3">
          {/* Filter */}
          <select
            value={filterType}
            onChange={(e) => setFilterType(e.target.value as any)}
            className="input w-auto text-sm"
          >
            <option value="all">All Events</option>
            <option value="created">Created Dates</option>
            <option value="due">Due Dates</option>
            <option value="sla">SLA Deadlines</option>
          </select>

          {/* View Selector */}
          <div className="flex items-center border border-gray-200 dark:border-gray-700 rounded-lg overflow-hidden">
            <button
              onClick={() => {
                setView('dayGridMonth');
                calendarRef.current?.getApi().changeView('dayGridMonth');
              }}
              className={`px-3 py-1.5 text-sm ${view === 'dayGridMonth' ? 'bg-blue-50 dark:bg-blue-900/30 text-blue-600' : 'text-gray-600 dark:text-gray-400 hover:bg-gray-50 dark:hover:bg-gray-800'}`}
            >
              Month
            </button>
            <button
              onClick={() => {
                setView('timeGridWeek');
                calendarRef.current?.getApi().changeView('timeGridWeek');
              }}
              className={`px-3 py-1.5 text-sm border-l border-gray-200 dark:border-gray-700 ${view === 'timeGridWeek' ? 'bg-blue-50 dark:bg-blue-900/30 text-blue-600' : 'text-gray-600 dark:text-gray-400 hover:bg-gray-50 dark:hover:bg-gray-800'}`}
            >
              Week
            </button>
            <button
              onClick={() => {
                setView('timeGridDay');
                calendarRef.current?.getApi().changeView('timeGridDay');
              }}
              className={`px-3 py-1.5 text-sm border-l border-gray-200 dark:border-gray-700 ${view === 'timeGridDay' ? 'bg-blue-50 dark:bg-blue-900/30 text-blue-600' : 'text-gray-600 dark:text-gray-400 hover:bg-gray-50 dark:hover:bg-gray-800'}`}
            >
              Day
            </button>
          </div>

          <button
            onClick={() => navigate('/tickets/new')}
            className="btn btn-primary flex items-center gap-2"
          >
            <Plus className="w-5 h-5" />
            New Ticket
          </button>
        </div>
      </div>

      {/* Legend */}
      {showLegend && (
        <div className="flex flex-wrap items-center gap-4 text-sm text-gray-600 dark:text-gray-400 bg-gray-50 dark:bg-gray-800/50 p-3 rounded-lg">
          <span className="font-medium text-gray-900 dark:text-gray-100">Legend:</span>
          <div className="flex items-center gap-4">
            <span className="flex items-center gap-1">
              <span className="w-3 h-3 rounded" style={{ backgroundColor: statusColors.open.bg, border: `1px solid ${statusColors.open.border}` }}></span>
              Open
            </span>
            <span className="flex items-center gap-1">
              <span className="w-3 h-3 rounded" style={{ backgroundColor: statusColors.in_progress.bg, border: `1px solid ${statusColors.in_progress.border}` }}></span>
              In Progress
            </span>
            <span className="flex items-center gap-1">
              <span className="w-3 h-3 rounded" style={{ backgroundColor: statusColors.waiting.bg, border: `1px solid ${statusColors.waiting.border}` }}></span>
              Waiting
            </span>
            <span className="flex items-center gap-1">
              <span className="w-3 h-3 rounded" style={{ backgroundColor: statusColors.resolved.bg, border: `1px solid ${statusColors.resolved.border}` }}></span>
              Resolved
            </span>
          </div>
          <div className="flex items-center gap-4 border-l border-gray-300 dark:border-gray-600 pl-4">
            <span className="flex items-center gap-1">
              <span className="w-2 h-2 rounded-full" style={{ backgroundColor: priorityIndicators.urgent }}></span>
              Urgent
            </span>
            <span className="flex items-center gap-1">
              <span className="w-2 h-2 rounded-full" style={{ backgroundColor: priorityIndicators.high }}></span>
              High
            </span>
            <span className="flex items-center gap-1">
              <span className="w-2 h-2 rounded-full" style={{ backgroundColor: priorityIndicators.medium }}></span>
              Medium
            </span>
            <span className="flex items-center gap-1">
              <span className="w-2 h-2 rounded-full" style={{ backgroundColor: priorityIndicators.low }}></span>
              Low
            </span>
          </div>
          <button
            onClick={() => setShowLegend(false)}
            className="ml-auto text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
          >
            Hide
          </button>
        </div>
      )}

      {/* Calendar */}
      <div className="card p-4 calendar-container" style={{ minHeight: 'calc(100vh - 280px)' }}>
        <FullCalendar
          ref={calendarRef}
          plugins={[dayGridPlugin, timeGridPlugin, interactionPlugin]}
          initialView={view}
          headerToolbar={{
            left: 'prev,next today',
            center: 'title',
            right: ''
          }}
          events={getEvents()}
          eventClick={handleEventClick}
          eventContent={renderEventContent}
          height="100%"
          dayMaxEvents={3}
          moreLinkClick="popover"
          navLinks={true}
          nowIndicator={true}
          slotMinTime="06:00:00"
          slotMaxTime="22:00:00"
          eventDisplay="block"
          eventTimeFormat={{
            hour: 'numeric',
            minute: '2-digit',
            meridiem: 'short'
          }}
        />
      </div>

      {/* Add custom styles for FullCalendar */}
      <style>{`
        .calendar-container .fc {
          font-family: inherit;
        }
        .calendar-container .fc-theme-standard td,
        .calendar-container .fc-theme-standard th {
          border-color: var(--color-border, #e5e7eb);
        }
        .calendar-container .fc-theme-standard .fc-scrollgrid {
          border-color: var(--color-border, #e5e7eb);
        }
        .calendar-container .fc-daygrid-day-number,
        .calendar-container .fc-col-header-cell-cushion {
          color: var(--color-text-primary, #1f2937);
        }
        .calendar-container .fc-day-today {
          background: rgba(59, 130, 246, 0.1) !important;
        }
        .calendar-container .fc-button-primary {
          background: var(--color-primary, #3b82f6) !important;
          border: none !important;
        }
        .calendar-container .fc-button-primary:hover {
          background: var(--color-primary-dark, #2563eb) !important;
        }
        .calendar-container .fc-event {
          cursor: pointer;
          border-radius: 4px;
          font-size: 0.75rem;
        }
        .calendar-container .fc-event:hover {
          opacity: 0.9;
        }
        .calendar-container .fc-more-link {
          color: var(--color-primary, #3b82f6);
          font-weight: 500;
        }
        .dark .calendar-container .fc-daygrid-day-number,
        .dark .calendar-container .fc-col-header-cell-cushion {
          color: #e5e7eb;
        }
        .dark .calendar-container .fc-theme-standard td,
        .dark .calendar-container .fc-theme-standard th,
        .dark .calendar-container .fc-theme-standard .fc-scrollgrid {
          border-color: #374151;
        }
      `}</style>
    </div>
  );
}

export default TicketsCalendar;
