import fetch from 'node-fetch';
import { payloadBuffer, metrics } from './shared.js';

export async function sendRequest(requestId, sql) {

  const payload = {
    requestId,
    sql,
  };

  payloadBuffer.set(requestId, payload);
  metrics.sent++;

  const res = await fetch('http://backend/api', {
    method: 'POST',
    body: JSON.stringify(payload),
    headers: { 'Content-Type': 'application/json' },
  });

  if (res.status === 202) {
    metrics.accepted++;
  }
}
