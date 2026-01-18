import { useState, useRef, useEffect } from 'react'
import reactLogo from './assets/react.svg'
import viteLogo from '/vite.svg'
import { io } from "socket.io-client";
import './App.css'
import { shareDataServer } from './share/share';
import { callWithHedging } from './load-balancer/gRPC/main';



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

  const resultsRef = useRef([]);
  const expectedRef = useRef(0);
  const printedRef = useRef(false);
  // Keep as string so clearing the input doesn't produce NaN warnings.
  const [countRequests, setCountRequests] = useState('');

  // Distribution counters (do NOT affect latency samples)
  const attemptsByBackendRef = useRef(new Map()); // hedged attempts sent to each backend
  const primaryByBackendRef = useRef(new Map());  // primary (consistent hash) backend per request

  // Robust latency timing: do NOT depend on server echoing Urlcallback
  const startTimesRef = useRef(new Map()); // queryId -> timeStart(ms)

  useEffect(() => {

    const socket = io(import.meta.env.VITE_SOCKET_URL);
    if (!socket) {
      console.error("Socket connection failed");
      return;
    }
    setSocket(socket);

    socket.on("job_done", (msg) => {
      const queryId =
        msg?.QueryId ??
        msg?.queryId ??
        msg?.query_id ??
        msg?.Data?.QueryId ??
        msg?.Data?.queryId;

      // Prefer local start time (most accurate, avoids missing Urlcallback)
      let timeStart = queryId ? startTimesRef.current.get(queryId) : undefined;

      if (!Number.isFinite(timeStart)) {
        const t0Raw =
          msg?.Urlcallback ??
          msg?.urlcallback ??
          msg?.Data?.Urlcallback ??
          msg?.Data?.urlcallback;
        timeStart = parseInt(t0Raw, 10);
      }

      const timeEnd = Date.now();
      const duration = timeEnd - timeStart;

      if (!Number.isFinite(duration) || duration < 0) return;

      if (queryId) startTimesRef.current.delete(queryId);

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
        console.log("Backend distribution (primary):", distributionSnapshot(primaryByBackendRef.current));

      }
    });
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
    primaryByBackendRef.current = new Map();
    startTimesRef.current = new Map();

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
        console.log("Backend distribution (primary):", distributionSnapshot(primaryByBackendRef.current));
      }
    }, 10000);
  }

  const fetchQueyData = async () => {

    const queryId = "q-" + Math.random().toString(36).slice(2);
    const querySQL = `SELECT * FROM users WHERE id = ${Math.floor(Math.random() * 5) + 1};`;

    const ring = new (await import('./load-balancer/vnode/main')).HashRing(shareDataServer, 20);
    const backends = ring.getNodes(querySQL, 3);

    // Count request distribution to each backend (attempts = actual outgoing traffic)
    for (const b of backends) {
      if (b?.IP) bumpCount(attemptsByBackendRef.current, b.IP, 1);
    }
    if (backends?.[0]?.IP) bumpCount(primaryByBackendRef.current, backends[0].IP, 1);

    // Start timing as close to actual send as possible
    const timeStart = Date.now();
    startTimesRef.current.set(queryId, timeStart);

    socket.emit('register', { queryId: queryId });

    callWithHedging(
      backends,
      { QueryId: queryId, QuerySQL: querySQL, Urlcallback: timeStart.toString(), Action: "READ" },
      20000
    ).catch(err => {
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