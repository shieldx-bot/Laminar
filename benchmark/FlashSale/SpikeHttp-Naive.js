import http from 'k6/http';
import { check } from 'k6';

export const options = {
  scenarios: {
    traffic_spike: {
      executor: 'ramping-arrival-rate',
      startRate: 1000,
      timeUnit: '1s',
      preAllocatedVUs: 1000, 
      maxVUs: 10000, 
      stages: [
        { target: 1000, duration: '5s' },   // Ổn định
        { target: 50000, duration: '1s' },  // Tăng sốc lên 50k RPS
        { target: 50000, duration: '10s' }, 
        { target: 0, duration: '5s' },      
      ],
    },
  },
  discardResponseBodies: true, 
};

export default function () {
  // Payload cho Naive Server (giả định)
  const payload = JSON.stringify({
    "QueryId": "spike_naive",
    "QuerySQL": "SELECT id FROM users LIMIT 1"
  });

  const params = {
    headers: { 'Content-Type': 'application/json' },
  };

  // Gọi vào Naive Server (Post port 3001)
  const res = http.post('http://localhost:3001/api/naive', payload, params);

  check(res, {
    'status is 200': (r) => r.status === 200,
  });
}
