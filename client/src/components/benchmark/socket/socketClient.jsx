// socket/socketClient.js
import net from 'net';

let client = null;

export function connectSocket({ host, port }) {
  if (client) return client; // singleton

  client = new net.Socket();
  client.connect(port, host, () => {
    console.log(`Connected to ACK socket ${host}:${port}`);
  });

  return client;
}

export function getSocket() {
  return client;
}
