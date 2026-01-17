import { useState, useRef, useEffect } from 'react'
import reactLogo from './assets/react.svg'
import viteLogo from '/vite.svg'
import { io } from "socket.io-client";
import './App.css'
import { shareDataServer } from './share/share';
import { callWithHedging } from './load-balancer/gRPC/main';

function App() {
  const [count, setCount] = useState(0)
  const [socket, setSocket] = useState(null);
  const resultsRef = useRef([]);

  useEffect(() => {



    const socket = io("http://localhost:3000");
    if (!socket) {
      console.error("Socket connection failed");
      return;
    } else {
      console.log("Socket connected");
    }
    setSocket(socket);


    socket.on("job_done", (msg) => {
      console.log("Job done message received:", msg);
      const timeEnd = Date.now();
      const timeStart = parseInt(msg.Urlcallback);
      const duration = timeEnd - timeStart;
      console.log("Response time (ms):", duration);
      if (!isNaN(duration)) {
        resultsRef.current.push(duration);
      }
    });
  }, []);

  const testRequest = () => {
    for (let i = 0; i < 100; i++) {

      fetchQueyData();

    }
    // After some time, log average response time
    setTimeout(() => {
      const arr = resultsRef.current;
      console.log("Total requests:", arr.length);
      if (arr.length > 0) {
        console.log("Average response time:", arr.reduce((a, b) => a + b, 0) / arr.length);
      }
    }, 5000);




  }


  const fetchQueyData = async () => {
    const queryId = "q-" + Math.random().toString(36).slice(2);
    const querySQL = `SELECT * FROM users limit  1`;
    const timeStart = Date.now();
    const ring = new (await import('./load-balancer/vnode/main')).HashRing(shareDataServer, 20);
    const backends = ring.getNodes(querySQL, 3);
    socket.emit('register', { queryId: queryId });
    callWithHedging(
      backends,
      {QueryId: queryId, QuerySQL: querySQL, Urlcallback: timeStart.toString(), Action: "READ"},
      400
    ).then(res => {
      // const response =
      // console.log("✅ Response from:", res.server);
      // console.log(res.response);
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
        Click on the Vite and React logos to learn more
      </p>
      <button onClick={testRequest}>Test Requests</button>
    </>
  )
}

export default App
