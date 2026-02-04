/** @type {import('tailwindcss').Config} */
export default {
  content: [
    "./src/renderer/**/*.{js,ts,jsx,tsx,html}",
  ],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        primary: {
          DEFAULT: 'var(--primary-color)',
          hover: 'var(--primary-hover)',
          light: 'var(--primary-light)',
        },
        background: 'var(--background-color)',
        surface: {
          DEFAULT: 'var(--surface-color)',
          alt: 'var(--surface-alt-color)',
        },
        hover: 'var(--hover-color)',
        border: 'var(--border-color)',
        'text-primary': 'var(--text-primary)',
        'text-secondary': 'var(--text-secondary)',
        danger: 'var(--danger-color)',
        success: 'var(--success-color)',
        warning: 'var(--warning-color)',
      },
    },
  },
  plugins: [],
}
