const express = require('express');
const { Pool } = require('pg');
const cors = require('cors');

const app = express();
app.use(cors());
app.use(express.json());

const PORT = parseInt(process.env.PORT || '8081', 10);
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
app.post('/api/query', async (req, res) => {
  const queryId = req.body?.QueryId ?? req.body?.queryId;
  const querySQL = req.body?.QuerySQL ?? req.body?.querySQL ?? req.body?.sql;

  if (!querySQL || typeof querySQL !== 'string') {
    res.status(400).json({ error: 'Missing QuerySQL', instanceId: INSTANCE_ID, queryId });
    return;
  }

  const client = await pool.connect();
  try {
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
    client.release();
  }
});

app.listen(PORT, () => {
  console.log(`[baseline-backend] listening on http://localhost:${PORT} (instanceId=${INSTANCE_ID})`);
});
