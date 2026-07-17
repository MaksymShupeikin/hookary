#!/bin/sh
# Creates the local MinIO bucket that stands in for the R2 bucket, and keeps it
# private: clients only ever touch storage through presigned URLs from the API.
#
# CORS is not set here. MinIO has no per-bucket CORS API and takes a server-wide
# origin list instead, so it is configured on the minio service in compose.yaml.
# Real per-bucket rules for R2 live in deploy/cloudflare/r2-cors.json.
#
# Idempotent: compose re-runs it on every up.
set -eu

mc alias set local "http://minio:9000" "$S3_ACCESS_KEY_ID" "$S3_SECRET_ACCESS_KEY"

if mc ls "local/$S3_BUCKET" >/dev/null 2>&1; then
  echo "bucket $S3_BUCKET already exists"
else
  mc mb "local/$S3_BUCKET"
fi

mc anonymous set none "local/$S3_BUCKET"

echo "minio-init done: $S3_BUCKET ready and private"
