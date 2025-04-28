# Concurrent Queue Example Server

This example demonstrates how to use the `gocmq` library to create a concurrent queue for scraping URLs. The server provides two endpoints:

1. `/scrape/{url}` - This endpoint enqueues a job to scrape the specified URL and returns a job ID.
2. `/scrape/status/{job_id}` - This endpoint returns the status of the job with the given job ID.

## Running the Server

To run the server, navigate to the `examples/server` directory and execute the following command:

```sh
go run main.go
```

The server will start and listen on port 8080.

## Endpoints

### 1. Enqueue a Scrape Job

**Endpoint:** `/scrape/{url}`

**Method:** `GET`

**Description:** Enqueues a job to scrape the specified URL and returns a job ID.

**Response:**

```json
{
  "job_id": "generated_job_id"
}
```

### 2. Get Job Status

**Endpoint:** `/scrape/status/{job_id}`

**Method:** `GET`

**Description:** Returns the status of the job with the given job ID.

**Response:**

```jsonc
{
  "Status": "job_status"
  "Result": "scraped_content" // dummy scraped content
}
```

## Example Usage

1. Enqueue a scrape job:

```sh
curl http://localhost:8080/scrape/example.com
```

Response:

```json
{
  "job_id": "abc123"
}
```

2. Check the status of the job:

```sh
curl http://localhost:8080/scrape/status/abc123
```

Response:

```json
{
  "Status": "Closed"
}
```

## Implementation Details

The server uses the `gocmq` library to create a concurrent queue with a concurrency level of 10. The `scrapeWorker` function simulates the scraping work by sleeping for a few seconds and then returning the scraped content.

The job results are stored in a `sync.Map` to allow concurrent access. The `generateJobID` function generates a random job ID for each enqueued job.

The `scrapeHandler` function handles the `/scrape/{url}` endpoint by enqueuing a job and returning the job ID. The `statusHandler` function handles the `/scrape/status/{job_id}` endpoint by returning the status of the job.

## License

This project is licensed under the MIT License.
