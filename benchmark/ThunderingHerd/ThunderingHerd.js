import http from 'k6/http';

export const options = {
  scenarios: {
    thundering_herd: {
      executor: 'per-vu-iterations',
      vus: 10000,        // 10.000 users
      iterations: 1,    // mỗi user 1 request
      maxDuration: '30s',
    },
  },
};

export default function () {
  const payload = JSON.stringify({
    "QueryId": "1234",
    "QuerySQL": "SELECT id, username, email, password_hash, balance, is_active, created_at, updated_at FROM users LIMIT 1"
  });

  const params = {
    headers: {
      'Content-Type': 'application/json',
    },
  };

  const res = http.post('http://localhost:3000/api/http3', payload, params); 
  
  if (res.status !== 200) {
    console.error(`Error: Status ${res.status}. Body: ${res.body}`);
  }
}  