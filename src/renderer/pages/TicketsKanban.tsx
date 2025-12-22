import React, { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  DndContext,
  DragOverlay,
  closestCorners,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
  DragStartEvent,
  DragEndEvent,
  DragOverEvent
} from '@dnd-kit/core';
import {
  SortableContext,
  sortableKeyboardCoordinates,
  verticalListSortingStrategy,
  useSortable
} from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';
import { useTicketStore, Ticket } from '../stores/ticketStore';
import { TicketViewSwitcher, SLABadge } from '../components/tickets';
import { Plus, User, Clock, AlertTriangle, MoreHorizontal } from 'lucide-react';

type TicketStatus = 'open' | 'in_progress' | 'waiting' | 'resolved' | 'closed';

interface KanbanColumn {
  id: TicketStatus;
  title: string;
  color: string;
  bgColor: string;
}

const columns: KanbanColumn[] = [
  { id: 'open', title: 'Open', color: 'text-blue-600', bgColor: 'bg-blue-100 dark:bg-blue-900/30' },
  { id: 'in_progress', title: 'In Progress', color: 'text-purple-600', bgColor: 'bg-purple-100 dark:bg-purple-900/30' },
  { id: 'waiting', title: 'Waiting', color: 'text-yellow-600', bgColor: 'bg-yellow-100 dark:bg-yellow-900/30' },
  { id: 'resolved', title: 'Resolved', color: 'text-green-600', bgColor: 'bg-green-100 dark:bg-green-900/30' },
  { id: 'closed', title: 'Closed', color: 'text-gray-600', bgColor: 'bg-gray-100 dark:bg-gray-700' }
];

const priorityColors: Record<string, string> = {
  urgent: 'border-l-red-500',
  high: 'border-l-orange-500',
  medium: 'border-l-yellow-500',
  low: 'border-l-gray-400'
};

interface TicketCardProps {
  ticket: Ticket;
  onClick: () => void;
}

function TicketCard({ ticket, onClick }: TicketCardProps) {
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging
  } = useSortable({ id: ticket.id });

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.5 : 1
  };

  const formatDate = (dateString: string) => {
    const date = new Date(dateString);
    const now = new Date();
    const diff = now.getTime() - date.getTime();
    const hours = Math.floor(diff / (1000 * 60 * 60));
    const days = Math.floor(hours / 24);

    if (hours < 1) return 'Just now';
    if (hours < 24) return `${hours}h ago`;
    if (days < 7) return `${days}d ago`;
    return date.toLocaleDateString();
  };

  return (
    <div
      ref={setNodeRef}
      style={style}
      {...attributes}
      {...listeners}
      className={`
        bg-white dark:bg-gray-800 rounded-lg shadow-sm border-l-4 p-3 cursor-grab
        hover:shadow-md transition-shadow
        ${priorityColors[ticket.priority] || 'border-l-gray-300'}
        ${isDragging ? 'shadow-lg ring-2 ring-blue-500' : ''}
      `}
      onClick={(e) => {
        // Prevent onClick when dragging
        if (!isDragging) {
          onClick();
        }
      }}
    >
      {/* Ticket Number and Priority */}
      <div className="flex items-center justify-between mb-2">
        <span className="text-xs font-mono text-gray-500 dark:text-gray-400">
          #{ticket.ticketNumber}
        </span>
        <span className={`
          text-xs px-1.5 py-0.5 rounded font-medium
          ${ticket.priority === 'urgent' ? 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400' :
            ticket.priority === 'high' ? 'bg-orange-100 text-orange-700 dark:bg-orange-900/30 dark:text-orange-400' :
            ticket.priority === 'medium' ? 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400' :
            'bg-gray-100 text-gray-600 dark:bg-gray-700 dark:text-gray-400'}
        `}>
          {ticket.priority}
        </span>
      </div>

      {/* Subject */}
      <h4 className="text-sm font-medium text-gray-900 dark:text-gray-100 line-clamp-2 mb-2">
        {ticket.subject}
      </h4>

      {/* SLA Badge */}
      {(ticket.firstResponseDueAt || ticket.resolutionDueAt) && (
        <div className="mb-2">
          <SLABadge
            firstResponseDueAt={ticket.firstResponseDueAt}
            resolutionDueAt={ticket.resolutionDueAt}
            firstResponseAt={ticket.firstResponseAt}
            resolvedAt={ticket.resolvedAt}
            slaResponseBreached={ticket.slaResponseBreached}
            slaResolutionBreached={ticket.slaResolutionBreached}
            slaPausedAt={ticket.slaPausedAt}
            status={ticket.status}
          />
        </div>
      )}

      {/* Footer */}
      <div className="flex items-center justify-between text-xs text-gray-500 dark:text-gray-400 mt-2 pt-2 border-t border-gray-100 dark:border-gray-700">
        <div className="flex items-center gap-1">
          {ticket.assignedTo ? (
            <>
              <User className="w-3 h-3" />
              <span className="truncate max-w-[80px]">{ticket.assignedTo}</span>
            </>
          ) : (
            <span className="text-gray-400 italic">Unassigned</span>
          )}
        </div>
        <div className="flex items-center gap-1">
          <Clock className="w-3 h-3" />
          <span>{formatDate(ticket.createdAt)}</span>
        </div>
      </div>
    </div>
  );
}

