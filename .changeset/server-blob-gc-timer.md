---
"@nota/desktop": patch
---

The server can collect unreferenced blobs on its own: set `NOTA_SERVER_BLOB_GC_INTERVAL=24h` (or `7d`) and it runs the same pass as `nota serve blobs gc` on that timer. Off by default.
