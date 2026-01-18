const express = require('express');

const app = express();
app.use(express.json());

const PORT = parseInt(process.env.PORT || '8090', 10);

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
function pickBackendRoundRobin() {
  const idx = rrIndex % BACKENDS.length;
  rrIndex = (rrIndex + 1) >>> 0;
  return BACKENDS[idx];
}

app.get('/api/health', (req, res) => {
  res.json({ ok: true, backends: BACKENDS, algorithm: 'round-robin' });
});

// Forward body to chosen backend /api/query
app.post('/api/query', async (req, res) => {
  const backend = pickBackendRoundRobin();
  const url = `${backend.replace(/\/$/, '')}/api/query`;

  const timeoutMs = parseInt(process.env.FORWARD_TIMEOUT_MS || '20000', 10);
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

    // Attach chosen backend info even if upstream returns non-json
    let payload;
    try {
      payload = JSON.parse(text);
    } catch {
      payload = { raw: text };
    }

    res.send({
      ...payload,
      ChosenBackend: backend
    });
  } catch (err) {
    res.status(502).json({
      Status: 'error',
      error: err?.name === 'AbortError' ? 'Upstream timeout' : (err?.message || String(err)),
      ChosenBackend: backend
    });
  } finally {
    clearTimeout(t);
  }
});

app.listen(PORT, () => {
  console.log(`[baseline-lb] listening on http://localhost:${PORT}`);
  console.log(`[baseline-lb] algorithm=round-robin backends=${BACKENDS.join(',')}`);
});
