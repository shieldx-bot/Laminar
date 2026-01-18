import { LaminarGatewayClient } from "../../generated/LaminarServiceClientPb.js";
import * as laminarPb from "../../generated/laminar_pb.js";

 
function createClient(IP) {
  const hostname = /^https?:\/\//i.test(IP) ? IP : `http://${IP}`;
  return new LaminarGatewayClient(hostname, null, null);
}

export function callWithHedging(backends, requestData, timeoutMs = 500) {
  return new Promise((resolve, reject) => {
    let finished = false;
    const calls = [];
    let pending = backends.length;
    if (pending === 0) {
      reject(new Error("No backends provided"));
      return;
    }

    // Convert plain object to Proto Message
    const request = new laminarPb.CallBackRequest();

    const querySQL =
      requestData?.QuerySQL ?? requestData?.sql ?? requestData?.querySQL;
    const queryId =
      requestData?.QueryId ?? requestData?.queryId;

    const urlcallback =
      requestData?.Urlcallback ??
      requestData?.urlcallback ??
      requestData?.callbackUrl ??
      requestData?.callbackURL;

    const action =
      requestData?.Action ?? requestData?.action;

    const payload =
      requestData?.payload; // should be Uint8Array for bytes

    if (querySQL) request.setQuerysql(querySQL);
    if (queryId) request.setQueryid(queryId);
    if (urlcallback) request.setUrlcallback(urlcallback);
    if (action) request.setAction(action);
    if (payload instanceof Uint8Array) request.setPayload(payload);

    backends.forEach((server) => {
      const client = createClient(server.IP);

      let settled = false;
      let timeoutId;

      const settleOnce = () => {
        if (settled) return false;
        settled = true;
        if (timeoutId) clearTimeout(timeoutId);
        return true;
      };

      const call = client.callBack(
        request,
        {},
        (err, response) => {
          if (finished) return;

          // Ensure we don't also count the timeout for this call.
          if (!settleOnce()) return;

          if (!err) {
            finished = true;

            // cancel request còn lại
            calls.forEach(c => c.cancel());

            resolve({
              server: server.IP,
              response: response.toObject()
            });
          } else {
            pending -= 1;
            if (pending === 0 && !finished) {
              reject(err);
            }
          }
        }
      );

      calls.push(call);

      timeoutId = setTimeout(() => {
        if (finished) return;

        // If callback already fired (success/error), ignore timeout.
        if (!settleOnce()) return;

        call.cancel();
        pending -= 1;
        if (pending === 0 && !finished) {
          reject(new Error("All hedged calls timed out"));
        }
      }, timeoutMs);

      const originalCancel = call.cancel.bind(call);
      call.cancel = () => {
        settleOnce();
        originalCancel();
      };
    });
} );
}
