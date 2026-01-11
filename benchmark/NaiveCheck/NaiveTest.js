import http from 'k6/http';
import { check } from 'k6';

export const options = {
  scenarios: {
    thundering_herd: {
      executor: 'per-vu-iterations',
      vus: 10000,        // Giảm xuống 1000 vì 10000 sẽ làm sập server nodejs thuần ngay lập tức
      iterations: 1,    
      maxDuration: '30s',
    },
  },
};

export default function () {
  const payload = JSON.stringify({
    "QueryId": "1234"
  });

  const params = {
    headers: {
      'Content-Type': 'application/json',
    },
  };

  // Gọi vào Server Naive port 3001
  const res = http.post('http://localhost:3001/api/naive', payload, params);
  
  check(res, {
    'status is 200': (r) => r.status === 200,
  });
}
