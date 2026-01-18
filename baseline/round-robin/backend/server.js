const express = require('express');
const { Pool } = require('pg');
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
  console.error('[baseline-backend] unhandledRejection:', reason);
});

process.on('uncaughtException', (err) => {
  console.error('[baseline-backend] uncaughtException:', err);
});

function asyncHandler(fn) {
  return (req, res, next) => Promise.resolve(fn(req, res, next)).catch(next);
}
const ALLOWED_ORIGINS = new Set([
  // React client
  'http://34.126.132.214:5173',
  // Load balancer origin (only matters if browser calls backend directly)
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

const PORT = parseInt(process.env.PORT || '3000', 10);
const INSTANCE_ID = process.env.INSTANCE_ID || `backend-${PORT}`;

// CPU P95 + CPU avg + RAM avg.
startCpuP95Logger(`baseline-backend:${INSTANCE_ID}`);

const DATABASE_URL = process.env.DATABASE_URL || "postgresql://postgres:Vananh12345@@13.114.152.22:5432/laminar?sslmode=disable";
if (!DATABASE_URL) {
  // Fail fast so misconfig is obvious.
  console.error('Missing env DATABASE_URL (e.g. postgresql://user:pass@host:5432/db)');
  process.exit(1);
}

const pool = new Pool({
  connectionString: DATABASE_URL,
  max: parseInt(process.env.PG_POOL_MAX || '50', 10),
  idleTimeoutMillis: parseInt(process.env.PG_IDLE_TIMEOUT_MS || '30000', 10)
});

app.get('/api/health', (req, res) => {
  res.json({ ok: true, instanceId: INSTANCE_ID });
});

// Body: { QueryId, QuerySQL }
app.post('/api/query', asyncHandler(async (req, res) => {
  const queryId = req.body?.QueryId ?? req.body?.queryId;
  const querySQL = req.body?.QuerySQL ?? req.body?.querySQL ?? req.body?.sql;

  if (!querySQL || typeof querySQL !== 'string') {
    res.status(400).json({ error: 'Missing QuerySQL', instanceId: INSTANCE_ID, queryId });
    return;
  }

  let client;
  try {
    client = await pool.connect();
    const result = await client.query(querySQL);
    res.json({
      Status: 'success',
      QueryId: queryId,
      InstanceId: INSTANCE_ID,
      Records: result.rows,
      ReceivedSize: JSON.stringify(result.rows).length
    });
  } catch (err) {
    res.status(500).json({
      Status: 'error',
      QueryId: queryId,
      InstanceId: INSTANCE_ID,
      error: err?.message || String(err)
    });
  } finally {
    if (client) client.release();
  }
}));

// Central error handler (captures async route errors)
app.use((err, req, res, next) => {
  console.error('[baseline-backend] request error:', {
    method: req.method,
    path: req.path,
    message: err?.message,
    stack: err?.stack
  });

  if (res.headersSent) return next(err);
  res.status(500).json({
    Status: 'error',
    InstanceId: INSTANCE_ID,
    error: err?.message || String(err)
  });
});

app.listen(PORT, () => {
  console.log(`[baseline-backend] listening on http://localhost:${PORT} (instanceId=${INSTANCE_ID})`);
});
