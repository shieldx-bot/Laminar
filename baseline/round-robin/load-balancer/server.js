const express = require('express');
const cors = require('cors');

const app = express();

const ALLOWED_ORIGINS = new Set([
  // React client
  'http://34.126.132.214:5173',
  // Load balancer origin (only matters if something in a browser ever calls LB from pages served on this origin)
  'http://35.186.151.245:8082'
]);

app.use(cors({
  origin: (origin, cb) => {
    if (!origin) return cb(null, true);
    return cb(null, ALLOWED_ORIGINS.has(origin));
  },
  methods: ['GET', 'POST', 'OPTIONS'],
  allowedHeaders: ['content-type']
}));
app.use(express.json());

const PORT = parseInt(process.env.PORT || '8082', 10);

function parseBackends(raw) {
  const items = (raw || '')
    .split(',')
    .map(s => s.trim())
    .filter(Boolean)
    .map(s => (s.startsWith('http://') || s.startsWith('https://')) ? s : `http://${s}`);
  return items;
}

const BACKENDS = parseBackends('http://43.207.121.197:8081,http://54.95.111.110:8081,http://54.249.135.230:8081,http://35.247.171.94:8081,http://34.143.172.6:8081,http://34.180.73.189:8081');
if (!BACKENDS.length) {
  console.error('Missing env BACKENDS (comma-separated), e.g. http://localhost:8081,http://localhost:8082');
  process.exit(1);
}

let rrIndex = 0;
function nextStartIndexRoundRobin() {
  const idx = rrIndex % BACKENDS.length;
  rrIndex = (rrIndex + 1) >>> 0;
  return idx;
}

function normalizeUpstreamBody(text) {
  try {
    return JSON.parse(text);
  } catch {
    return { raw: text };
  }
}

app.get('/api/health', (req, res) => {
  res.json({ ok: true, backends: BACKENDS, algorithm: 'round-robin' });
});

// Forward body to chosen backend /api/query
app.post('/api/query', async (req, res) => {
  const timeoutMs = parseInt(process.env.FORWARD_TIMEOUT_MS || '20000', 10);

  const startIdx = nextStartIndexRoundRobin();
  const triedBackends = [];
  let lastError;

  for (let offset = 0; offset < BACKENDS.length; offset++) {
    const backend = BACKENDS[(startIdx + offset) % BACKENDS.length];
    const url = `${backend.replace(/\/$/, '')}/api/query`;
    triedBackends.push(backend);

    const controller = new AbortController();
    const t = setTimeout(() => controller.abort(), timeoutMs);

    try {
      const upstream = await fetch(url, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify(req.body || {}),
        signal: controller.signal
      });

      const text = await upstream.text();

      res.status(upstream.status);
      res.setHeader('x-chosen-backend', backend);
      res.setHeader('content-type', upstream.headers.get('content-type') || 'application/json');
      res.send({
        ...normalizeUpstreamBody(text),
        ChosenBackend: backend
      });
      return;
    } catch (err) {
      lastError = err;
      // Try next backend on network/timeout errors.
      continue;
    } finally {
      clearTimeout(t);
    }
  }

  res.status(502).json({
    Status: 'error',
    error:
      lastError?.name === 'AbortError'
        ? `Upstream timeout after ${timeoutMs}ms`
        : (lastError?.message || String(lastError || 'Upstream error')),
    TriedBackends: triedBackends
  });
});

app.listen(PORT, () => {
  console.log(`[baseline-lb] listening on http://localhost:${PORT}`);
  console.log(`[baseline-lb] algorithm=round-robin backends=${BACKENDS.join(',')}`);
});
