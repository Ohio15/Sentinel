import { ReactNode, CSSProperties } from 'react';

interface BadgeProps {
  children: ReactNode;
  variant?: 'default' | 'success' | 'warning' | 'danger' | 'info';
  size?: 'sm' | 'md';
  className?: string;
}

export function Badge({ children, variant = 'default', size = 'sm', className = '' }: BadgeProps) {
  const variantStyles: Record<string, CSSProperties> = {
    default: {
      backgroundColor: 'var(--hover-bg)',
      color: 'var(--text-secondary)',
    },
    success: {
      backgroundColor: 'var(--status-success-bg)',
      color: 'var(--status-success-text)',
    },
    warning: {
      backgroundColor: 'var(--status-warning-bg)',
      color: 'var(--status-warning-text)',
    },
    danger: {
      backgroundColor: 'var(--status-danger-bg)',
      color: 'var(--status-danger-text)',
    },
    info: {
      backgroundColor: 'var(--primary-light)',
      color: 'var(--primary-color)',
    },
  };

  const sizes = {
    sm: 'px-2 py-0.5 text-xs',
    md: 'px-2.5 py-1 text-sm',
  };

  return (
    <span
      className={`inline-flex items-center font-medium rounded-full ${sizes[size]} ${className}`}
      style={variantStyles[variant]}
    >
      {children}
    </span>
  );
}
