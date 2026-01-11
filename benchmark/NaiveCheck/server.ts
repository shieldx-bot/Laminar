import express, { Request, Response } from 'express';
import { Pool } from 'pg';

const app = express();
const port = 8081;

const pool = new Pool({
  connectionString: "postgresql://postgres:Vananh12345%40@34.177.108.132:5432/laminar?sslmode=disable",
  max: 200,
  idleTimeoutMillis: 30000,
});

app.use(express.json());

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
