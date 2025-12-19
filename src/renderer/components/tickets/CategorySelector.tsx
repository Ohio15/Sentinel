import React, { useState, useEffect, useRef } from 'react';
import { ChevronDown, ChevronRight, Folder, FolderOpen, X, Check } from 'lucide-react';

interface Category {
  id: string;
  name: string;
  parentId: string | null;
  color: string | null;
  icon: string | null;
  isActive: boolean;
  children?: Category[];
}

interface CategorySelectorProps {
  value?: string | null;
  onChange: (categoryId: string | null) => void;
  categories: Category[];
  placeholder?: string;
  disabled?: boolean;
  showClear?: boolean;
  size?: 'sm' | 'md' | 'lg';
}

function buildCategoryTree(categories: Category[]): Category[] {
  const categoryMap = new Map<string, Category>();
  const roots: Category[] = [];

  // Create a map of all categories
  categories.forEach((cat) => {
    categoryMap.set(cat.id, { ...cat, children: [] });
  });

  // Build the tree structure
  categories.forEach((cat) => {
    const category = categoryMap.get(cat.id)!;
    if (cat.parentId && categoryMap.has(cat.parentId)) {
      const parent = categoryMap.get(cat.parentId)!;
      parent.children = parent.children || [];
      parent.children.push(category);
    } else {
      roots.push(category);
    }
  });

  return roots;
}

function findCategoryById(categories: Category[], id: string): Category | null {
  for (const cat of categories) {
    if (cat.id === id) return cat;
    if (cat.children) {
      const found = findCategoryById(cat.children, id);
      if (found) return found;
    }
  }
  return null;
}

function getCategoryPath(categories: Category[], id: string): string[] {
  const path: string[] = [];

  function findPath(cats: Category[], targetId: string): boolean {
    for (const cat of cats) {
      if (cat.id === targetId) {
        path.push(cat.name);
        return true;
      }
      if (cat.children) {
        if (findPath(cat.children, targetId)) {
          path.unshift(cat.name);
          return true;
        }
      }
    }
    return false;
  }

  findPath(categories, id);
  return path;
}

interface CategoryItemProps {
  category: Category;
  level: number;
  selectedId: string | null;
  expandedIds: Set<string>;
  onSelect: (id: string) => void;
  onToggle: (id: string) => void;
}

function CategoryItem({
  category,
  level,
  selectedId,
  expandedIds,
  onSelect,
  onToggle
}: CategoryItemProps) {
  const hasChildren = category.children && category.children.length > 0;
  const isExpanded = expandedIds.has(category.id);
  const isSelected = category.id === selectedId;

  const handleClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    onSelect(category.id);
  };

  const handleToggle = (e: React.MouseEvent) => {
    e.stopPropagation();
    onToggle(category.id);
  };

  return (
    <div>
      <div
        className={`
          flex items-center gap-2 px-3 py-2 cursor-pointer
          hover:bg-gray-100 dark:hover:bg-gray-700
          ${isSelected ? 'bg-blue-50 dark:bg-blue-900/30' : ''}
        `}
        style={{ paddingLeft: `${12 + level * 16}px` }}
        onClick={handleClick}
      >
        {hasChildren ? (
          <button
            onClick={handleToggle}
            className="p-0.5 hover:bg-gray-200 dark:hover:bg-gray-600 rounded"
          >
            {isExpanded ? (
              <ChevronDown className="w-4 h-4 text-gray-500" />
            ) : (
              <ChevronRight className="w-4 h-4 text-gray-500" />
            )}
          </button>
        ) : (
          <span className="w-5" />
        )}

        <span
          className="w-3 h-3 rounded-full flex-shrink-0"
          style={{ backgroundColor: category.color || '#6B7280' }}
        />

        <span className="flex-1 text-sm text-gray-900 dark:text-gray-100 truncate">
          {category.name}
        </span>

        {isSelected && (
          <Check className="w-4 h-4 text-blue-600 dark:text-blue-400 flex-shrink-0" />
        )}
      </div>

      {hasChildren && isExpanded && (
        <div>
          {category.children!.map((child) => (
            <CategoryItem
              key={child.id}
              category={child}
              level={level + 1}
              selectedId={selectedId}
              expandedIds={expandedIds}
              onSelect={onSelect}
              onToggle={onToggle}
            />
          ))}
        </div>
      )}
    </div>
  );
}

const sizeClasses = {
  sm: 'text-sm py-1.5 px-2.5',
  md: 'text-sm py-2 px-3',
  lg: 'text-base py-2.5 px-4'
};

