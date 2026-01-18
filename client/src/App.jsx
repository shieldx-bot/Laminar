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
    p50: percentile(arr, 50),
    p95: percentile(arr, 95),
    p99: percentile(arr, 99),
    max: arr.length ? arr[arr.length - 1] : NaN,
  };
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

  useEffect(() => {

    const socket = io(import.meta.env.VITE_SOCKET_URL);
    if (!socket) {
      console.error("Socket connection failed");
      return;
    }
    setSocket(socket);

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
        console.log("Tail latency stats (ms):", s);
        SendTelegramMessage(`Tail latency stats (ms): ${JSON.stringify(s)}`);

      }
    });
  }, []);

  const testRequest = async () => {
    const TOTAL = parseInt(import.meta.env.VITE_TOTAL_REQUESTS, 10); // tăng lên 1000+ nếu muốn P99 ổn định hơn
    resultsRef.current = [];
    expectedRef.current = TOTAL;
    printedRef.current = false;

    for (let i = 0; i < TOTAL; i++) {
      fetchQueyData();
    }

    // fallback: nếu chưa đủ sample sau 10s thì vẫn in ra cái đang có
    setTimeout(() => {
      if (!printedRef.current) {
        const s = computeTailStats(resultsRef.current);
        console.log("Tail latency stats (partial, ms):", s);
      }
    }, 10000);
  }

  const fetchQueyData = async () => {

    const queryId = "q-" + Math.random().toString(36).slice(2);
    const querySQL = `SELECT * FROM users WHERE id = ${Math.floor(Math.random() * 100) + 1};`;
    const timeStart = Date.now();

    const ring = new (await import('./load-balancer/vnode/main')).HashRing(shareDataServer, 20);
    const backends = ring.getNodes(querySQL, 3);

    socket.emit('register', { queryId: queryId });

    callWithHedging(
      backends,
      { QueryId: queryId, QuerySQL: querySQL, Urlcallback: timeStart.toString(), Action: "READ" },
      5000
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
      <button onClick={testRequest}>Test Requests</button>
    </>
  )
}

export default App
