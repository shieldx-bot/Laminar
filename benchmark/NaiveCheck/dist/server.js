"use strict";
var __importDefault = (this && this.__importDefault) || function (mod) {
    return (mod && mod.__esModule) ? mod : { "default": mod };
};
Object.defineProperty(exports, "__esModule", { value: true });
const express_1 = __importDefault(require("express"));
const pg_1 = require("pg");
const app = (0, express_1.default)();
const port = 3001;
const pool = new pg_1.Pool({
    connectionString: "postgresql://postgres:Vananh12345%40@34.177.108.132:5432/laminar?sslmode=disable",
    max: 200,
    idleTimeoutMillis: 30000,
});
app.use(express_1.default.json());
app.post('/api/naive', async (req, res) => {
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
        }
        finally {
            client.release();
        }
    }
    catch (err) {
        res.status(500).json({ error: err.message });
    }
});
app.listen(port, () => {
    console.log(`Naive server listening at http://localhost:${port}`);
});
