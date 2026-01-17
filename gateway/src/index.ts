// gateway.js
import http from 'http';
import { Server } from 'socket.io';
import Redis from 'ioredis';

const redis = new Redis();         // for get/set
const redisSub = new Redis();      // for subscribe
const httpServer = http.createServer();
const io = new Server(httpServer, { cors: { origin: '*' } });

const QUERY_KEY_PREFIX = 'query:socket:'; // query:<queryId> -> socketId
const PENDING_LIST_PREFIX = 'pending:query:'; // pending:query:<queryId> -> list

io.on('connection', (socket) => {
 
  socket.on('register', async ({ queryId }) => {
    console.log('socket registered', socket.id, queryId);
    // validate token if needed...
    if (!queryId) return;
     await redis.set(`${QUERY_KEY_PREFIX}${queryId}`, socket.id, 'EX', 60 * 60); // TTL 1h
    // if there were pending messages, deliver them:
    const pendingKey = `${PENDING_LIST_PREFIX}${queryId}`;
    let msg;
    while ((msg = await redis.lpop(pendingKey))) {
      socket.emit('job_done', JSON.parse(msg));
    }
  });

  socket.on('disconnect', async () => {
    console.log('socket disconnected', socket.id);
    // cleanup optionally for mapping: remove all queryIds that map to this socket
    // (could be optimized by storing reverse mapping or use expirations)
  });
});

// subscribe to backend events
redisSub.subscribe('query_done', (err, count) => {
  if (err) throw err;
  console.log('Subscribed to query_done');
});

redisSub.on('message', async (channel, message) => {
  console.log('Received message:', channel, message);
  if (channel !== 'query_done') return;
  const payload = JSON.parse(message); // { QueryId, Records, ... }
  // Fix: Lấy trực tiếp QueryId từ payload (do backend gửi về là PascalCase)
   const queryId =
    payload?.QueryId ??
    payload?.queryId ??
    payload?.query_id;
 
  if (!queryId) return;
  const socketId = await redis.get(`${QUERY_KEY_PREFIX}${queryId}`);
   
  if (socketId) {
    console.log('Client online, delivering message to socketId:', socketId);
    io.to(socketId).emit('job_done', payload);
  } else {
    // client offline: push to pending list to deliver later
    await redis.rpush(`${PENDING_LIST_PREFIX}${queryId}`, message);
    // optionally set TTL for pending list
    await redis.expire(`${PENDING_LIST_PREFIX}${queryId}`, 60 * 60 * 24);
  }
});

httpServer.listen(3000, () => console.log('Socket Gateway listening on 3000'));
