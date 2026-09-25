# Continuum
> ⚠️ Early stage project  

Continuum is a resumable file uploading system built on top of HTTP.  
It's designed to ensure fast, fault-tolerant, and network-resilient uploads for large files over unstable connections. 

## Key Features
- Parallel Chunked Transfer
- Resume interrupted uploads
- Local disk storage support

## Quick Start
### .env variables

| name                  | description                                                                                           |
| --------------------- | ----------------------------------------------------------------------------------------------------- |
| ADDRESS               | server address in the form of "host:port", if empty 0.0.0.0:80 is used                                |
| BASE_FILE_PATH        | directory where uploaded files are stored (required)                                                  |  
| MAX_FILE_SIZE_MB      | max size of a single uploaded file, in megabytes (required)                                           |
| MAX_CHUNK_SIZE_MB     | max size of a single upload chunk, in megabytes (required)                                            |
| MAX_IN_MEMORY_SECONDS | max seconds an incomplete upload is kept in memory without activity before being discarded (required) |

Prerequisites: go 1.26 or higher

```bash
git clone https://github.com/emanu3l3/continuum.git
cd continuum
cp .env.example .env
go mod download
go run ./cmd/server
```

## Specification
In order to upload a file every client needs to provide some information about it such as: its `name`, `extension`, `size` and `chunk_size`.  
`size` refers to the file's size and `chunk_size` is the size of each chunk (except possibly the last one) that the client will send. These two quantities must be expressed in **bytes**.

`chunk_size` can be chosen for each file upload (it's up to the client to choose a reasonable size), knowing this we can calculate the **total number of chunks** needed in order to complete the upload, that would be `ceil(size / chunk_size)`; If size isn't a multiple of `chunk_size` the last chunk will have a size of `size - chunk_size * (total_chunks - 1)`.  
Chunks are numbered starting from 0; for instance if we have a file with size = 4096 (bytes) and chunk_size = 1000 (bytes) the total number of chunks will be 5, thus we will have `[0, 1, 2, 3, 4]` chunks. These numbers represents also their **chunkID**.  
Knowing that and the `chunk_size` we know that each chunk represents the file data from byte

  *`(chunkID * chunk_size)`* *to byte* *`((chunkID + 1) * chunk_size)`*

**Note**: The byte range uses a half-open interval: [start, end)  

| chunkID | bytes       |
| ------- | ----------  |
| 0       | 0 - 1000    |
| 1       | 1000 - 2000 |     
| 2       | 2000 - 3000 |  
| 3       | 3000 - 4000 |
| 4       | 4000 - 4096 |

The chunking process needs to start from the start of the file and continue linearly till the EOF.
This method keeps things simple for the client when sending chunks as it only has to provide the chunkID and the binary data associated. The maximum allowed file size and chunk_size are defined by the `MAX_FILE_SIZE_MB` and `MAX_CHUNK_SIZE_MB` env variables.

Note that Continuum can receive chunks sent in parallel thus for the client is not mandatory sending them in order.
 

## API
**Note**: every error will be returned as a json in the form of `{"error": error_message}`

Continuum exposes three endpoints:
### `POST /files`
Initializes a new upload and returns a UUID that identifies it.

| Field      | Type   | Description                 |
| ---------- | ------ | --------------------------- |
| name       | string | File name                   |
| extension  | string | File extension (without dot)|
| size       | int64  | Total file size in bytes    |
| chunk_size | int64  | Size of each chunk in bytes |


**Request example**
```
POST /files
Content-Type: application/json

{
	"name": "file",
	"extension": "pdf",
	"size": 4096,
	"chunk_size": 1024
}
```

**Response**
```
201 Created

{
  "uuid": "550e8400-e29b-41d4-a716-446655440000"
}
```

**Errors**

| Status | Condition                                  |
| ------ | ---------------------------------------    |
| 400    | Invalid request body or invalid fields.     |
| 500    | Couldn't initialize the upload.             |

### `PATCH /files/{uuid}/chunk/{chunkID}`
Upload a portion of the file. The client sends the chunk as a raw binary data.  
As soon as all chunks have been written the upload will be completed.  
Right now the `Content-Type` value is not validated by the server, but it's recommended to send `application/octet-stream`.

**Request example**
```
PATCH /files/550e8400-e29b-41d4-a716-446655440000/chunk/0 
Content-Type: application/octet-stream

<chunk data>
```
**Response**
```
200 OK

{
	"success": "chunk written"
}
```
**Errors**

| Status | Condition                                  		 |
| ------ | ---------------------------------------    		 |
| 400    | Invalid uuid or chunkID.     						 |
| 404    | Upload not found. |
| 409    | Upload already completed or chunk already written. |
| 500    | Couldn't write the chunk.            				 |

### `GET /files/{uuid}`
Get info about the upload status or resume the upload if a crash occurs (returns the upload status as well).  
If the upload is already completed `chunks_written` field will be an empty array, otherwise it will contain the chunkIDs of the chunks that have been written.

**Request example**
```
GET /files/550e8400-e29b-41d4-a716-446655440000 
```

**Response**
```
200 OK

{
	"file": {
		"name": "file",
		"extension": "pdf",
		"size": 4096,
		"chunk_size": 1024,
		"total_chunks": 4,
		"completed": true,
		"created_at": "2026-08-24T16:07:32.8284546Z",
		"completed_at": "2026-08-24T16:07:49.3547143Z"
	},
	"chunks_written": []
}
```

```
200 OK

{
	"file": {
		"name": "file",
		"extension": "pdf",
		"size": 4096,
		"chunk_size": 1024,
		"total_chunks": 4,
		"completed": false,
		"created_at": "2026-08-24T16:07:32.8284546Z",
		"completed_at": "2026-08-24T16:07:49.3547143Z"
	},
	"chunks_written": [0, 3]
}
```

**Errors**
| Status | Condition                                  |
| ------ | ---------------------------------------    |
| 400    | Invalid uuid.     						 |
| 404    | Upload not found.     |
| 500    | Couldn't get info about the upload.             |



## Storage
Currently Continuum supports only local disk storage, but it can be easily expanded to other storage mechanisms
by implementing the `Storage` interface.  

### Disk Storage
Stores all uploaded files in the directory specified by the `BASE_FILE_PATH` env variable.  
For each file, a folder named after its generated UUID is created, containing:
- the actual file, named `UUID.extension`
- a metadata file with file information, named `UUID.json`
- a state file containing chunkIDs of written chunks, named `UUID.state`.

The state file is automatically deleted once the file upload has been completed.

```
BASE_FILE_PATH="./files"

./files
└── 550e8400-e29b-41d4-a716-446655440000
	├── 550e8400-e29b-41d4-a716-446655440000.pdf
	├── 550e8400-e29b-41d4-a716-446655440000.json
	└── 550e8400-e29b-41d4-a716-446655440000.state
```	

## Registry and Resuming
Continuum has an in-memory **registry** that keeps track of all information about the upload and files.
This optimizes performance by eliminating disk I/O overhead and avoiding frequent file open/close operations to retrieve metadata.

When a new upload is initialized it's added to the registry and as soon as it completes it's removed.  
When an upload has no activity for `MAX_IN_MEMORY_SECONDS` it's removed from the registry, this ensures that abandonated or incomplete uploads don't remain in memory indefinitely.   
To resume an upload the client calls `GET /files/{uuid}`, reads `chunks_written` and sends only the missing chunks.

## Contributing
This project is in early stage, the main goal is to learn and experiment with a hands-on approach, so ...
don't hesitate opening issues or sending pull requests!!
