import grpc from 'k6/net/grpc';
import { check, sleep } from 'k6';

const client = new grpc.Client();
// Đổi đường dẫn trỏ tới đúng file proto trong thư mục go-services
client.load(['../../go-services/api/proto'], 'laminar.proto');

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


export default () => {
  client.connect('localhost:50051', {
    plaintext: true
  });

  const payload = {
    QueryId: "1234",
    QuerySQL: "SELECT id, username, email, password_hash, balance, is_active, created_at, updated_at FROM users LIMIT 1"
  };

  const response = client.invoke('laminar.LaminarGateway/TestHTTP3', payload);

  // StatusValue=OK (object), GrpcOK=undefined -> grpc.OK không tồn tại
  // Chúng ta sẽ so sánh string "OK" của status
  
  check(response, {
    'status is OK': (r) => r && String(r.status) === 'OK',
  });

  if (response && String(response.status) !== 'OK') {
     console.error(`gRPC Error: ${response.status} - ${response.error}`);
  }

  client.close();
};
