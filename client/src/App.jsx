import { useState, useRef, useEffect } from 'react'
import reactLogo from './assets/react.svg'
import viteLogo from '/vite.svg'
import { io } from "socket.io-client";
import './App.css'
import { shareDataServer } from './share/share';
import { callWithHedging } from './load-balancer/gRPC/main';
import { HashRing } from './load-balancer/vnode/main';



function percentile(sortedArr, p) {
  if (!sortedArr.length) return NaN;
  const idx = (p / 100) * (sortedArr.length - 1);
  const lo = Math.floor(idx);
  const hi = Math.ceil(idx);
  if (lo === hi) return sortedArr[lo];
  const w = idx - lo;
  return sortedArr[lo] + (sortedArr[hi] - sortedArr[lo]) * w;
}

function computeTailStats(samples) {
  const arr = samples.slice().sort((a, b) => a - b);
  const sum = arr.reduce((a, b) => a + b, 0);
  return {
    count: arr.length,
    avg: arr.length ? sum / arr.length : NaN,
    min: arr.length ? arr[0] : NaN,
    p10: percentile(arr, 10),
    p20: percentile(arr, 20),
    p30: percentile(arr, 30),
    p40: percentile(arr, 40),
    p50: percentile(arr, 50),
    p60: percentile(arr, 60),
    p70: percentile(arr, 70),
    p80: percentile(arr, 80),
    p90: percentile(arr, 90),
    p95: percentile(arr, 95),
    p99: percentile(arr, 99),
    max: arr.length ? arr[arr.length - 1] : NaN,
  };
}

function bumpCount(map, key, inc = 1) {
  map.set(key, (map.get(key) || 0) + inc);
}

function distributionSnapshot(map) {
  const entries = Array.from(map.entries());
  entries.sort((a, b) => b[1] - a[1]);
  const total = entries.reduce((acc, [, v]) => acc + v, 0);

  const byBackend = {};
  const ratio = {};

  for (const [k, v] of entries) {
    byBackend[k] = v;
    ratio[k] = total > 0 ? v / total : 0;
  }

  return { total, byBackend, ratio };
}

function SendTelegramMessage(message) {
  console.log("Sending Telegram message:", message);
  const botToken = '8526833134:AAEYEBakLwF5zVvDntHT-_Lnaf9eZPtft5A';
  const chatId = '-5090601314';
  const url = `https://api.telegram.org/bot${botToken}/sendMessage`;

  try {
    fetch(url, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json'
      },
      body: JSON.stringify({
        chat_id: chatId,
        text: message
      })
    })
      .then(response => response.json())
      .then(data => {
        console.log('Message sent:', data);
      })
      .catch((error) => {
        console.error('Error sending message:', error);
      });
  } catch (err) {
    console.error('Exception sending message:', err);
  }
}

