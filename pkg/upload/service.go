package upload

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

type svc struct {
	store           Storage
	reg             UploadRegistry
	maxInMemSeconds int64
}

func NewService(store Storage, reg UploadRegistry, maxInMemSeconds int64) *svc {
	return &svc{
		store:           store,
		reg:             reg,
		maxInMemSeconds: maxInMemSeconds,
	}
}

func (s *svc) resetInactivityTimer(upContext *UploadContext, upUUID uuid.UUID) {
	upContext.Rw.Lock()
	defer upContext.Rw.Unlock()

	d := time.Duration(s.maxInMemSeconds) * time.Second

	if upContext.InactivityTimer != nil {
		upContext.InactivityTimer.Stop()
	}

	upContext.InactivityTimer = time.AfterFunc(d, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		s.store.Close(ctx, upUUID)
		s.reg.Remove(upUUID)
	})
}

func (s *svc) NewUpload(ctx context.Context, f *FileMetadata) (uuid.UUID, error) {
	const op = "NewUpload"

	upUUID, err := uuid.NewV7()
	if err != nil {
		return uuid.UUID{}, Internal(op, err, "couldn't initialize the upload.")
	}

	err = s.store.InitUpload(ctx, upUUID, f)
	if err != nil {
		return uuid.UUID{}, Internal(op, err, "couldn't initialize the upload.")
	}

	upContext := NewUploadContext(f)
	defer s.resetInactivityTimer(upContext, upUUID)

	err = s.reg.Save(upUUID, upContext)
	if err != nil {
		return uuid.UUID{}, Internal(op, err, "couldn't initialize the upload.")
	}

	return upUUID, nil
}

func (s *svc) finalizeUpload(ctx context.Context, upContext *UploadContext, upUUID uuid.UUID) error {
	const op = "finalizeUpload"

	err := s.store.Complete(ctx, upUUID)
	if err != nil {
		return Internal(op, err, "couldn't complete the upload")
	}

	upContext.Rw.Lock()
	upContext.FileMtd.Completed = true
	upContext.Rw.Unlock()

	err = s.reg.Remove(upUUID)
	if err != nil {
		return Internal(op, err, "couldn't complete the upload")
	}

	return nil
}

func (s *svc) ProcessChunk(ctx context.Context, body io.Reader, upUUID uuid.UUID, chunkID int64) error {
	const op = "ProcessChunk"

	upContext, err := s.reg.Get(upUUID)
	if err != nil {
		return NotFound(op, ErrUploadNotFound)
	}

	defer s.resetInactivityTimer(upContext, upUUID)

	if upContext.FileMtd.Completed {
		return Conflict(op, ErrUploadCompleted)
	}

	upContext.Rw.RLock()
	_, alreadyWritten := upContext.ChunksWritten[chunkID]
	upContext.Rw.RUnlock()

	if alreadyWritten {
		return Conflict(op, ErrChunkAlreadyWritten)
	}

	// check if chunkID is a valid one. The total
	// number of chunks is utils.CeilDiv(size / chunk size),
	// thus 0 <= chunkID < totalChunks.
	if chunkID >= upContext.FileMtd.TotalChunks || chunkID < 0 {
		return BadRequest(op, ErrChunkID)
	}

	// read only the size of the chunk from the body
	reader := io.LimitReader(body, int64(upContext.FileMtd.ChunkSize))
	chunkBytes, err := io.ReadAll(reader)
	if err != nil {
		return Internal(op, err, "couldn't write the chunk")
	}

	// check if file magic bytes correspond to the file extension
	if chunkID == 0 {
		rawMIME := http.DetectContentType(chunkBytes)
		parsedMIME, _, err := mime.ParseMediaType(rawMIME)
		if err != nil {
			parsedMIME = rawMIME
		}

		exensions, err := mime.ExtensionsByType(parsedMIME)
		if err != nil || len(exensions) == 0 {
			s.reg.Remove(upUUID)
			s.store.Delete(ctx, upUUID)
			return BadRequest(op, ErrMimeType)
		}

		found := false
		for _, validExt := range exensions {
			if strings.TrimPrefix(validExt, ".") == upContext.FileMtd.Extension {
				found = true
				break
			}
		}

		if !found {
			s.reg.Remove(upUUID)
			s.store.Delete(ctx, upUUID)
			return BadRequest(op, ErrMimeType)
		}
	}

	err = s.store.WriteChunk(ctx, upUUID, chunkID, upContext.FileMtd.ChunkSize, chunkBytes)
	if err != nil {
		if errors.Is(err, ErrChunkAlreadyWritten) {
			return BadRequest(op, ErrChunkID)
		}
		return Internal(op, err, "couldn't write the chunk")
	}

	upContext.Rw.Lock()
	upContext.ChunksWritten[chunkID] = struct{}{}
	upContext.TotalChunksWritten += 1
	upContext.Rw.Unlock()

	// check if upload is completed
	if upContext.FileMtd.TotalChunks == upContext.TotalChunksWritten {
		if err = s.finalizeUpload(ctx, upContext, upUUID); err != nil {
			return err
		}
	}

	return nil
}

func (s *svc) ProcessStatus(ctx context.Context, upUUID uuid.UUID) (*UploadStatus, error) {
	const op = "ProcessStatus"

	upContext, err := s.reg.Get(upUUID)

	if err == nil {
		return NewUploadStatus(upContext), nil
	}

	fileMtd, err := s.store.GetMetadata(ctx, upUUID)
	if err != nil {
		return nil, NotFound(op, ErrUploadNotFound)
	}

	if fileMtd.Completed {
		return &UploadStatus{
			FileMtd:       fileMtd,
			ChunksWritten: []int{},
		}, nil
	}

	upCtx := NewUploadContext(fileMtd)
	defer s.resetInactivityTimer(upCtx, upUUID)

	upState, err := s.store.GetState(ctx, upUUID, fileMtd.Extension)
	if err != nil {
		return nil, Internal(op, err, "couldn't get info about the upload")
	}

	upCtx.ChunksWritten = upState.ChunksWritten
	upCtx.TotalChunksWritten = upState.TotalChunksWritten

	if (upCtx.FileMtd.TotalChunks == upCtx.TotalChunksWritten) && !upCtx.FileMtd.Completed {
		if err = s.finalizeUpload(ctx, upCtx, upUUID); err != nil {
			return nil, err
		}
		return NewUploadStatus(upCtx), nil
	}

	if err := s.reg.Save(upUUID, upCtx); err != nil {
		return nil, Internal(op, err, "couldn't get info about the upload")
	}

	return NewUploadStatus(upCtx), nil
}
