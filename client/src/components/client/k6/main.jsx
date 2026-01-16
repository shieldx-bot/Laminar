import { sendRequest } from './sender.js';
import './ack-listener.js';
import { metrics } from './shared.js';
import crypto from 'crypto';

const RATE = 1000; // req/s

setInterval(() => {
  for (let i = 0; i < RATE / 10; i++) {
     const sql = "SELECT * FROM users LIMIT 1" ;
    const requestId = crypto.randomUUID();
    sendRequest(requestId , sql);
  }
}, 100);

setInterval(() => {
  console.log(metrics);
}, 1000);