// Overlay card shown while dragging
function DragOverlayCard({ ticket }: { ticket: Ticket }) {
  return (
    <div
      className={`
        bg-white dark:bg-gray-800 rounded-lg shadow-xl border-l-4 p-3
        ring-2 ring-blue-500 transform scale-105
        ${priorityColors[ticket.priority] || 'border-l-gray-300'}
      `}
    >
      <div className="flex items-center justify-between mb-2">
        <span className="text-xs font-mono text-gray-500">#{ticket.ticketNumber}</span>
        <span className={`
          text-xs px-1.5 py-0.5 rounded font-medium
          ${ticket.priority === 'urgent' ? 'bg-red-100 text-red-700' :
            ticket.priority === 'high' ? 'bg-orange-100 text-orange-700' :
            ticket.priority === 'medium' ? 'bg-yellow-100 text-yellow-700' :
            'bg-gray-100 text-gray-600'}
        `}>
          {ticket.priority}
        </span>
      </div>
      <h4 className="text-sm font-medium text-gray-900 dark:text-gray-100 line-clamp-2">
        {ticket.subject}
      </h4>
    </div>
  );
}

interface KanbanColumnProps {
  column: KanbanColumn;
  tickets: Ticket[];
  onTicketClick: (ticketId: string) => void;
}

function KanbanColumnComponent({ column, tickets, onTicketClick }: KanbanColumnProps) {
  return (
    <div className="flex-shrink-0 w-72">
      {/* Column Header */}
      <div className={`rounded-t-lg px-3 py-2 ${column.bgColor}`}>
        <div className="flex items-center justify-between">
          <h3 className={`font-semibold ${column.color}`}>
            {column.title}
          </h3>
          <span className={`text-sm font-medium px-2 py-0.5 rounded-full ${column.bgColor} ${column.color}`}>
            {tickets.length}
          </span>
        </div>
      </div>

      {/* Column Content */}
      <div className="bg-gray-50 dark:bg-gray-900/50 rounded-b-lg p-2 min-h-[calc(100vh-280px)] max-h-[calc(100vh-280px)] overflow-y-auto">
        <SortableContext
          items={tickets.map((t) => t.id)}
          strategy={verticalListSortingStrategy}
        >
          <div className="space-y-2">
            {tickets.length === 0 ? (
              <div className="text-center py-8 text-gray-400 dark:text-gray-500 text-sm">
                No tickets
              </div>
            ) : (
              tickets.map((ticket) => (
                <TicketCard
                  key={ticket.id}
                  ticket={ticket}
                  onClick={() => onTicketClick(ticket.id)}
                />
              ))
            )}
          </div>
        </SortableContext>
      </div>
    </div>
  );
}

type ViewType = 'table' | 'kanban' | 'calendar' | 'analytics';

interface TicketsKanbanProps {
  onTicketSelect?: (ticketId: string) => void;
}

