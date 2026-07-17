# Cloudflare R2 setup

Local development uses MinIO (see `compose.yaml`) and needs none of this. These
are the production steps, applied once per environment.

## Buckets

| Environment | Bucket |
|---|---|
| dev | `hookary-media-dev` |
| prod | `hookary-media-prod` |

Both stay **private**. Nothing is ever served from a public bucket URL; clients
get short-lived presigned URLs minted by the API.

## CORS

Browser uploads go straight from Flutter Web to R2, so the bucket needs CORS
rules of its own — without them presigned `PUT`s fail in the browser, while the
same URL works fine from curl. Edit `r2-cors.json` to match the real origin,
then:

```sh
npx wrangler r2 bucket cors set hookary-media-prod --file r2-cors.json
```

`ETag` must stay in `ExposeHeaders`: the browser cannot read part ETags without
it, and multipart completion has nothing to send back.

## Presigned URLs

Sign against the S3 API endpoint, not a custom domain:

```
https://<account-id>.r2.cloudflarestorage.com
```

A custom domain will not validate the signature. Treat every presigned URL as a
bearer token: 10–15 minute TTL, and never write one to a log.

## Lifecycle rules

| Prefix | Rule |
|---|---|
| `work/` | delete after 1–3 days — job scratch only |
| `source/` | delete after 30 days for MVP |
| `output/` | keep until the project is deleted |

Also enable automatic abort of incomplete multipart uploads; a browser tab
closed mid-upload otherwise leaves parts that are billed but unreachable.

The checked-in policy applies those three decisions in one operation:

```sh
npx wrangler r2 bucket lifecycle set hookary-media-prod --file r2-lifecycle.json
```

## Credentials

The API is the only process holding R2 credentials. Flutter never receives them.
Use an R2 API token scoped to the single bucket, not an account-wide key.
