---
"@nota/desktop": patch
---

The sync server can keep file contents in an S3-compatible bucket (Cloudflare R2, AWS S3, MinIO) with `NOTA_SERVER_BLOBS=s3://bucket/prefix`, so its disk stays small. `nota serve blobs migrate` moves existing blobs there. See docs/deploy.md.
