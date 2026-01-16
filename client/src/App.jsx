import { useState } from 'react'
import reactLogo from './assets/react.svg'
import viteLogo from '/vite.svg'
import { io } from "socket.io-client";
import { useEffect } from 'react'
import './App.css'

function App() {
  const [count, setCount] = useState(0)
  const [socket, setSocket] = useState(null);
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
      console.log("JOB DONE:", msg);
      const timeEnd = Date.now();
      const timeStart = parseInt(msg.Urlcallback);
      console.log("Time taken (ms):",timeStart);
    });
  }, []);
  const fetQueryData = async () => {
    const queryId = "q-" + Math.random().toString(36).slice(2);
    console.log("QueryId:", queryId);
     socket.emit("register", { queryId });
     const timeStart = Date.now();
    try {
      const response = await fetch('http://localhost:8083/balance', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({
          QuerySQL: 'SELECT * FROM users limit 1;',
          QueryId: queryId,
          Action: "read",
          Urlcallback: timeStart.toString(),
        }),
      });
      const data = await response.json();
      console.log("Query Response from loadbalancer:", data);
 
      
    }
    catch (error) {
      console.error("Error fetching query data:", error);
    }
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
      <button onClick={fetQueryData}>Fetch Query Data</button>
    </>
  )
}

export default App
