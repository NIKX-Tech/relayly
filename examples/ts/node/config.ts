/**
 * Simple configuration loader for the Node.js example.
 * Follows the strategy: CLI arguments > Environment Variables > Defaults.
 */

export interface Config {
  serverUrl: string;
  pairCode: string;
  message: string;
  keyPath: string;
}

export function loadConfig(): Config {
  // Defaults
  let serverUrl = 'wss://relay.example.com';
  let pairCode = '';
  let message = 'Hello from Node.js! 👋';
  let keyPath = process.env.RELAYLY_KEY_PATH || require('node:path').join(require('node:os').homedir(), '.relayly', 'node-device.key');

  // Environment Variables
  if (process.env.RELAYLY_SERVER) serverUrl = process.env.RELAYLY_SERVER;
  if (process.env.RELAYLY_PAIR_CODE) pairCode = process.env.RELAYLY_PAIR_CODE;
  if (process.env.RELAYLY_MESSAGE) message = process.env.RELAYLY_MESSAGE;

  // CLI Arguments (Simple positional ones for this example)
  const args = process.argv.slice(2);
  if (args.length >= 1) serverUrl = args[0];
  if (args.length >= 2) pairCode = args[1];
  if (args.length >= 3) message = args.slice(2).join(' ');

  return { serverUrl, pairCode, message, keyPath };
}
