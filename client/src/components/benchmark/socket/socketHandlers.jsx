// socket/socketHandlers.js
import { payloadBuffer, metrics } from '../shared.js';
import { getSocket } from './socketClient.js';

let buffer = '';

export function attachSocketHandlers() {
  const socket = getSocket();
  if (!socket) {
    throw new Error('Socket not connected yet');
  }

  socket.on('data', chunk => {
    buffer += chunk.toString();

    let idx;
    while ((idx = buffer.indexOf('\n')) !== -1) {
      const line = buffer.slice(0, idx);
      buffer = buffer.slice(idx + 1);

      try {
        const msg = JSON.parse(line);
        const { requestId } = msg;

        if (payloadBuffer.has(requestId)) {
          payloadBuffer.delete(requestId);
          metrics.done++;
        }
      } catch {
        console.error('Invalid ACK:', line);
      }
    }
  });

  socket.on('close', () => {
    console.log('ACK socket closed');
  });

  socket.on('error', err => {
    console.error('ACK socket error', err);
  });
}
