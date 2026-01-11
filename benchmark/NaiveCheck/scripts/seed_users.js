const { Pool } = require('pg');

const pool = new Pool({
  // Chú ý: Password chứa ký tự đặc biệt @ cần được mã hóa là %40 nếu dùng connection string
  connectionString: "postgresql://postgres:Vananh12345%40@34.177.108.132:5432/laminar?sslmode=disable",
});

async function seed() {
  const client = await pool.connect();
  try {
    console.log("Starting seed...");

    // Kiểm tra xem bảng users có tồn tại không để tránh lỗi
    // Nếu chưa có bảng thì tạo tạm (dựa trên schema đã biết)
    await client.query(`
      CREATE TABLE IF NOT EXISTS users (
        id SERIAL PRIMARY KEY,
        username VARCHAR(255) NOT NULL,
        email VARCHAR(255) NOT NULL,
        password_hash VARCHAR(255) NOT NULL,
        balance BIGINT DEFAULT 0,
        is_active BOOLEAN DEFAULT TRUE,
        created_at TIMESTAMP DEFAULT NOW(),
        updated_at TIMESTAMP DEFAULT NOW()
      );
    `);
    
    // Begin transaction để đảm bảo toàn vẹn dữ liệu và tăng tốc độ insert
    await client.query('BEGIN');

    const totalRecords = 10000;
    
    for (let i = 1; i <= totalRecords; i++) {
        const username = `user_bench_${Date.now()}_${i}`;
        const email = `user${Date.now()}_${i}@example.com`;
        const password_hash = `hashed_password_secret_${i}`; 
        const balance = Math.floor(Math.random() * 1000000);
        const is_active = Math.random() < 0.9; // 90% active
        
        const query = `
            INSERT INTO users (username, email, password_hash, balance, is_active, created_at, updated_at) 
            VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
        `;
        
        await client.query(query, [username, email, password_hash, balance, is_active]);
        
        if (i % 100 === 0) {
            console.log(`Inserted ${i} records...`);
        }
    }

    await client.query('COMMIT');
    console.log("✅ Seeding completed successfully! 10000 records inserted.");
  } catch (e) {
    await client.query('ROLLBACK');
    console.error("❌ Seeding failed:", e);
  } finally {
    client.release();
    await pool.end();
  }
}

seed();
