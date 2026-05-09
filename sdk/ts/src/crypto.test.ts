import { describe, it, expect } from 'vitest';
import { 
  generateKey, 
  encrypt, 
  decrypt, 
  encodeBase64, 
  decodeBase64, 
  stringToBytes, 
  bytesToString 
} from './crypto';

describe('Relayly Crypto', () => {
  it('should generate a valid keypair', () => {
    const keyPair = generateKey();
    expect(keyPair.publicKey).toHaveLength(32);
    expect(keyPair.privateKey).toHaveLength(32);
  });

  it('should encrypt and decrypt messages correctly', () => {
    const alice = generateKey();
    const bob = generateKey();
    
    const message = 'Hello Bob, I am Alice!';
    const plaintext = stringToBytes(message);
    
    // Alice encrypts for Bob
    const { ciphertext, nonce } = encrypt(plaintext, bob.publicKey, alice.privateKey);
    
    // Bob decrypts from Alice
    const decryptedBytes = decrypt(ciphertext, nonce, alice.publicKey, bob.privateKey);
    const decryptedMessage = bytesToString(decryptedBytes);
    
    expect(decryptedMessage).toBe(message);
  });

  it('should handle base64 encoding/decoding', () => {
    const bytes = new Uint8Array([1, 2, 3, 4, 5]);
    const b64 = encodeBase64(bytes);
    const decoded = decodeBase64(b64);
    
    expect(decoded).toEqual(bytes);
  });

  it('should fail decryption with wrong keys', () => {
    const alice = generateKey();
    const bob = generateKey();
    const eve = generateKey();
    
    const message = 'Secret message';
    const plaintext = stringToBytes(message);
    
    const { ciphertext, nonce } = encrypt(plaintext, bob.publicKey, alice.privateKey);
    
    // Eve tries to decrypt with her own key
    expect(() => {
      decrypt(ciphertext, nonce, alice.publicKey, eve.privateKey);
    }).toThrow();
  });
});
