import { LaminarGatewayClient } from "../../generated/LaminarServiceClientPb.js";
import * as laminarPb from "../../generated/laminar_pb.js";

 
function createClient(IP) {
  return new LaminarGatewayClient(IP, null, null);
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
    if (requestData.QuerySQL) request.setQuerysql(requestData.QuerySQL);
    if (requestData.QueryId) request.setQueryid(requestData.QueryId);
    // Add other fields as needed

    backends.forEach((server) => {
      const client = createClient(server.IP);

      const call = client.callBack(
        request,
        {},
        (err, response) => {
          if (finished) return;

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

      const timeoutId = setTimeout(() => {
        if (finished) return;
        call.cancel();
        pending -= 1;
        if (pending === 0 && !finished) {
          reject(new Error("All hedged calls timed out"));
        }
      }, timeoutMs);

      const originalCancel = call.cancel.bind(call);
      call.cancel = () => {
        clearTimeout(timeoutId);
        originalCancel();
      };
    });
} );
}
 