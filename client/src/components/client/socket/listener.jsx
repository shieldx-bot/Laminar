import net from 'net';
import { payloadBuffer, metrics } from './shared.js';

const client = new net.Socket();

client.connect(9000, 'backend-host');

client.on('data', data => {
  const msg = JSON.parse(data.toString());
  const { requestId } = msg;

  if (payloadBuffer.has(requestId)) {
    payloadBuffer.delete(requestId);
    metrics.done++;
  }
});