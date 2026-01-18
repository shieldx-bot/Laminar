import express, { Request, Response } from 'express';
import { Pool } from 'pg';
import os from 'os';

const app = express();
const port = 8081;

function startCpuP95Logger(label: string) {
  const sampleEveryMs = parseInt(process.env.CPU_SAMPLE_MS || '200', 10);
  const windowMs = parseInt(process.env.CPU_WINDOW_MS || '5000', 10);

  const bounds = [0, 1, 2, 5, 10, 20, 40, 60, 80, 100, 150, 200, 400, Number.POSITIVE_INFINITY];
  const buckets = new Array<number>(bounds.length).fill(0);
  let max = 0;
  let samples = 0;
  let sumCpu = 0;

  let sumRss = 0;
  let maxRss = 0;

  let prevWall = process.hrtime.bigint();
  let prevCpu = process.cpuUsage();

  const bucketIndex = (pct: number) => {
    if (!(pct > 0)) return 0;
    for (let i = 1; i < bounds.length; i++) {
      if (pct <= bounds[i]) return i;
    }
    return bounds.length - 1;
  };

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
    console.log(`[cpu] label=${label} window=${windowMs}ms samples=${samples} avg=${avgCpu.toFixed(1)} p95<=${Math.round(p95Upper)} max=${max.toFixed(1)} cores=${os.cpus().length}`);

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

const pool = new Pool({
  connectionString: "postgresql://postgres:Vananh12345%40@34.177.108.132:5432/laminar?sslmode=disable",
  max: 200,
  idleTimeoutMillis: 30000,
});

app.use(express.json());

// CPU P95 + CPU avg + RAM avg.
startCpuP95Logger(`naive-check:${port}`);

app.get('/api/ping', async(req: Request, res: Response) => { 
  res.json({ message: 'pong' });
})

app.post('/api/naive', async (req: Request, res: Response) => {
  try {
    
    let query = req.body.QuerySQL;
    const client = await pool.connect();
    try {
      const result = await client.query(query);
      res.json({ 
        Status: 'success',
        QueryId: req.body.QueryId,
        Records: result.rows , 
        ReceivedSize: JSON.stringify(result.rows).length
      });
    } finally {
      client.release();
    }
  } catch (err: any) {
    res.status(500).json({ error: err.message });
  }
});

app.listen(port, () => {
  console.log(`Naive server listening at http://localhost:${port}`);
});
