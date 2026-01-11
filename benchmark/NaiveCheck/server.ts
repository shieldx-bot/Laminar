import express, { Request, Response } from 'express';
import { Pool } from 'pg';

const app = express();
const port = 3001;

const pool = new Pool({
  connectionString: "postgresql://postgres:Vananh12345%40@34.177.108.132:5432/laminar?sslmode=disable",
  max: 200,
  idleTimeoutMillis: 30000,
});

app.use(express.json());

app.post('/api/naive', async (req: Request, res: Response) => {
  try {
    let query = `
     SELECT id, username, email, password_hash, balance, is_active, created_at, updated_at FROM users LIMIT 1
    `;

    if (req.body && req.body.QuerySQL) {
       query = req.body.QuerySQL;
    }

    const client = await pool.connect();
    try {
      const result = await client.query(query);
      res.json({ data: result.rows });
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
