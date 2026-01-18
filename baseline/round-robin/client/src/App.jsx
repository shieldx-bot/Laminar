import { useEffect, useRef, useState } from 'react'

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

async function postJsonWithTimeout(url, body, timeoutMs) {
  const controller = new AbortController();
  const t = setTimeout(() => controller.abort(), timeoutMs);
  try {
    const res = await fetch(url, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(body),
      signal: controller.signal,
    });

    const text = await res.text();
    let json;
    try {
      json = JSON.parse(text);
    } catch {
      json = { raw: text };
    }

    if (!res.ok) {
      const err = new Error(json?.error || `HTTP ${res.status}`);
      err.status = res.status;
      err.payload = json;
      throw err;
    }

    return json;
  } finally {
    clearTimeout(t);
  }
}

export default function App() {
  const [countRequests, setCountRequests] = useState('');
  const [isRunning, setIsRunning] = useState(false);
  const [lbHealth, setLbHealth] = useState(null);

  const lbUrl = (import.meta.env.VITE_LB_URL || 'http://35.186.151.245:8082').replace(/\/$/, '');
  const defaultTotal = parseInt(import.meta.env.VITE_TOTAL_REQUESTS || '0', 10);
  const timeoutMs = parseInt(import.meta.env.VITE_TIMEOUT_MS || '20000', 10);

  useEffect(() => {
    const url = `${lbUrl}/api/health`;
    fetch(url)
      .then(r => r.json())
      .then(data => setLbHealth(data))
      .catch(() => setLbHealth({ ok: false }));
  }, [lbUrl]);

  const resultsRef = useRef([]);
  const expectedRef = useRef(0);
  const printedRef = useRef(false);

  const attemptsByBackendRef = useRef(new Map());
  const primaryByBackendRef = useRef(new Map());

  const startTimesRef = useRef(new Map());
  const completedRef = useRef(0);

  const printOnce = (label, totalSent) => {
    if (printedRef.current) return;
    printedRef.current = true;

    const s = computeTailStats(resultsRef.current);
    s.sent = totalSent;
    s.expected = expectedRef.current;
    s.received = resultsRef.current.length;
    s.lost = Math.max(0, totalSent - resultsRef.current.length);

    console.log(`${label} tail latency stats (ms):`, s);
    console.log('Backend distribution (attempts):', distributionSnapshot(attemptsByBackendRef.current));
    console.log('Backend distribution (primary):', distributionSnapshot(primaryByBackendRef.current));
  };

  const run = async () => {
    const parsed = parseInt(countRequests, 10);
    const totalToSend = Number.isFinite(parsed) && parsed > 0
      ? parsed
      : (Number.isFinite(defaultTotal) && defaultTotal > 0 ? defaultTotal : 0);

    if (!totalToSend) return;

    setIsRunning(true);

    resultsRef.current = [];
    expectedRef.current = totalToSend;
    printedRef.current = false;

    attemptsByBackendRef.current = new Map();
    primaryByBackendRef.current = new Map();
    startTimesRef.current = new Map();
    completedRef.current = 0;

    const apiUrl = `${lbUrl}/api/query`;

    const jobs = Array.from({ length: totalToSend }, async () => {
      const queryId = `q-${Math.random().toString(36).slice(2)}`;
      const querySQL = `SELECT * FROM users WHERE id = ${Math.floor(Math.random() * 5) + 1};`;

      const timeStart = Date.now();
      startTimesRef.current.set(queryId, timeStart);

      try {
        const resp = await postJsonWithTimeout(apiUrl, {
          QueryId: queryId,
          QuerySQL: querySQL,
          Urlcallback: String(timeStart),
          Action: 'READ'
        }, timeoutMs);

        const chosen = resp?.ChosenBackend;
        if (chosen) {
          // Round-robin baseline: 1 attempt = 1 primary
          bumpCount(attemptsByBackendRef.current, chosen, 1);
          bumpCount(primaryByBackendRef.current, chosen, 1);
        }

        const t0 = startTimesRef.current.get(queryId);
        const duration = Date.now() - (Number.isFinite(t0) ? t0 : timeStart);
        startTimesRef.current.delete(queryId);

        if (Number.isFinite(duration) && duration >= 0) {
          resultsRef.current.push(duration);
        }
      } catch (err) {
        // Still count where the request was routed if LB returns it in error payload
        const chosen = err?.payload?.ChosenBackend;
        if (chosen) {
          bumpCount(attemptsByBackendRef.current, chosen, 1);
          bumpCount(primaryByBackendRef.current, chosen, 1);
        }
      } finally {
        completedRef.current += 1;
        if (!printedRef.current && completedRef.current >= totalToSend) {
          printOnce('Final', totalToSend);
          setIsRunning(false);
        }
      }
    });

    // Fallback print if some requests hang beyond timeout window
    setTimeout(() => {
      if (!printedRef.current) {
        printOnce('Partial', totalToSend);
        setIsRunning(false);
      }
    }, timeoutMs + 1000);

    // Ensure all promises are actually scheduled
    await Promise.allSettled(jobs);
  };

  return (
    <div className="card">
      <h2>Baseline: Centralized Round-Robin</h2>
      <div className="row">
        <label>
          Requests
          <input
            type="number"
            value={countRequests}
            onChange={e => setCountRequests(e.target.value)}
            placeholder={String(defaultTotal || 1000)}
            min={0}
            style={{ marginLeft: 8 }}
          />
        </label>
        <button onClick={run} disabled={isRunning}>
          {isRunning ? 'Running...' : 'Test Requests'}
        </button>
      </div>

      <p className="mono" style={{ marginTop: 12 }}>
        {`LB: ${lbUrl}\nTimeout: ${timeoutMs}ms\nBackends (from LB): ${lbHealth?.backends?.length ? lbHealth.backends.join(', ') : '(unknown)'}`}
      </p>

      <p className="mono">
        Output: open DevTools Console to see tail latency + backend distribution.
      </p>
    </div>
  );
}
