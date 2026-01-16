export function getSocketTarget() {
  // logic lấy IP / port (env, config, discovery, hash…)
  return {
    host: process.env.ACK_HOST || '127.0.0.1',
    port: Number(process.env.ACK_PORT || 9000),
  };
}