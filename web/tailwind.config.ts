import type { Config } from 'tailwindcss'
import tailwindcssAnimate from 'tailwindcss-animate'

const withOpacity = (variable: string) => `rgb(var(${variable}) / <alpha-value>)`

const config: Config = {
  darkMode: ['class', '[data-theme="dark"]'],
  content: ['./src/**/*.{ts,tsx}'],
  theme: {
    container: {
      center: true,
      padding: '1.25rem',
      screens: {
        '2xl': '1440px',
      },
    },
    extend: {
      fontFamily: {
        sans: ['var(--font-sans)', 'system-ui', 'sans-serif'],
        mono: ['var(--font-mono)', 'monospace'],
      },
      colors: {
        background: withOpacity('--color-bg-root'),
        foreground: withOpacity('--color-text-primary'),
        card: {
          DEFAULT: withOpacity('--color-bg-surface'),
          foreground: withOpacity('--color-text-primary'),
        },
        popover: {
          DEFAULT: withOpacity('--color-bg-surface'),
          foreground: withOpacity('--color-text-primary'),
        },
        primary: {
          DEFAULT: withOpacity('--color-accent'),
          foreground: withOpacity('--color-accent-foreground'),
        },
        secondary: {
          DEFAULT: withOpacity('--color-bg-muted'),
          foreground: withOpacity('--color-text-secondary'),
        },
        muted: {
          DEFAULT: withOpacity('--color-bg-muted'),
          foreground: withOpacity('--color-text-tertiary'),
        },
        accent: {
          DEFAULT: withOpacity('--color-bg-hover'),
          foreground: withOpacity('--color-text-primary'),
        },
        destructive: {
          DEFAULT: withOpacity('--color-danger'),
          foreground: withOpacity('--color-danger-foreground'),
        },
        border: withOpacity('--color-border'),
        input: withOpacity('--color-border'),
        ring: withOpacity('--color-ring'),
      },
      borderRadius: {
        lg: 'var(--radius-lg)',
        md: 'var(--radius-md)',
        sm: 'var(--radius-sm)',
      },
      boxShadow: {
        panel: 'var(--shadow-panel)',
        floating: 'var(--shadow-floating)',
      },
      keyframes: {
        'accordion-down': {
          from: { height: '0' },
          to: { height: 'var(--radix-accordion-content-height)' },
        },
        'accordion-up': {
          from: { height: 'var(--radix-accordion-content-height)' },
          to: { height: '0' },
        },
        'fade-up': {
          from: { opacity: '0', transform: 'translateY(12px)' },
          to: { opacity: '1', transform: 'translateY(0)' },
        },
      },
      animation: {
        'accordion-down': 'accordion-down 0.2s ease-out',
        'accordion-up': 'accordion-up 0.2s ease-out',
        'fade-up': 'fade-up 0.35s cubic-bezier(0.16, 1, 0.3, 1)',
      },
    },
  },
  plugins: [tailwindcssAnimate],
}

export default config
