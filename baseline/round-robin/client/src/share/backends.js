// Client-side shared backend list (for display / expected pool)
// You can override at build time with VITE_BACKENDS="http://localhost:8081,http://localhost:8082"

export function getBackendList() {
  const raw = import.meta.env.VITE_BACKENDS;
  if (!raw) return [];
  return raw
    .split(',')
    .map(s => s.trim())
    .filter(Boolean)
    .map(s => (s.startsWith('http://') || s.startsWith('https://')) ? s : `http://${s}`);
}
