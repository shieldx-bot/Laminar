// main.js
import { connectSocket } from './socket/socketClient.js';
import { attachSocketHandlers } from './socket/socketHandlers.js';
import { getSocketTarget } from './socket/getTarget.js';
import { sendRequest } from './sender.js';
import { metrics, payloadBuffer } from './shared.js';

// 1. lấy IP / port từ hàm khác
const target = getSocketTarget();

// 2. connect socket
connectSocket(target);

// 3. attach handlers (file khác)
attachSocketHandlers();

// 4. start load
const RATE = 1000;

setInterval(() => {
  for (let i = 0; i < RATE / 10; i++) {
    sendRequest();
  }
}, 100);

setInterval(() => {
  console.log({
    ...metrics,
    backlog: payloadBuffer.size,
  });
}, 1000);
