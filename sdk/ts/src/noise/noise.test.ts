import { describe, expect, it } from 'vitest';
import { HandshakeState } from './handshakeState.js';
import type { KeyPair } from './primitives.js';

// ── flynn/noise cross-validation vectors ──────────────────────────────────────
//
// Generated with a standalone Go program using flynn/noise (already proven
// server-side and in sdk/go) with a deterministic Random source, so the entire
// handshake is reproducible. This is the actual correctness gate for this
// hand-written implementation, not the spec recalled from memory: Noise's AEAD
// auth means a subtle bug here fails loudly (decrypt/MAC failure) rather than
// silently producing plausible-looking wrong output.
//
// detReader(seed) yields bytes seed, seed+1, seed+2, ... — mirrored exactly below.
function detBytes(seed: number, length: number): Uint8Array {
  const out = new Uint8Array(length);
  for (let i = 0; i < length; i++) out[i] = (seed + i) & 0xff;
  return out;
}
function hex(s: string): Uint8Array {
  const out = new Uint8Array(s.length / 2);
  for (let i = 0; i < out.length; i++) out[i] = parseInt(s.slice(i * 2, i * 2 + 2), 16);
  return out;
}
function toHex(b: Uint8Array): string {
  return Array.from(b).map((x) => x.toString(16).padStart(2, '0')).join('');
}

const VECTORS = {
  aStaticPrivate: '0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20',
  aStaticPublic: '07a37cbc142093c8b755dc1b10e86cb426374ad16aa853ed0bdfc0b2b86d1c7c',
  bStaticPrivate: '101112131415161718191a1b1c1d1e1f202122232425262728292a2b2c2d2e2f',
  bStaticPublic: 'd89e3bad79437dbed9f843418304f460ff05c7fe81fe4a9577a804cb9367ff66',
  msg1: '358072d6365880d1aeea329adf9121383851ed21a28e3b75e965d0d2cd166254',
  msg2: '34e42d4af5ef94a07a3a84201b889d4cd1a743cb27b11b6a10438a8feb8e5847ee0b2fa3bbca43904cbf6186d5e09fe67128c94cc3e3da6d35bf21f0358c487d5300c27a709ae1da5b4951c9eb1f0afd64e57891c7894b617293b07c9a455311',
  msg3: 'b8312f344cb91f060c34ae9a514f48981b3316af898179729fd217b843cf0f75b07d427b956b287b149ee47a4b0b71e3b822b0f15bc616ce52af8a3dbeab8bc8',
  ctAtoB1: 'a21eb0be51f6230018b2a51f1b501eb2885cf12b23e6351f1a577c43',
  ctBtoA1: '362c3040c6440177f0d09b74b5457be4fc12cc30733563aa87dc83b9',
  ctAtoB2: 'def9e930e19aaaa45d424f8ab9eeab956eb165a009d9029b0ee94fe91ab7d7519f924fad9e',
  ctBtoA2: '3794338f3282d21838842470a5d9ee3009b3b85bcc2958fc4a39d7b43c48fbed764c330388',
};

function aStatic(): KeyPair {
  return { privateKey: hex(VECTORS.aStaticPrivate), publicKey: hex(VECTORS.aStaticPublic) };
}
function bStatic(): KeyPair {
  return { privateKey: hex(VECTORS.bStaticPrivate), publicKey: hex(VECTORS.bStaticPublic) };
}

describe('HandshakeState (Noise_XX_25519_ChaChaPoly_BLAKE2s) vs flynn/noise', () => {
  it('produces byte-identical handshake messages', () => {
    const initiator = new HandshakeState(true, aStatic(), (n) => detBytes(0x20, n));
    const responder = new HandshakeState(false, bStatic(), (n) => detBytes(0x30, n));

    const { message: msg1 } = initiator.writeMessage();
    expect(toHex(msg1)).toBe(VECTORS.msg1);
    responder.readMessage(msg1);

    const { message: msg2 } = responder.writeMessage();
    expect(toHex(msg2)).toBe(VECTORS.msg2);
    initiator.readMessage(msg2);

    const { message: msg3, result: initResult } = initiator.writeMessage();
    expect(toHex(msg3)).toBe(VECTORS.msg3);
    expect(initResult).toBeDefined();

    const { result: respResult } = responder.readMessage(msg3);
    expect(respResult).toBeDefined();

    expect(toHex(responder.peerStaticKey!)).toBe(VECTORS.aStaticPublic);
    expect(toHex(initiator.peerStaticKey!)).toBe(VECTORS.bStaticPublic);

    // cs1 = initiator->responder, cs2 = responder->initiator (Noise convention).
    const ctAB1 = initResult!.cipherState1.encryptWithAd(new Uint8Array(0), new TextEncoder().encode('hello from A'));
    expect(toHex(ctAB1)).toBe(VECTORS.ctAtoB1);
    const ptAB1 = respResult!.cipherState1.decryptWithAd(new Uint8Array(0), ctAB1);
    expect(new TextDecoder().decode(ptAB1)).toBe('hello from A');

    const ctBA1 = respResult!.cipherState2.encryptWithAd(new Uint8Array(0), new TextEncoder().encode('hello from B'));
    expect(toHex(ctBA1)).toBe(VECTORS.ctBtoA1);
    const ptBA1 = initResult!.cipherState2.decryptWithAd(new Uint8Array(0), ctBA1);
    expect(new TextDecoder().decode(ptBA1)).toBe('hello from B');

    // Second message each direction — confirms the nonce counter increments correctly.
    const ctAB2 = initResult!.cipherState1.encryptWithAd(new Uint8Array(0), new TextEncoder().encode('second message from A'));
    expect(toHex(ctAB2)).toBe(VECTORS.ctAtoB2);

    const ctBA2 = respResult!.cipherState2.encryptWithAd(new Uint8Array(0), new TextEncoder().encode('second message from B'));
    expect(toHex(ctBA2)).toBe(VECTORS.ctBtoA2);
  });

  it('rejects a corrupted transport ciphertext', () => {
    const initiator = new HandshakeState(true, aStatic(), (n) => detBytes(0x20, n));
    const responder = new HandshakeState(false, bStatic(), (n) => detBytes(0x30, n));

    const { message: msg1 } = initiator.writeMessage();
    responder.readMessage(msg1);
    const { message: msg2 } = responder.writeMessage();
    initiator.readMessage(msg2);
    const { message: msg3, result: initResult } = initiator.writeMessage();
    const { result: respResult } = responder.readMessage(msg3);

    const ct = initResult!.cipherState1.encryptWithAd(new Uint8Array(0), new TextEncoder().encode('hi'));
    ct[0] = (ct[0] ?? 0) ^ 0xff;
    expect(() => respResult!.cipherState1.decryptWithAd(new Uint8Array(0), ct)).toThrow();
  });
});
