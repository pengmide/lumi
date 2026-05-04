export function ThemeScript() {
  const script = `
    (() => {
      try {
        const saved = localStorage.getItem('acp-theme');
        const theme = saved || (window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark');
        document.documentElement.setAttribute('data-theme', theme);
      } catch {
        document.documentElement.setAttribute('data-theme', 'dark');
      }
    })();
  `

  return <script dangerouslySetInnerHTML={{ __html: script }} />
}
