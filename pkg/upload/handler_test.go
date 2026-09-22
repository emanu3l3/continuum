package upload

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type mockService struct {
	newUploadFn     func(ctx context.Context, f *FileMetadata) (uuid.UUID, error)
	processChunkFn  func(ctx context.Context, body io.Reader, upUUID uuid.UUID, chunkID int64) error
	processStatusFn func(ctx context.Context, upUUID uuid.UUID) (*UploadStatus, error)
}

func (m *mockService) NewUpload(ctx context.Context, f *FileMetadata) (uuid.UUID, error) {
	if m.newUploadFn != nil {
		return m.newUploadFn(ctx, f)
	}
	return uuid.Nil, nil
}

func (m *mockService) ProcessChunk(ctx context.Context, stream io.Reader, upUUID uuid.UUID, chunkID int64) error {
	if m.processChunkFn != nil {
		return m.processChunkFn(ctx, stream, upUUID, chunkID)
	}
	return nil
}

func (m *mockService) ProcessStatus(ctx context.Context, upUUID uuid.UUID) (*UploadStatus, error) {
	if m.processStatusFn != nil {
		return m.processStatusFn(ctx, upUUID)
	}
	return &UploadStatus{}, nil
}

var (
	maxFileSize        int64 = 4_294_967_296
	maxChunkSize       int64 = 2_097_152
	acceptedExtensions       = map[string]struct{}{
		"pdf":  {},
		"mp4":  {},
		"png":  {},
		"jpg":  {},
		"jpeg": {},
		"mov":  {},
	}
)

func TestHandleNewUpload(t *testing.T) {
	generatedUUID := uuid.New()

	tests := []struct {
		name         string
		body         string
		mockErr      error
		expectedCode int
	}{
		{"correct data", `{"name": "file", "extension": "mp4", "size": 3294967296, "chunk_size": 2097152}`, nil, http.StatusCreated},
		{"malformed json", `{"name": "file", invalid}`, nil, http.StatusBadRequest},
		{"invalid file size", `{"name": "file", "extension": "mp4", "size": 5294967296, "chunk_size": 2097152}`, nil, http.StatusBadRequest},
		{"invalid chunk size", `{"name": "file", "extension": "mp4", "size": 3294967296, "chunk_size": 5097152}`, nil, http.StatusBadRequest},
		{"chunk size > file size", `{"name": "file", "extension": "mp4", "size": 1097152, "chunk_size": 2097152}`, nil, http.StatusBadRequest},
		{"internal service error", `{"name": "file", "extension": "mp4", "size": 3294967296, "chunk_size": 2097152}`, errors.New("disk error"), http.StatusInternalServerError},
		{"wrapped internal error", `{"name": "file", "extension": "mp4", "size": 3294967296, "chunk_size": 2097152}`, Internal("NewUpload", errors.New("uuid gen failed"), "couldn't create the upload"), http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := &mockService{
				newUploadFn: func(ctx context.Context, f *FileMetadata) (uuid.UUID, error) {
					if tt.mockErr != nil {
						return uuid.Nil, tt.mockErr
					}
					return generatedUUID, nil
				},
			}
			r := NewHandler(srv, maxFileSize, maxChunkSize, acceptedExtensions)

			req := httptest.NewRequest(http.MethodPost, "/files", strings.NewReader(tt.body))
			rr := httptest.NewRecorder()

			r.ServeHTTP(rr, req)

			if rr.Code != tt.expectedCode {
				t.Errorf("got %d expected %d per test '%s'", rr.Code, tt.expectedCode, tt.name)
			}
		})
	}
}

func TestHandleProcessChunk(t *testing.T) {
	validUUID := uuid.NewString()
	const op = "ProcessChunk"

	tests := []struct {
		name         string
		url          string
		mockErr      error
		expectedCode int
	}{
		{"correct data", "/files/" + validUUID + "/chunk/1", nil, http.StatusOK},
		{"invalid upUUID", "/files/invalid-uuid/chunk/1", nil, http.StatusBadRequest},
		{"invalid chunkID format", "/files/" + validUUID + "/chunk/abc", nil, http.StatusBadRequest},
		{"upload not found", "/files/" + validUUID + "/chunk/1", NotFound(op, ErrUploadNotFound), http.StatusNotFound},
		{"upload already completed", "/files/" + validUUID + "/chunk/1", Conflict(op, ErrUploadCompleted), http.StatusConflict},
		{"chunk already written", "/files/" + validUUID + "/chunk/1", Conflict(op, ErrChunkAlreadyWritten), http.StatusConflict},
		{"invalid chunkID value", "/files/" + validUUID + "/chunk/1", BadRequest(op, ErrChunkID), http.StatusBadRequest},
		{"internal server error", "/files/" + validUUID + "/chunk/1", errors.New("write error"), http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := &mockService{
				processChunkFn: func(ctx context.Context, stream io.Reader, upUUID uuid.UUID, chunkID int64) error {
					return tt.mockErr
				},
			}
			r := NewHandler(srv, maxFileSize, maxChunkSize, acceptedExtensions)

			req := httptest.NewRequest(http.MethodPatch, tt.url, strings.NewReader("qwerty"))
			rr := httptest.NewRecorder()

			r.ServeHTTP(rr, req)

			if rr.Code != tt.expectedCode {
				t.Errorf("got %d expected %d per test '%s'", rr.Code, tt.expectedCode, tt.name)
			}
		})
	}
}

func TestHandleStatusUpload(t *testing.T) {
	validUUID := uuid.NewString()
	const op = "ProcessStatus"

	tests := []struct {
		name         string
		url          string
		mockErr      error
		expectedCode int
	}{
		{"correct data", "/files/" + validUUID, nil, http.StatusOK},
		{"invalid upUUID", "/files/invalid-uuid", nil, http.StatusBadRequest},
		{"upload not found", "/files/" + validUUID, NotFound(op, ErrUploadNotFound), http.StatusNotFound},
		{"failed to recover chunks", "/files/" + validUUID, Internal(op, ErrRecoverChunks, "couldn't get info about the upload"), http.StatusInternalServerError},
		{"generic internal error", "/files/" + validUUID, errors.New("read error"), http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := &mockService{
				processStatusFn: func(ctx context.Context, upUUID uuid.UUID) (*UploadStatus, error) {
					if tt.mockErr != nil {
						return nil, tt.mockErr
					}
					return &UploadStatus{}, nil
				},
			}
			r := NewHandler(srv, maxFileSize, maxChunkSize, acceptedExtensions)

			req := httptest.NewRequest(http.MethodGet, tt.url, nil)
			rr := httptest.NewRecorder()

			r.ServeHTTP(rr, req)

			if rr.Code != tt.expectedCode {
				t.Errorf("got %d expected %d per test '%s'", rr.Code, tt.expectedCode, tt.name)
			}
		})
	}
}
