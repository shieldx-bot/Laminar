const express = require('express');
const { Pool } = require('pg');
const cors = require('cors');

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

const DATABASE_URL = process.env.DATABASE_URL || "postgresql://postgres:Vananh12345@@localhost:5432/laminar?sslmode=disable";
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
