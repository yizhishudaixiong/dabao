/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{js,ts,jsx,tsx}'],
  theme: {
    extend: {
      colors: {
        base: {
          DEFAULT: '#f4f5f8',
          deep: '#eceef2',
        },
        surface: {
          DEFAULT: '#ffffff',
          soft: '#f7f8fb',
          raised: '#eef1f6',
        },
        line: {
          DEFAULT: '#e2e6ee',
          soft: '#eceff5',
        },
        ink: {
          DEFAULT: '#1c2333',
          soft: '#4a5568',
          faint: '#8b94a6',
        },
        accent: {
          DEFAULT: '#2563eb',
          bright: '#0ea5e9',
          soft: '#3b82f6',
        },
        good: '#10b981',
        warn: '#f59e0b',
        bad: '#ef4444',
      },
      borderRadius: {
        card: '16px',
        panel: '12px',
        btn: '10px',
        field: '8px',
      },
      boxShadow: {
        card: '0 1px 2px rgba(16,24,40,0.05), 0 8px 24px -12px rgba(16,24,40,0.10)',
        hover: '0 2px 4px rgba(16,24,40,0.06), 0 12px 32px -12px rgba(16,24,40,0.16)',
        pop: '0 8px 40px -8px rgba(16,24,40,0.20)',
        glow: '0 0 0 1px rgba(37,99,235,0.22), 0 8px 24px -8px rgba(37,99,235,0.22)',
      },
      fontSize: {
        xs: ['14px', { lineHeight: '20px' }],
        sm: ['16px', { lineHeight: '24px' }],
        base: ['18px', { lineHeight: '28px' }],
        lg: ['20px', { lineHeight: '30px' }],
        xl: ['22px', { lineHeight: '32px' }],
      },
      fontFamily: {
        sans: [
          '-apple-system', 'BlinkMacSystemFont', 'Segoe UI', 'Microsoft YaHei',
          'PingFang SC', 'Helvetica Neue', 'Arial', 'sans-serif',
        ],
        mono: ['JetBrains Mono', 'Consolas', 'SF Mono', 'Menlo', 'monospace'],
      },
    },
  },
  plugins: [],
}