export function TicketsKanban({ onTicketSelect, onViewChange }: TicketsKanbanProps) {
  const navigate = useNavigate();
  const { tickets, fetchTickets, updateTicket, loading } = useTicketStore();
  const [activeTicket, setActiveTicket] = useState<Ticket | null>(null);
  const [showCreateModal, setShowCreateModal] = useState(false);

  const sensors = useSensors(
    useSensor(PointerSensor, {
      activationConstraint: {
        distance: 8 // 8px drag threshold to start dragging
      }
    }),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates
    })
  );

  useEffect(() => {
    fetchTickets();
  }, []);

  // Group tickets by status
  const ticketsByStatus = columns.reduce((acc, column) => {
    acc[column.id] = tickets.filter((t) => t.status === column.id);
    return acc;
  }, {} as Record<TicketStatus, Ticket[]>);

  const handleDragStart = (event: DragStartEvent) => {
    const ticket = tickets.find((t) => t.id === event.active.id);
    if (ticket) {
      setActiveTicket(ticket);
    }
  };

  const handleDragEnd = async (event: DragEndEvent) => {
    setActiveTicket(null);

    const { active, over } = event;
    if (!over) return;

    const ticket = tickets.find((t) => t.id === active.id);
    if (!ticket) return;

    // Find the target column
    let targetStatus: TicketStatus | null = null;

    // Check if dropped on a column
    if (columns.some((c) => c.id === over.id)) {
      targetStatus = over.id as TicketStatus;
    } else {
      // Dropped on another ticket, find its column
      const overTicket = tickets.find((t) => t.id === over.id);
      if (overTicket) {
        targetStatus = overTicket.status as TicketStatus;
      }
    }

    if (targetStatus && targetStatus !== ticket.status) {
      try {
        await updateTicket(ticket.id, {
          status: targetStatus,
          actorName: 'Admin'
        });
      } catch (error) {
        console.error('Failed to update ticket status:', error);
      }
    }
  };

  const handleTicketClick = (ticketId: string) => {
    if (onTicketSelect) {
      onTicketSelect(ticketId);
    } else {
      navigate(`/tickets/${ticketId}`);
    }
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
          <TicketViewSwitcher useRouting={false} currentView="kanban" onChange={onViewChange} />
        </div>
        <button
          onClick={() => setShowCreateModal(true)}
          className="btn btn-primary flex items-center gap-2"
        >
          <Plus className="w-5 h-5" />
          New Ticket
        </button>
      </div>

      {/* Stats Summary */}
      <div className="flex items-center gap-4 text-sm text-gray-600 dark:text-gray-400">
        <span className="font-medium text-gray-900 dark:text-gray-100">
          {tickets.length} tickets
        </span>
        <span>•</span>
        <span className="flex items-center gap-1">
          <span className="w-2 h-2 rounded-full bg-red-500"></span>
          {tickets.filter((t) => t.priority === 'urgent').length} urgent
        </span>
        <span className="flex items-center gap-1">
          <span className="w-2 h-2 rounded-full bg-orange-500"></span>
          {tickets.filter((t) => t.priority === 'high').length} high
        </span>
        <span className="flex items-center gap-1">
          <AlertTriangle className="w-4 h-4 text-red-500" />
          {tickets.filter((t) => t.slaResponseBreached || t.slaResolutionBreached).length} SLA breached
        </span>
      </div>

      {/* Kanban Board */}
      <DndContext
        sensors={sensors}
        collisionDetection={closestCorners}
        onDragStart={handleDragStart}
        onDragEnd={handleDragEnd}
      >
        <div className="flex gap-4 overflow-x-auto pb-4">
          {columns.map((column) => (
            <KanbanColumnComponent
              key={column.id}
              column={column}
              tickets={ticketsByStatus[column.id] || []}
              onTicketClick={handleTicketClick}
            />
          ))}
        </div>

        <DragOverlay>
          {activeTicket && <DragOverlayCard ticket={activeTicket} />}
        </DragOverlay>
      </DndContext>
    </div>
  );
}

export default TicketsKanban;
