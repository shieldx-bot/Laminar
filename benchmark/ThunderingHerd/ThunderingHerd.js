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

  const res = http.post('http://34.177.108.132:8081/TestHTTP3', payload, params); 
  
  if (res.status !== 200) {
    console.error(`Error: Status ${res.status}. Body: ${res.body}`);
  }
}  
// cd && cd Laminar/go-services && go run ./cmd/gateway/main.go

    // HTTP   NAIVE CHECK BENCHMARK RESULTS
    // http_req_duration..............: avg=7.44ms   min=0s      med=0s      max=644.8ms p(90)=0s      p(95)=70.66ms 
    //   { expected_response:true }...: avg=126.25ms min=63.58ms med=87.97ms max=644.8ms p(90)=175.2ms p(95)=506.12ms
    // http_req_failed................: 94.10% 9410 out of 10000
    // http_reqs......................: 10000  326.781316/s

    // EXECUTION
    // iteration_duration.............: avg=29.04s   min=1.44s   med=30.01s  max=30.26s  p(90)=30.1s   p(95)=30.11s  
    // iterations.....................: 10000  326.781316/s
    // vus............................: 3311   min=3311          max=10000
    // vus_max........................: 10000  min=10000         max=10000

    // NETWORK
    // data_received..................: 261 kB 8.5 kB/s
    // data_sent......................: 168 kB 5.5 kB/s
