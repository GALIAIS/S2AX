/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{vue,js,ts,jsx,tsx}'],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        // Neutral zinc scale keeps shared Tailwind utilities free of blue-gray casts.
        gray: {
          50: '#fafafa',
          100: '#f4f4f5',
          200: '#e4e4e7',
          300: '#d4d4d8',
          400: '#a1a1aa',
          500: '#71717a',
          600: '#52525b',
          700: '#3f3f46',
          800: '#27272a',
          900: '#18181b',
          950: '#09090b'
        },
        // macOS semantic system blue
        primary: {
          50: '#f2f8ff',
          100: '#e5f1ff',
          200: '#cce4ff',
          300: '#99caff',
          400: '#66afff',
          500: '#007aff',
          600: '#006ee6',
          700: '#005bbf',
          800: '#004799',
          900: '#003671',
          950: '#001d3d'
        },
        // macOS graphite neutrals
        accent: {
          50: '#f5f5f7',
          100: '#e5e5ea',
          200: '#d1d1d6',
          300: '#aeaeb2',
          400: '#8e8e93',
          500: '#636366',
          600: '#48484a',
          700: '#3a3a3c',
          800: '#2c2c2e',
          900: '#1c1c1e',
          950: '#111113'
        },
        // macOS dark surfaces
        dark: {
          50: '#f5f5f7',
          100: '#e5e5ea',
          200: '#d1d1d6',
          300: '#b0b0b8',
          400: '#8b8b94',
          500: '#66666f',
          600: '#45454d',
          700: '#2a2a2f',
          800: '#202024',
          900: '#1c1c1f',
          950: '#151517'
        }
      },
      fontFamily: {
        sans: [
          'system-ui',
          '-apple-system',
          'BlinkMacSystemFont',
          'Segoe UI',
          'Roboto',
          'Helvetica Neue',
          'Arial',
          'PingFang SC',
          'Hiragino Sans GB',
          'Microsoft YaHei',
          'sans-serif'
        ],
        mono: ['ui-monospace', 'SFMono-Regular', 'Menlo', 'Monaco', 'Consolas', 'monospace']
      },
      boxShadow: {
        glass: '0 18px 48px rgba(0, 0, 0, 0.12)',
        'glass-sm': '0 8px 24px rgba(0, 0, 0, 0.08)',
        glow: '0 0 0 3px rgba(0, 122, 255, 0.16)',
        'glow-lg': '0 0 0 4px rgba(0, 122, 255, 0.2)',
        card: '0 1px 2px rgba(0, 0, 0, 0.04), 0 8px 24px rgba(0, 0, 0, 0.035)',
        'card-hover': '0 12px 32px rgba(0, 0, 0, 0.09)',
        'inner-glow': 'inset 0 1px 0 rgba(255, 255, 255, 0.1)'
      },
      backgroundImage: {
        'gradient-radial': 'radial-gradient(var(--tw-gradient-stops))',
        'gradient-primary': 'linear-gradient(180deg, #1687ff 0%, #0071e3 100%)',
        'gradient-dark': 'linear-gradient(180deg, #2c2c2e 0%, #1c1c1e 100%)',
        'gradient-glass':
          'linear-gradient(135deg, rgba(255,255,255,0.1) 0%, rgba(255,255,255,0.05) 100%)',
        'mesh-gradient':
          'radial-gradient(at 40% 20%, rgba(0, 122, 255, 0.10) 0px, transparent 50%), radial-gradient(at 80% 0%, rgba(175, 82, 222, 0.05) 0px, transparent 50%), radial-gradient(at 0% 50%, rgba(90, 200, 250, 0.06) 0px, transparent 50%)'
      },
      animation: {
        'fade-in': 'fadeIn 0.3s ease-out',
        'slide-up': 'slideUp 0.3s ease-out',
        'slide-down': 'slideDown 0.3s ease-out',
        'slide-in-right': 'slideInRight 0.3s ease-out',
        'scale-in': 'scaleIn 0.2s ease-out',
        'pulse-slow': 'pulse 3s cubic-bezier(0.4, 0, 0.6, 1) infinite',
        shimmer: 'shimmer 2s linear infinite',
        glow: 'glow 2s ease-in-out infinite alternate'
      },
      keyframes: {
        fadeIn: {
          '0%': { opacity: '0' },
          '100%': { opacity: '1' }
        },
        slideUp: {
          '0%': { opacity: '0', transform: 'translateY(10px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' }
        },
        slideDown: {
          '0%': { opacity: '0', transform: 'translateY(-10px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' }
        },
        slideInRight: {
          '0%': { opacity: '0', transform: 'translateX(20px)' },
          '100%': { opacity: '1', transform: 'translateX(0)' }
        },
        scaleIn: {
          '0%': { opacity: '0', transform: 'scale(0.95)' },
          '100%': { opacity: '1', transform: 'scale(1)' }
        },
        shimmer: {
          '0%': { backgroundPosition: '-200% 0' },
          '100%': { backgroundPosition: '200% 0' }
        },
        glow: {
          '0%': { boxShadow: '0 0 0 2px rgba(0, 122, 255, 0.12)' },
          '100%': { boxShadow: '0 0 0 4px rgba(0, 122, 255, 0.22)' }
        }
      },
      backdropBlur: {
        xs: '2px'
      },
      borderRadius: {
        '4xl': '2rem'
      }
    }
  },
  plugins: []
}
