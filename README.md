# Image resize service

## Requirements

- Go 1.25+
- `curl`, and `jq` for the zip example

## Run the server

```sh
go run .
```

The server listens on `localhost:8080`.

Or with the Makefile (runs with the race detector, logs to `server.log`):

```sh
make start     # start the server in the background
make request   # send req.json with async=true (ASYNC=false for blocking)
make clear     # stop the server and remove server.log
```

## Try it

Resize (async returns immediately, sync blocks until done):

```sh
curl -X POST -H "Content-Type: application/json" -d @req.json "http://localhost:8080/v1/resize?async=true"
curl -X POST -H "Content-Type: application/json" -d @req.json "http://localhost:8080/v1/resize?async=false"
```

Open one of the returned `url`s in the browser to get the image.

Resize and download all images as a zip:

```sh
curl -s -X POST -H "Content-Type: application/json" -d @req.json "http://localhost:8080/v1/resize?async=true" \
  | jq -r '[.[].id | select(. != "")] | join(",")' \
  | xargs -I {} curl "http://localhost:8080/v1/images/{}/archive.zip" -o images.zip
```

## Implementation notes

### Design

- **Cache** (existing LRU): finished images.
- **Registry** (`async.go`): images in progress. Each entry has a `done`
  channel that is closed when processing finishes, waking every request
  waiting for that image. It also prevents processing the same URL twice.
- **Worker pool** (`async.go`): `runtime.NumCPU()` workers and a job queue
  of 50, used by the async mode.

Workers add the image to the cache *before* removing it from the registry,
so a request that checks registry → cache never misses it.

### Resize modes

- `async=true`: URLs not in the cache are queued and the response (`202`) is
  returned immediately. `"success"` means accepted, not done yet. If the
  queue is full, the URL is returned as `"failure"`.
- `async=false` (default): URLs are processed in the request, but through
  the registry, so a URL already being processed is waited for, not fetched
  again. Returns `201`.

### Waiting for images in progress

- `/v1/image/{id}`: waits if the image is in progress. If processing fails,
  returns `500`; unknown ids return `404`.
- `/v1/images/{ids}/archive.zip`: one goroutine per id waits for its image
  and sends it to a channel. The handler writes each image to the zip as it
  arrives (completion order) and flushes it. Failed or unknown images are
  omitted. Entries use `zip.Store`, because with compression `Flush` holds
  back the data until the next entry.

### Timeouts

- Waiting is limited to 30s and returns `504`. For the zip, if some images
  were already sent, the archive is closed with those (the `200` status was
  already sent).
- The source fetch uses an `http.Client` with a 10s timeout, so a source
  server that never responds cannot block a worker forever.

### Known limitations

- Failed images are not cached; a later request for the same URL retries it.
- In sync mode, the URLs of one request are processed one after another.
