$ErrorActionPreference = "Stop"

if (-not $env:HTTP_ADDR) {
    $env:HTTP_ADDR = ":8080"
}
if (-not $env:BASE_URL) {
    $env:BASE_URL = "http://localhost:8080"
}
if (-not $env:DATABASE_PATH) {
    $env:DATABASE_PATH = "data/urls.db"
}

go run ./cmd/server
