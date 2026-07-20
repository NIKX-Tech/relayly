/**
 * Simple configuration loader for the Node.js example.
 * Follows the strategy: CLI arguments > Environment Variables > Defaults.
 */

import { join } from 'node:path';
import { homedir } from 'node:os';

export interface Config {
  serverUrl: string;
  pairCode: string;
  message: string;
  keyPath: string;
  credsPath: string;
}

export function loadConfig(): Config {
  // Defaults
  let serverUrl = 'ws://localhost:8080/ws';
  let pairCode = '';
  let message = 'Hello from Node.js! 👋';
  const keyPath = process.env.RELAYLY_KEY_PATH || join(homedir(), '.relayly', 'node-device.key');
  const credsPath = process.env.RELAYLY_CREDS_PATH || join(homedir(), '.relayly', 'node-device.json');

  // Environment Variables
  if (process.env.RELAYLY_SERVER) serverUrl = process.env.RELAYLY_SERVER;
  if (process.env.RELAYLY_PAIR_CODE) pairCode = process.env.RELAYLY_PAIR_CODE;
  if (process.env.RELAYLY_MESSAGE) message = process.env.RELAYLY_MESSAGE;

  // CLI Arguments (Simple positional ones for this example)
  const args = process.argv.slice(2);
  if (args.length >= 1) serverUrl = args[0]!;
  if (args.length >= 2) pairCode = args[1]!;
  if (args.length >= 3) message = args.slice(2).join(' ');

  return { serverUrl, pairCode, message, keyPath, credsPath };
}
