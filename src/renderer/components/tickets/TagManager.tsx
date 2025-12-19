import React, { useState, useEffect, useRef, KeyboardEvent } from 'react';
import { X, Plus, Tag, Check } from 'lucide-react';

interface TicketTag {
  id: string;
  name: string;
  color: string;
  usageCount?: number;
}

interface TagManagerProps {
  selectedTags: TicketTag[];
  availableTags: TicketTag[];
  onChange: (tags: TicketTag[]) => void;
  onCreateTag?: (name: string) => Promise<TicketTag>;
  disabled?: boolean;
  placeholder?: string;
  maxTags?: number;
}

export function TagManager({
  selectedTags,
  availableTags,
  onChange,
  onCreateTag,
  disabled = false,
  placeholder = 'Add tags...',
  maxTags
}: TagManagerProps) {
  const [inputValue, setInputValue] = useState('');
  const [isOpen, setIsOpen] = useState(false);
  const [highlightedIndex, setHighlightedIndex] = useState(-1);
  const [isCreating, setIsCreating] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);

  // Filter available tags based on input and exclude already selected
  const selectedIds = new Set(selectedTags.map((t) => t.id));
  const filteredTags = availableTags.filter(
    (tag) =>
      !selectedIds.has(tag.id) &&
      tag.name.toLowerCase().includes(inputValue.toLowerCase())
  );

  // Check if current input matches an exact tag name
  const exactMatch = availableTags.find(
    (t) => t.name.toLowerCase() === inputValue.toLowerCase()
  );
  const canCreateNew =
    onCreateTag &&
    inputValue.trim().length > 0 &&
    !exactMatch &&
    !selectedTags.some((t) => t.name.toLowerCase() === inputValue.toLowerCase());

  // Close dropdown when clicking outside
  useEffect(() => {
    function handleClickOutside(event: MouseEvent) {
      if (containerRef.current && !containerRef.current.contains(event.target as Node)) {
        setIsOpen(false);
        setHighlightedIndex(-1);
      }
    }

    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  // Reset highlighted index when filtered tags change
  useEffect(() => {
    setHighlightedIndex(-1);
  }, [inputValue]);

  const handleInputChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setInputValue(e.target.value);
    setIsOpen(true);
  };

  const handleInputFocus = () => {
    setIsOpen(true);
  };

  const addTag = (tag: TicketTag) => {
    if (maxTags && selectedTags.length >= maxTags) return;
    onChange([...selectedTags, tag]);
    setInputValue('');
    setIsOpen(false);
    inputRef.current?.focus();
  };

  const removeTag = (tagId: string) => {
    onChange(selectedTags.filter((t) => t.id !== tagId));
    inputRef.current?.focus();
  };

  const handleCreateTag = async () => {
    if (!onCreateTag || !inputValue.trim() || isCreating) return;

    setIsCreating(true);
    try {
      const newTag = await onCreateTag(inputValue.trim());
      addTag(newTag);
    } catch (error) {
      console.error('Failed to create tag:', error);
    } finally {
      setIsCreating(false);
    }
  };

  const handleKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (disabled) return;

    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault();
        const maxIndex = filteredTags.length + (canCreateNew ? 0 : -1);
        setHighlightedIndex((prev) => Math.min(prev + 1, maxIndex));
        break;

      case 'ArrowUp':
        e.preventDefault();
        setHighlightedIndex((prev) => Math.max(prev - 1, -1));
        break;

      case 'Enter':
        e.preventDefault();
        if (highlightedIndex >= 0 && highlightedIndex < filteredTags.length) {
          addTag(filteredTags[highlightedIndex]);
        } else if (highlightedIndex === filteredTags.length && canCreateNew) {
          handleCreateTag();
        } else if (filteredTags.length === 1) {
          addTag(filteredTags[0]);
        } else if (canCreateNew && filteredTags.length === 0) {
          handleCreateTag();
        }
        break;

      case 'Backspace':
        if (inputValue === '' && selectedTags.length > 0) {
          removeTag(selectedTags[selectedTags.length - 1].id);
        }
        break;

      case 'Escape':
        setIsOpen(false);
        setHighlightedIndex(-1);
        break;
    }
  };

  const atMaxTags = maxTags !== undefined && selectedTags.length >= maxTags;

  return (
    <div ref={containerRef} className="relative">
      <div
        className={`
          flex flex-wrap items-center gap-1.5 p-2 rounded-lg border
          bg-white dark:bg-gray-800
          border-gray-300 dark:border-gray-600
          focus-within:ring-2 focus-within:ring-blue-500 focus-within:border-blue-500
          ${disabled ? 'opacity-50 cursor-not-allowed' : ''}
        `}
      >
        {/* Selected tags */}
        {selectedTags.map((tag) => (
          <span
            key={tag.id}
            className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-sm"
            style={{
              backgroundColor: tag.color + '20',
              color: tag.color
            }}
          >
            <span>{tag.name}</span>
            {!disabled && (
              <button
                type="button"
                onClick={() => removeTag(tag.id)}
                className="hover:bg-black/10 rounded-full p-0.5"
              >
                <X className="w-3 h-3" />
              </button>
            )}
          </span>
        ))}

        {/* Input field */}
        {!atMaxTags && (
          <input
            ref={inputRef}
            type="text"
            value={inputValue}
            onChange={handleInputChange}
            onFocus={handleInputFocus}
            onKeyDown={handleKeyDown}
            disabled={disabled}
            placeholder={selectedTags.length === 0 ? placeholder : ''}
            className={`
              flex-1 min-w-[120px] bg-transparent border-none outline-none
              text-sm text-gray-900 dark:text-gray-100
              placeholder-gray-400 dark:placeholder-gray-500
              disabled:cursor-not-allowed
            `}
          />
        )}
      </div>

      {/* Dropdown */}
      {isOpen && !disabled && (filteredTags.length > 0 || canCreateNew) && (
        <div className="absolute z-50 w-full mt-1 bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg shadow-lg max-h-48 overflow-y-auto">
          {filteredTags.map((tag, index) => (
            <button
              key={tag.id}
              type="button"
              onClick={() => addTag(tag)}
              className={`
                w-full flex items-center gap-2 px-3 py-2 text-left text-sm
                hover:bg-gray-100 dark:hover:bg-gray-700
                ${highlightedIndex === index ? 'bg-gray-100 dark:bg-gray-700' : ''}
              `}
            >
              <span
                className="w-3 h-3 rounded-full flex-shrink-0"
                style={{ backgroundColor: tag.color }}
              />
              <span className="flex-1 text-gray-900 dark:text-gray-100">{tag.name}</span>
              {tag.usageCount !== undefined && (
                <span className="text-xs text-gray-400">{tag.usageCount}</span>
              )}
            </button>
          ))}

          {canCreateNew && (
            <button
              type="button"
              onClick={handleCreateTag}
              disabled={isCreating}
              className={`
                w-full flex items-center gap-2 px-3 py-2 text-left text-sm
                hover:bg-gray-100 dark:hover:bg-gray-700 border-t border-gray-200 dark:border-gray-700
                ${highlightedIndex === filteredTags.length ? 'bg-gray-100 dark:bg-gray-700' : ''}
              `}
            >
              <Plus className="w-4 h-4 text-blue-500" />
              <span className="text-blue-600 dark:text-blue-400">
                {isCreating ? 'Creating...' : `Create "${inputValue}"`}
              </span>
            </button>
          )}
        </div>
      )}
    </div>
  );
}