function App() {
  const [count, setCount] = useState(0)
  const [socket, setSocket] = useState(null);
  const socketRef = useRef(null);

  const resultsRef = useRef([]);
  const expectedRef = useRef(0);
  const printedRef = useRef(false);
  // Keep as string to avoid React warning when input is cleared ("" -> NaN).
  const [countRequests, setCountRequests] = useState('');

  // Distribution counters (do NOT affect latency samples)
  const attemptsByBackendRef = useRef(new Map()); // how many RPC attempts were sent to each backend
  const winsByBackendRef = useRef(new Map()); // which backend won (first success) per query

  const waitForSocketConnected = (sock, timeoutMs = 3000) => {
    return new Promise((resolve) => {
      if (sock?.connected) return resolve(true);
      if (!sock) return resolve(false);

      const t = setTimeout(() => {
        cleanup();
        resolve(false);
      }, timeoutMs);

      const onConnect = () => {
        cleanup();
        resolve(true);
      };

      const cleanup = () => {
        clearTimeout(t);
        sock.off('connect', onConnect);
      };

      sock.on('connect', onConnect);
    });
  };

  useEffect(() => {

    const socket = io(import.meta.env.VITE_SOCKET_URL);
    if (!socket) {
      console.error("Socket connection failed");
      return;
    }
    setSocket(socket);
    socketRef.current = socket;

    socket.on('connect', () => {
      console.log('✅ socket connected', socket.id);
    });
    socket.on('connect_error', (err) => {
      console.error('❌ socket connect_error', err);
    });
    socket.on('disconnect', (reason) => {
      console.warn('⚠️ socket disconnected', reason);
    });

    socket.on("job_done", (msg) => {
      // proto toObject() usually returns camelCase keys:
      // urlcallback (not Urlcallback)
      const t0Raw =
        msg?.Urlcallback ??
        msg?.urlcallback ??
        msg?.Data?.Urlcallback ??
        msg?.Data?.urlcallback;

      const timeStart = parseInt(t0Raw, 10);
      const timeEnd = Date.now();
      const duration = timeEnd - timeStart;

      if (!Number.isFinite(duration) || duration < 0) return;

      resultsRef.current.push(duration);

      // Print once when we have enough samples
      if (!printedRef.current && expectedRef.current > 0 && resultsRef.current.length >= expectedRef.current) {
        printedRef.current = true;
        const s = computeTailStats(resultsRef.current);
        s.sent = expectedRef.current;
        s.expected = expectedRef.current;
        s.received = resultsRef.current.length;
        s.lost = Math.max(0, expectedRef.current - resultsRef.current.length);
        console.log("Tail latency stats (ms):", s);
        SendTelegramMessage(`Tail latency stats (ms): ${JSON.stringify(s)}`);

        console.log("Backend distribution (attempts):", distributionSnapshot(attemptsByBackendRef.current));
        console.log("Backend distribution (wins):", distributionSnapshot(winsByBackendRef.current));

      }
    });

    // Cleanup: prevents duplicate connections/listeners in React StrictMode dev
    return () => {
      socket.off('connect');
      socket.off('connect_error');
      socket.off('disconnect');
      socket.off('job_done');
      try {
        socket.disconnect();
      } catch {
        // ignore
      }
      if (socketRef.current === socket) socketRef.current = null;
      setSocket(null);
    };
  }, []);

  const testRequest = async () => {
    const TOTAL = parseInt(import.meta.env.VITE_TOTAL_REQUESTS, 10); // tăng lên 1000+ nếu muốn P99 ổn định hơn

    const countRequestsParsed = parseInt(countRequests, 10);

    const totalToSend = Number.isFinite(countRequestsParsed) && countRequestsParsed > 0
      ? countRequestsParsed
      : (Number.isFinite(TOTAL) && TOTAL > 0 ? TOTAL : 0);

    resultsRef.current = [];
    expectedRef.current = totalToSend;
    printedRef.current = false;

    attemptsByBackendRef.current = new Map();
    winsByBackendRef.current = new Map();

    // Ensure socket is connected before we start firing requests,
    // otherwise you'll get 0 "job_done" => 0 samples => NaN becomes null in JSON.
    const ok = await waitForSocketConnected(socketRef.current, 5000);
    if (!ok) {
      console.error('Socket not connected; cannot register queryIds / receive job_done');
    }

    for (let i = 0; i < totalToSend; i++) {
      fetchQueyData();
    }

    // fallback: nếu chưa đủ sample sau 10s thì vẫn in ra cái đang có
    setTimeout(() => {
      if (!printedRef.current) {
        const s = computeTailStats(resultsRef.current);
        s.sent = totalToSend;
        s.expected = expectedRef.current;
        s.received = resultsRef.current.length;
        s.lost = Math.max(0, expectedRef.current - resultsRef.current.length);
        console.log("Tail latency stats (partial, ms):", s);
        SendTelegramMessage(`Tail latency stats (partial, ms): ${JSON.stringify(s)}`);

        console.log("Backend distribution (attempts):", distributionSnapshot(attemptsByBackendRef.current));
        console.log("Backend distribution (wins):", distributionSnapshot(winsByBackendRef.current));

        if (resultsRef.current.length === 0) {
          console.warn('No samples received. Common causes: backend gRPC-Web unreachable/CORS, or socket server not emitting job_done.');
        }
      }
    }, 10000);
  }

  const fetchQueyData = async () => {

    const sock = socketRef.current;
    if (!sock?.connected) {
      console.warn('Skip request: socket not connected yet');
      return;
    }

    const queryId = "q-" + Math.random().toString(36).slice(2);
    const ID = Math.floor(Math.random() * 5) + 1;
    let userSelect = "";
    if (ID <= 2){ 
      userSelect = "username";
    } else if (ID === 3){ 
      userSelect = "email";
    } else { 
      userSelect = "balance";
    } 
    const querySQL = `SELECT ${userSelect} FROM users WHERE id = ${ID};`;

    const ring = new HashRing(shareDataServer, 20);
    const backends = ring.getNodes(querySQL, 3);

    // Count distribution of outgoing hedged attempts
    for (const b of backends) {
      if (b?.IP) bumpCount(attemptsByBackendRef.current, b.IP, 1);
    }

    // Start timing as close to the actual send as possible (avoid client-side noise)
    const timeStart = Date.now();

    sock.emit('register', { queryId: queryId });

    callWithHedging(
      backends,
      { QueryId: queryId, QuerySQL: querySQL, Urlcallback: timeStart.toString(), Action: "READ" },
      20000
    ).then(({ server }) => {
      if (server) bumpCount(winsByBackendRef.current, server, 1);
    }).catch(err => {
      console.error("❌ RPC failed:", err);
    });
  }

  return (
    <>
      <div>
        <a href="https://vite.dev" target="_blank">
          <img src={viteLogo} className="logo" alt="Vite logo" />
        </a>
        <a href="https://react.dev" target="_blank">
          <img src={reactLogo} className="logo react" alt="React logo" />
        </a>
      </div>
      <h1>Vite + React</h1>
      <div className="card">
        <button onClick={() => setCount((count) => count + 1)}>
          count is {count}
        </button>
        <p>
          Edit <code>src/App.jsx</code> and save to test HMR
        </p>
      </div>
      <p className="read-the-docs">
        Click on the Vite and React logos to learn more <br></br>
        {import.meta.env.VITE_SOCKET_URL} <br></br>
      </p>
      <input
        type="number"
        value={countRequests}
        onChange={e => setCountRequests(e.target.value)}
        placeholder="Requests"
        min={0}
      />
      <button onClick={testRequest}>Test Requests</button>
    </>
  )
}

export default App
