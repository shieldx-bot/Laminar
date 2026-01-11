import http from 'k6/http';
import { check } from 'k6';
import { randomIntBetween } from 'https://jslib.k6.io/k6-utils/1.2.0/index.js';

export const options = {
  discardResponseBodies: true,
  scenarios: {
    random_query: {
      executor: 'constant-vus',
      vus: 10000,
      duration: '30s',
    },
  },
};

export default function () {
  const randomId = randomIntBetween(1, 1000);
  const payload = JSON.stringify({
    "QueryId": "req_" + randomId,
    "QuerySQL": "SELECT id, username, email, password_hash, balance, is_active, created_at, updated_at FROM users WHERE id = " + randomId
  });

  const params = {
    headers: { 'Content-Type': 'application/json' },
  };

  const res = http.post('http://localhost:3001/api/naive', payload, params);

  check(res, {
    'status is 200': (r) => r.status === 200,
  });
}
