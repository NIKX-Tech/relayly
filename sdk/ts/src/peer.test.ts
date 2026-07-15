import { describe, expect, it } from 'vitest';
import { PeerConnection } from './peer.js';
import { NoiseSession } from './noise/session.js';
import { generateKeypair } from './noise/primitives.js';

/** Drives a full handshake between a fresh initiator NoiseSession and a PeerConnection
 * playing the responder role, returning both once ready. */
function completeFirstHandshake(aKey: ReturnType<typeof generateKeypair>, bKey: ReturnType<typeof generateKeypair>) {
  const { session: aSession, msg1 } = NoiseSession.startAsInitiator(aKey);
  const bPeer = new PeerConnection('device-a', '');

  const step1 = bPeer.handleHandshakeEnvelope(bKey, msg1);
  expect(step1.completed).toBeUndefined();
  const msg3 = aSession.handleHandshakeMessage(step1.reply!)!;

  const step2 = bPeer.handleHandshakeEnvelope(bKey, msg3);
  expect(step2.completed).toBeDefined();
  expect(step2.wasPending).toBe(false); // first-ever handshake, not a rekey
  bPeer.promote(step2.completed!);

  return { aSession, bPeer };
}

describe('PeerConnection', () => {
  it('completes a first-ever handshake and round-trips a transport message both ways', async () => {
    const aKey = generateKeypair();
    const bKey = generateKeypair();
    const { aSession, bPeer } = completeFirstHandshake(aKey, bKey);
    await aSession.waitReady();

    const ctAtoB = aSession.encrypt(new TextEncoder().encode('hello from A'));
    const ptAtoB = bPeer.recv(ctAtoB);
    expect(new TextDecoder().decode(ptAtoB)).toBe('hello from A');

    const ctBtoA = bPeer.send(new TextEncoder().encode('hello from B'));
    const ptBtoA = aSession.decrypt(ctBtoA);
    expect(new TextDecoder().decode(ptBtoA)).toBe('hello from B');
  });

  it('make-before-break: an in-flight rekey never disturbs the existing session', async () => {
    const aKey = generateKeypair();
    const bKey = generateKeypair();
    const { aSession, bPeer } = completeFirstHandshake(aKey, bKey);
    await aSession.waitReady();

    // Inject an unsolicited rekey attempt that never completes (stop after msg1/msg2).
    const rekey = NoiseSession.startAsInitiator(aKey);
    const rekeyStep = bPeer.handleHandshakeEnvelope(bKey, rekey.msg1);
    expect(rekeyStep.wasPending).toBe(true);
    expect(rekeyStep.completed).toBeUndefined(); // still mid-handshake

    // The original session must still work, both directions, throughout.
    const ct1 = aSession.encrypt(new TextEncoder().encode('still using the old session'));
    expect(new TextDecoder().decode(bPeer.recv(ct1))).toBe('still using the old session');

    const ct2 = bPeer.send(new TextEncoder().encode('original session still alive mid-rekey'));
    expect(new TextDecoder().decode(aSession.decrypt(ct2))).toBe('original session still alive mid-rekey');
  });

  it('rate-limits a second unsolicited msg1 arriving right after a failed first attempt', async () => {
    const aKey = generateKeypair();
    const bKey = generateKeypair();
    const { aSession, bPeer } = completeFirstHandshake(aKey, bKey);
    await aSession.waitReady();

    // First unsolicited attempt: accepted, then fails (garbage instead of a real
    // msg2 reply) — settles as failed, exactly like client.ts would observe after a
    // timeout, and calls abandon() on it.
    const rekey = NoiseSession.startAsInitiator(aKey);
    const firstOutcome = bPeer.handleHandshakeEnvelope(bKey, rekey.msg1);
    expect(firstOutcome.wasPending).toBe(true);
    const garbage = new Uint8Array(4).fill(0xff);
    const failedOutcome = bPeer.handleHandshakeEnvelope(bKey, garbage);
    expect(failedOutcome.completed).toBeDefined();
    bPeer.abandon(failedOutcome.completed!);

    // A second unsolicited attempt arriving immediately after must be dropped.
    const rekey2 = NoiseSession.startAsInitiator(aKey);
    const secondOutcome = bPeer.handleHandshakeEnvelope(bKey, rekey2.msg1);
    expect(secondOutcome.reply).toBeUndefined();
    expect(secondOutcome.completed).toBeUndefined();
    expect(secondOutcome.wasPending).toBe(false);

    // The original session must still be unaffected throughout.
    const ct = aSession.encrypt(new TextEncoder().encode('still alive'));
    expect(new TextDecoder().decode(bPeer.recv(ct))).toBe('still alive');
  });
});
