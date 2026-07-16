/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{vue,js,ts,jsx,tsx}'],
  theme: {
    extend: {
      colors: {
        ink: {
          950: '#070b14',
          900: '#0a101d',
          850: '#0d1526',
          800: '#111b30',
          700: '#1a2742',
          600: '#24344f',
        },
      },
      fontFamily: {
        mono: ['"JetBrains Mono"', 'ui-monospace', 'SFMono-Regular', 'Menlo', 'Consolas', 'monospace'],
      },
      boxShadow: {
        glow: '0 0 24px rgba(16, 185, 129, 0.25)',
        'glow-red': '0 0 24px rgba(239, 68, 68, 0.25)',
      },
    },
  },
  plugins: [],
}
