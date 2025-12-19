import React, { useState, useRef, useEffect } from 'react';
import {
  Link,
  Plus,
  X,
  ExternalLink,
  ArrowUp,
  ArrowDown,
  GitBranch,
  Copy,
  Lock,
  Unlock,
  Search,
  Loader2
} from 'lucide-react';

type LinkType = 'parent' | 'child' | 'related' | 'duplicate' | 'blocks' | 'blocked_by';

interface TicketLink {
  id: string;
  sourceTicketId: string;
  targetTicketId: string;
  linkType: LinkType;
  createdBy?: string;
  createdAt?: string;
  linkedTicket?: {
    id: string;
    ticketNumber: string;
    subject: string;
    status: string;
    priority: string;
  };
}

interface TicketLinksProps {
  ticketId: string;
  links: TicketLink[];
  onAddLink: (targetTicketId: string, linkType: LinkType) => Promise<void>;
  onRemoveLink: (linkId: string) => Promise<void>;
  onNavigateToTicket?: (ticketId: string) => void;
  searchTickets?: (query: string) => Promise<Array<{
    id: string;
    ticketNumber: string;
    subject: string;
    status: string;
  }>>;
  disabled?: boolean;
}

const linkTypeConfig: Record<LinkType, {
  label: string;
  icon: React.ElementType;
  description: string;
  color: string;
}> = {
  parent: {
    label: 'Parent',
    icon: ArrowUp,
    description: 'This ticket is a subtask of the linked ticket',
    color: 'text-purple-600 dark:text-purple-400'
  },
  child: {
    label: 'Child',
    icon: ArrowDown,
    description: 'The linked ticket is a subtask of this ticket',
    color: 'text-purple-600 dark:text-purple-400'
  },
  related: {
    label: 'Related',
    icon: GitBranch,
    description: 'These tickets are related to each other',
    color: 'text-blue-600 dark:text-blue-400'
  },
  duplicate: {
    label: 'Duplicate',
    icon: Copy,
    description: 'This ticket is a duplicate of the linked ticket',
    color: 'text-orange-600 dark:text-orange-400'
  },
  blocks: {
    label: 'Blocks',
    icon: Lock,
    description: 'This ticket blocks the linked ticket',
    color: 'text-red-600 dark:text-red-400'
  },
  blocked_by: {
    label: 'Blocked by',
    icon: Unlock,
    description: 'This ticket is blocked by the linked ticket',
    color: 'text-red-600 dark:text-red-400'
  }
};

const statusColors: Record<string, string> = {
  open: 'bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300',
  in_progress: 'bg-yellow-100 dark:bg-yellow-900/30 text-yellow-700 dark:text-yellow-300',
  waiting: 'bg-purple-100 dark:bg-purple-900/30 text-purple-700 dark:text-purple-300',
  resolved: 'bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-300',
  closed: 'bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300'
};

