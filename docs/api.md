# HTTP API

## Create a short URL

`POST /api/v1/urls`

```json
{
  "url": "https://go.dev/doc/",
  "custom_alias": "go-docs",
  "expires_in_seconds": 86400
}
```

`custom_alias` and `expires_in_seconds` are optional. Aliases must contain
4-32 letters, numbers, underscores, or hyphens.

Success: `201 Created`

```json
{
  "id": 1,
  "code": "go-docs",
  "short_url": "http://localhost:8080/go-docs",
  "original_url": "https://go.dev/doc/",
  "created_at": "2026-06-13T00:00:00Z",
  "expires_at": "2026-06-14T00:00:00Z",
  "visits": 0
}
```

When a custom alias already exists, the API returns `409 Conflict` with the
existing link:

```json
{
  "error": "short code already exists",
  "short_url": "http://localhost:8080/go-docs"
}
```

## Get link metadata

`GET /api/v1/urls/{code}`

Returns `200 OK`, or `404 Not Found` when the link is missing or expired.

## Redirect

`GET /{code}`

Returns `302 Found` and atomically increments the visit count.

## Delete a link

`DELETE /api/v1/urls/{code}`

Returns `204 No Content`.

## Health checks

- `GET /healthz` checks that the process is serving requests.
- `GET /readyz` checks that the database is reachable.

## Error format

```json
{
  "error": "short link not found"
}
```
