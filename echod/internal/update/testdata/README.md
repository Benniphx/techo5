These public signature fixtures exercise the compiled release trust boundary offline.

- `upstream-v0.9.30-manifest.json` and its detached signature are exact bytes from
  <https://github.com/HuskerMinion/techo5/releases/download/v0.9.30/manifest.json>
  and the adjacent `manifest.json.sig` release asset, downloaded on 2026-10-04.
- `native-trust-message.json` is a purpose-only message signed by the fork release
  key. It cannot be installed as a firmware manifest. The signature covers its
  exact bytes, without a trailing newline.

No private key or seed is included. Show tests accept the fork signature and reject
the genuine upstream one; Dot and Spot tests retain the upstream trust root.
