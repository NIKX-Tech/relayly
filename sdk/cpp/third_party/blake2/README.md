# Vendored BLAKE2s reference implementation

`blake2s-ref.c`, `blake2.h`, and `blake2-impl.h` are copied verbatim from the
[official BLAKE2 repository](https://github.com/BLAKE2/BLAKE2) (`ref/` directory),
authored by the algorithm's own designers (Samuel Neves et al.). Triple-licensed
CC0 / OpenSSL / Apache-2.0 — see the header comment in each file.

Vendored because libsodium (already a dependency, used for X25519 and
ChaCha20-Poly1305) only implements BLAKE2b, not BLAKE2s, and
`Noise_XX_25519_ChaChaPoly_BLAKE2s` needs the latter. See `sdk/cpp/README.md`'s
"Why libsodium + hand-written XX, not noise-c?" section for the full reasoning.

Only `blake2s-ref.c`'s `blake2s_init`/`blake2s_init_key`/`blake2s_update`/
`blake2s_final` functions are used; the file's own self-test `main()` is guarded
behind `#if defined(SUPERCOP)` and is never compiled (we don't define `SUPERCOP`),
so `blake2-kat.h` (the self-test's known-answer-test vectors) doesn't need to be
vendored alongside it.

Do not hand-edit these files — if BLAKE2/BLAKE2 ever ships a fix, re-copy from
upstream instead of patching in place.
