import grpc from 'k6/net/grpc';
import { check } from 'k6';
import { randomIntBetween } from 'https://jslib.k6.io/k6-utils/1.2.0/index.js';

const client = new grpc.Client();
client.load(['../../go-services/api/proto'], 'laminar.proto');

// Per-VU connection state (each VU has its own JS runtime)
let connected = false;

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

export default () => {
  // IMPORTANT: don't connect/close every iteration; it will create massive churn and gRPC CANCELLED errors.
  if (!connected) {
    client.connect('localhost:50051', {
      plaintext: true,
    });
    connected = true;
  }

  const randomId = randomIntBetween(1, 1000);
  const data = {
    // Make QueryId unique per request for better sharding/tracing
    QueryId: `req_${__VU}_${__ITER}_${randomId}`,
    QuerySQL: "SELECT id, username, email, password_hash, balance, is_active, created_at, updated_at FROM users WHERE id = " + randomId
  };

  const response = client.invoke('laminar.LaminarGateway/TestHTTP3', data);

  // NOTE: response.status is the gRPC status code (0 = OK).
  check(response, {
    'grpc status is OK': (r) => {
      if (!r) return false;
      if (r.status !== grpc.StatusOK) {
        console.log(`gRPC error: status=${r.status} error=${r.error || ''}`);
      }
      return r.status === grpc.StatusOK;
    },
  });

  // NOTE: response.message.status is the application-level status string from TestHTTP3Response.
  check(response, {
    'app status is True': (r) => {
      const appStatus = r?.message?.status;
      if (appStatus !== 'True') {
        console.log(`App status not True: status=${appStatus}`);
      }
      return appStatus === 'True';
    },
  });
};
