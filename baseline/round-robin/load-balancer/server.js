const express = require('express');
const cors = require('cors');

function startCpuP95Logger(label) {
  const sampleEveryMs = parseInt(process.env.CPU_SAMPLE_MS || '200', 10);
  const windowMs = parseInt(process.env.CPU_WINDOW_MS || '5000', 10);

  const bounds = [0, 1, 2, 5, 10, 20, 40, 60, 80, 100, 150, 200, 400, Number.POSITIVE_INFINITY];
  const buckets = new Array(bounds.length).fill(0);
  let max = 0;
  let samples = 0;
  let sumCpu = 0;

  let sumRss = 0;
  let maxRss = 0;

  let prevWall = process.hrtime.bigint();
  let prevCpu = process.cpuUsage();

  function bucketIndex(pct) {
    if (!(pct > 0)) return 0;
    for (let i = 1; i < bounds.length; i++) {
      if (pct <= bounds[i]) return i;
    }
    return bounds.length - 1;
  }

  setInterval(() => {
    const nowWall = process.hrtime.bigint();
    const nowCpu = process.cpuUsage();
    const dWallUs = Number((nowWall - prevWall) / 1000n);
    const dCpuUs = (nowCpu.user - prevCpu.user) + (nowCpu.system - prevCpu.system);
    prevWall = nowWall;
    prevCpu = nowCpu;

    if (dWallUs <= 0 || dCpuUs < 0) return;
    const pct = (dCpuUs / dWallUs) * 100;
    if (pct > max) max = pct;
    sumCpu += pct;
    buckets[bucketIndex(pct)]++;
    samples++;

    const rss = process.memoryUsage().rss;
    sumRss += rss;
    if (rss > maxRss) maxRss = rss;
  }, sampleEveryMs).unref();

  setInterval(() => {
    if (samples === 0) {
      console.log(`[cpu_p95] label=${label} window=${windowMs}ms samples=0`);
      return;
    }
    const target = Math.max(1, Math.floor(samples * 0.95));
    let cum = 0;
    let p95Upper = bounds[bounds.length - 2];
    for (let i = 0; i < buckets.length; i++) {
      cum += buckets[i];
      if (cum >= target) {
        p95Upper = bounds[i];
        break;
      }
    }
    const avgCpu = sumCpu / samples;
    console.log(`[cpu] label=${label} window=${windowMs}ms samples=${samples} avg=${avgCpu.toFixed(1)} p95<=${Math.round(p95Upper)} max=${max.toFixed(1)}`);

    const avgRssMb = (sumRss / samples) / 1024 / 1024;
    const maxRssMb = maxRss / 1024 / 1024;
    console.log(`[ram] label=${label} window=${windowMs}ms samples=${samples} avg_mb=${avgRssMb.toFixed(1)} max_mb=${maxRssMb.toFixed(1)}`);

    buckets.fill(0);
    max = 0;
    samples = 0;
    sumCpu = 0;
    sumRss = 0;
    maxRss = 0;
  }, windowMs).unref();
}

const app = express();

process.on('unhandledRejection', (reason) => {
  console.error('[baseline-lb] unhandledRejection:', reason);
});

process.on('uncaughtException', (err) => {
  console.error('[baseline-lb] uncaughtException:', err);
});

function asyncHandler(fn) {
  return (req, res, next) => Promise.resolve(fn(req, res, next)).catch(next);
}

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

// CPU P95 + CPU avg + RAM avg.
startCpuP95Logger(`baseline-lb:${PORT}`);

function parseBackends(raw) {
  const items = (raw || '')
    .split(',')
    .map(s => s.trim())
    .filter(Boolean)
    .map(s => (s.startsWith('http://') || s.startsWith('https://')) ? s : `http://${s}`);
  return items;
}

const BACKENDS = parseBackends('http://43.207.121.197:3000,http://54.95.111.110:3000,http://54.249.135.230:3000,http://35.247.171.94:3000,http://34.143.172.6:3000,http://34.180.73.189:3000');
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
app.post('/api/query', asyncHandler(async (req, res) => {
  const timeoutMs = parseInt(process.env.FORWARD_TIMEOUT_MS || '20000', 10);

  let requestBody;
  try {
    requestBody = JSON.stringify(req.body || {});
  } catch (err) {
    res.status(400).json({
      Status: 'error',
      error: 'Invalid JSON body',
      details: err?.message || String(err)
    });
    return;
  }

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
        body: requestBody,
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
}));

// Central error handler (captures async route errors)
app.use((err, req, res, next) => {
  console.error('[baseline-lb] request error:', {
    method: req.method,
    path: req.path,
    message: err?.message,
    stack: err?.stack
  });

  if (res.headersSent) return next(err);
  res.status(500).json({
    Status: 'error',
    error: err?.message || String(err)
  });
});

app.listen(PORT, () => {
  console.log(`[baseline-lb] listening on http://localhost:${PORT}`);
  console.log(`[baseline-lb] algorithm=round-robin backends=${BACKENDS.join(',')}`);
});
