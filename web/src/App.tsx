import React from 'react';
import logo from './logo.svg';
import './App.css';



function App() {
  const upload = async () => {
    const payload = new Uint8Array(10 * 1024 * 1024); // 10MB

    const requests = Array.from({ length: 5 }, () =>
      fetch('https://shieldx.dev/api/upload', {
        method: 'POST',
        body: payload,
      })
    );

    const start = performance.now();
    await Promise.all(requests);
    alert(`Done in ${performance.now() - start} ms`);
  };
  return (
    <div className="App">
      <header className="App-header">
        <img src={logo} className="App-logo" alt="logo" />
        <p>
          Edit <code>src/App.tsx</code> and save to reload.
        </p>

        <button onClick={upload}>Upload 5x 10MB</button>
      </header>
    </div>
  );
}

export default App;