// Display-only tag list
interface TagListProps {
  tags: TicketTag[];
  size?: 'sm' | 'md';
  max?: number;
}

export function TagList({ tags, size = 'md', max }: TagListProps) {
  const displayTags = max ? tags.slice(0, max) : tags;
  const remaining = max && tags.length > max ? tags.length - max : 0;

  if (tags.length === 0) {
    return (
      <span className={`text-gray-400 dark:text-gray-500 ${size === 'sm' ? 'text-xs' : 'text-sm'}`}>
        No tags
      </span>
    );
  }

  return (
    <div className="flex flex-wrap items-center gap-1">
      {displayTags.map((tag) => (
        <span
          key={tag.id}
          className={`
            inline-flex items-center gap-1 rounded-full
            ${size === 'sm' ? 'px-1.5 py-0.5 text-xs' : 'px-2 py-0.5 text-sm'}
          `}
          style={{
            backgroundColor: tag.color + '20',
            color: tag.color
          }}
        >
          <Tag className={size === 'sm' ? 'w-2.5 h-2.5' : 'w-3 h-3'} />
          <span>{tag.name}</span>
        </span>
      ))}
      {remaining > 0 && (
        <span
          className={`
            text-gray-500 dark:text-gray-400
            ${size === 'sm' ? 'text-xs' : 'text-sm'}
          `}
        >
          +{remaining} more
        </span>
      )}
    </div>
  );
}

// Single tag badge
interface TagBadgeProps {
  tag: TicketTag;
  size?: 'sm' | 'md';
  onRemove?: () => void;
}

export function TagBadge({ tag, size = 'md', onRemove }: TagBadgeProps) {
  return (
    <span
      className={`
        inline-flex items-center gap-1 rounded-full
        ${size === 'sm' ? 'px-1.5 py-0.5 text-xs' : 'px-2 py-0.5 text-sm'}
      `}
      style={{
        backgroundColor: tag.color + '20',
        color: tag.color
      }}
    >
      <span>{tag.name}</span>
      {onRemove && (
        <button
          type="button"
          onClick={onRemove}
          className="hover:bg-black/10 rounded-full p-0.5"
        >
          <X className={size === 'sm' ? 'w-2.5 h-2.5' : 'w-3 h-3'} />
        </button>
      )}
    </span>
  );
}

export default TagManager;