export function CategorySelector({
  value,
  onChange,
  categories,
  placeholder = 'Select category',
  disabled = false,
  showClear = true,
  size = 'md'
}: CategorySelectorProps) {
  const [isOpen, setIsOpen] = useState(false);
  const [expandedIds, setExpandedIds] = useState<Set<string>>(new Set());
  const containerRef = useRef<HTMLDivElement>(null);

  const categoryTree = buildCategoryTree(categories.filter((c) => c.isActive));
  const selectedCategory = value ? findCategoryById(categoryTree, value) : null;
  const categoryPath = value ? getCategoryPath(categoryTree, value) : [];

  // Close dropdown when clicking outside
  useEffect(() => {
    function handleClickOutside(event: MouseEvent) {
      if (containerRef.current && !containerRef.current.contains(event.target as Node)) {
        setIsOpen(false);
      }
    }

    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  // Expand parent categories when a value is selected
  useEffect(() => {
    if (value) {
      const parentsToExpand = new Set<string>();

      function findParents(cats: Category[], targetId: string, parents: string[]): boolean {
        for (const cat of cats) {
          if (cat.id === targetId) {
            parents.forEach((p) => parentsToExpand.add(p));
            return true;
          }
          if (cat.children) {
            if (findParents(cat.children, targetId, [...parents, cat.id])) {
              return true;
            }
          }
        }
        return false;
      }

      findParents(categoryTree, value, []);
      if (parentsToExpand.size > 0) {
        setExpandedIds((prev) => new Set([...prev, ...parentsToExpand]));
      }
    }
  }, [value]);

  const handleSelect = (categoryId: string) => {
    onChange(categoryId);
    setIsOpen(false);
  };

  const handleToggle = (categoryId: string) => {
    setExpandedIds((prev) => {
      const next = new Set(prev);
      if (next.has(categoryId)) {
        next.delete(categoryId);
      } else {
        next.add(categoryId);
      }
      return next;
    });
  };

  const handleClear = (e: React.MouseEvent) => {
    e.stopPropagation();
    onChange(null);
  };

  return (
    <div ref={containerRef} className="relative">
      <button
        type="button"
        onClick={() => !disabled && setIsOpen(!isOpen)}
        disabled={disabled}
        className={`
          w-full flex items-center justify-between gap-2 rounded-lg border
          bg-white dark:bg-gray-800
          border-gray-300 dark:border-gray-600
          hover:border-gray-400 dark:hover:border-gray-500
          focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500
          disabled:opacity-50 disabled:cursor-not-allowed
          ${sizeClasses[size]}
        `}
      >
        <div className="flex items-center gap-2 flex-1 min-w-0">
          {selectedCategory ? (
            <>
              <span
                className="w-3 h-3 rounded-full flex-shrink-0"
                style={{ backgroundColor: selectedCategory.color || '#6B7280' }}
              />
              <span className="text-gray-900 dark:text-gray-100 truncate">
                {categoryPath.length > 1 ? categoryPath.join(' / ') : selectedCategory.name}
              </span>
            </>
          ) : (
            <span className="text-gray-500 dark:text-gray-400">{placeholder}</span>
          )}
        </div>

        <div className="flex items-center gap-1">
          {showClear && selectedCategory && !disabled && (
            <button
              type="button"
              onClick={handleClear}
              className="p-0.5 hover:bg-gray-200 dark:hover:bg-gray-600 rounded"
            >
              <X className="w-4 h-4 text-gray-400" />
            </button>
          )}
          <ChevronDown
            className={`w-4 h-4 text-gray-400 transition-transform ${isOpen ? 'rotate-180' : ''}`}
          />
        </div>
      </button>

      {isOpen && (
        <div className="absolute z-50 w-full mt-1 bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg shadow-lg max-h-64 overflow-y-auto">
          {categoryTree.length === 0 ? (
            <div className="px-3 py-4 text-sm text-gray-500 dark:text-gray-400 text-center">
              No categories available
            </div>
          ) : (
            <div className="py-1">
              {categoryTree.map((category) => (
                <CategoryItem
                  key={category.id}
                  category={category}
                  level={0}
                  selectedId={value || null}
                  expandedIds={expandedIds}
                  onSelect={handleSelect}
                  onToggle={handleToggle}
                />
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

// Simple badge display for category
interface CategoryBadgeProps {
  category: {
    name: string;
    color: string | null;
  } | null;
  size?: 'sm' | 'md';
}

export function CategoryBadge({ category, size = 'md' }: CategoryBadgeProps) {
  if (!category) {
    return (
      <span className={`text-gray-400 dark:text-gray-500 ${size === 'sm' ? 'text-xs' : 'text-sm'}`}>
        Uncategorized
      </span>
    );
  }

  return (
    <span
      className={`
        inline-flex items-center gap-1.5 rounded-full
        ${size === 'sm' ? 'px-2 py-0.5 text-xs' : 'px-2.5 py-1 text-sm'}
        bg-gray-100 dark:bg-gray-700 text-gray-800 dark:text-gray-200
      `}
    >
      <span
        className={`rounded-full ${size === 'sm' ? 'w-2 h-2' : 'w-2.5 h-2.5'}`}
        style={{ backgroundColor: category.color || '#6B7280' }}
      />
      <span>{category.name}</span>
    </span>
  );
}

export default CategorySelector;