export function TicketLinks({
  ticketId,
  links,
  onAddLink,
  onRemoveLink,
  onNavigateToTicket,
  searchTickets,
  disabled = false
}: TicketLinksProps) {
  const [isAddingLink, setIsAddingLink] = useState(false);
  const [selectedLinkType, setSelectedLinkType] = useState<LinkType>('related');
  const [searchQuery, setSearchQuery] = useState('');
  const [searchResults, setSearchResults] = useState<Array<{
    id: string;
    ticketNumber: string;
    subject: string;
    status: string;
  }>>([]);
  const [isSearching, setIsSearching] = useState(false);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [removingLinkId, setRemovingLinkId] = useState<string | null>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);

  // Group links by type
  const groupedLinks = links.reduce((acc, link) => {
    if (!acc[link.linkType]) {
      acc[link.linkType] = [];
    }
    acc[link.linkType].push(link);
    return acc;
  }, {} as Record<LinkType, TicketLink[]>);

  // Search for tickets
  useEffect(() => {
    if (!searchQuery.trim() || !searchTickets) {
      setSearchResults([]);
      return;
    }

    const timeoutId = setTimeout(async () => {
      setIsSearching(true);
      try {
        const results = await searchTickets(searchQuery);
        // Filter out current ticket and already linked tickets
        const linkedIds = new Set(links.map((l) => l.targetTicketId));
        linkedIds.add(ticketId);
        setSearchResults(results.filter((r) => !linkedIds.has(r.id)));
      } catch (error) {
        console.error('Failed to search tickets:', error);
        setSearchResults([]);
      } finally {
        setIsSearching(false);
      }
    }, 300);

    return () => clearTimeout(timeoutId);
  }, [searchQuery, searchTickets, ticketId, links]);

  // Close add link form when clicking outside
  useEffect(() => {
    function handleClickOutside(event: MouseEvent) {
      if (containerRef.current && !containerRef.current.contains(event.target as Node)) {
        setIsAddingLink(false);
      }
    }

    if (isAddingLink) {
      document.addEventListener('mousedown', handleClickOutside);
      return () => document.removeEventListener('mousedown', handleClickOutside);
    }
  }, [isAddingLink]);

  const handleAddLink = async (targetTicketId: string) => {
    setIsSubmitting(true);
    try {
      await onAddLink(targetTicketId, selectedLinkType);
      setIsAddingLink(false);
      setSearchQuery('');
      setSearchResults([]);
    } catch (error) {
      console.error('Failed to add link:', error);
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleRemoveLink = async (linkId: string) => {
    setRemovingLinkId(linkId);
    try {
      await onRemoveLink(linkId);
    } catch (error) {
      console.error('Failed to remove link:', error);
    } finally {
      setRemovingLinkId(null);
    }
  };

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-medium text-gray-900 dark:text-gray-100 flex items-center gap-2">
          <Link className="w-4 h-4" />
          Linked Tickets
          {links.length > 0 && (
            <span className="px-1.5 py-0.5 text-xs bg-gray-100 dark:bg-gray-700 rounded-full">
              {links.length}
            </span>
          )}
        </h3>
        {!disabled && !isAddingLink && (
          <button
            type="button"
            onClick={() => {
              setIsAddingLink(true);
              setTimeout(() => searchRef.current?.focus(), 0);
            }}
            className="inline-flex items-center gap-1 px-2 py-1 text-xs text-blue-600 dark:text-blue-400 hover:bg-blue-50 dark:hover:bg-blue-900/30 rounded"
          >
            <Plus className="w-3 h-3" />
            Add Link
          </button>
        )}
      </div>

      {/* Add Link Form */}
      {isAddingLink && (
        <div
          ref={containerRef}
          className="p-3 bg-gray-50 dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 space-y-3"
        >
          {/* Link Type Selector */}
          <div className="flex flex-wrap gap-1">
            {(Object.keys(linkTypeConfig) as LinkType[]).map((type) => {
              const config = linkTypeConfig[type];
              const Icon = config.icon;
              return (
                <button
                  key={type}
                  type="button"
                  onClick={() => setSelectedLinkType(type)}
                  className={`
                    inline-flex items-center gap-1 px-2 py-1 text-xs rounded-full
                    ${selectedLinkType === type
                      ? 'bg-blue-100 dark:bg-blue-900/50 text-blue-700 dark:text-blue-300'
                      : 'bg-white dark:bg-gray-700 text-gray-600 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-600'
                    }
                  `}
                  title={config.description}
                >
                  <Icon className="w-3 h-3" />
                  {config.label}
                </button>
              );
            })}
          </div>

          {/* Search Input */}
          <div className="relative">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400" />
            <input
              ref={searchRef}
              type="text"
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder="Search tickets by number or subject..."
              className="w-full pl-9 pr-3 py-2 text-sm rounded-lg border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-blue-500"
            />
            {isSearching && (
              <Loader2 className="absolute right-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400 animate-spin" />
            )}
          </div>

          {/* Search Results */}
          {searchResults.length > 0 && (
            <div className="space-y-1 max-h-48 overflow-y-auto">
              {searchResults.map((ticket) => (
                <button
                  key={ticket.id}
                  type="button"
                  onClick={() => handleAddLink(ticket.id)}
                  disabled={isSubmitting}
                  className="w-full flex items-center gap-3 p-2 rounded hover:bg-gray-100 dark:hover:bg-gray-700 text-left disabled:opacity-50"
                >
                  <span className="text-sm font-mono text-gray-500 dark:text-gray-400">
                    #{ticket.ticketNumber}
                  </span>
                  <span className="flex-1 text-sm text-gray-900 dark:text-gray-100 truncate">
                    {ticket.subject}
                  </span>
                  <span className={`px-1.5 py-0.5 text-xs rounded ${statusColors[ticket.status] || statusColors.open}`}>
                    {ticket.status.replace('_', ' ')}
                  </span>
                </button>
              ))}
            </div>
          )}

          {/* No Results */}
          {searchQuery && !isSearching && searchResults.length === 0 && (
            <div className="text-sm text-gray-500 dark:text-gray-400 text-center py-4">
              No tickets found matching "{searchQuery}"
            </div>
          )}

          {/* Cancel Button */}
          <div className="flex justify-end">
            <button
              type="button"
              onClick={() => {
                setIsAddingLink(false);
                setSearchQuery('');
                setSearchResults([]);
              }}
              className="text-xs text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200"
            >
              Cancel
            </button>
          </div>
        </div>
      )}

      {/* Linked Tickets List */}
      {links.length === 0 && !isAddingLink ? (
        <div className="text-sm text-gray-500 dark:text-gray-400 text-center py-4 bg-gray-50 dark:bg-gray-800/50 rounded-lg">
          No linked tickets
        </div>
      ) : (
        <div className="space-y-3">
          {(Object.keys(linkTypeConfig) as LinkType[]).map((type) => {
            const typeLinks = groupedLinks[type];
            if (!typeLinks || typeLinks.length === 0) return null;

            const config = linkTypeConfig[type];
            const Icon = config.icon;

            return (
              <div key={type} className="space-y-1">
                <div className={`flex items-center gap-1 text-xs font-medium ${config.color}`}>
                  <Icon className="w-3 h-3" />
                  {config.label}
                </div>
                <div className="space-y-1">
                  {typeLinks.map((link) => (
                    <div
                      key={link.id}
                      className="flex items-center gap-2 p-2 bg-gray-50 dark:bg-gray-800 rounded-lg group"
                    >
                      <span className="text-sm font-mono text-gray-500 dark:text-gray-400">
                        #{link.linkedTicket?.ticketNumber || '???'}
                      </span>
                      <span className="flex-1 text-sm text-gray-900 dark:text-gray-100 truncate">
                        {link.linkedTicket?.subject || 'Unknown ticket'}
                      </span>
                      {link.linkedTicket && (
                        <span className={`px-1.5 py-0.5 text-xs rounded ${statusColors[link.linkedTicket.status] || statusColors.open}`}>
                          {link.linkedTicket.status.replace('_', ' ')}
                        </span>
                      )}
                      <div className="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
                        {onNavigateToTicket && link.linkedTicket && (
                          <button
                            type="button"
                            onClick={() => onNavigateToTicket(link.linkedTicket!.id)}
                            className="p-1 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 rounded"
                            title="Open ticket"
                          >
                            <ExternalLink className="w-3.5 h-3.5" />
                          </button>
                        )}
                        {!disabled && (
                          <button
                            type="button"
                            onClick={() => handleRemoveLink(link.id)}
                            disabled={removingLinkId === link.id}
                            className="p-1 text-gray-400 hover:text-red-500 rounded disabled:opacity-50"
                            title="Remove link"
                          >
                            {removingLinkId === link.id ? (
                              <Loader2 className="w-3.5 h-3.5 animate-spin" />
                            ) : (
                              <X className="w-3.5 h-3.5" />
                            )}
                          </button>
                        )}
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

// Compact link count badge
interface TicketLinksBadgeProps {
  count: number;
  onClick?: () => void;
}

export function TicketLinksBadge({ count, onClick }: TicketLinksBadgeProps) {
  if (count === 0) return null;

  return (
    <button
      type="button"
      onClick={onClick}
      className="inline-flex items-center gap-1 px-1.5 py-0.5 text-xs bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-300 rounded hover:bg-gray-200 dark:hover:bg-gray-600"
      title={`${count} linked ticket${count === 1 ? '' : 's'}`}
    >
      <Link className="w-3 h-3" />
      {count}
    </button>
  );
}

export default TicketLinks;
