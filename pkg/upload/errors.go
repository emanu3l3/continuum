package upload

import (
	"errors"
	"net/http"
)

type Error struct {
	Op      string // function in which the error happened
	Status  int    // http status
	Err     error  // sentinel
	Message string // message to show to client
}

func (e *Error) Error() string {
	return e.Err.Error()
}

func (e *Error) Unwrap() error { return e.Err }

func NotFound(op string, err error) *Error {
	return &Error{Op: op, Status: http.StatusNotFound, Err: err, Message: err.Error()}
}

func Conflict(op string, err error) *Error {
	return &Error{Op: op, Status: http.StatusConflict, Err: err, Message: err.Error()}
}

func BadRequest(op string, err error) *Error {
	return &Error{Op: op, Status: http.StatusBadRequest, Err: err, Message: err.Error()}
}

func Internal(op string, err error, publicMsg string) *Error {
	return &Error{Op: op, Status: http.StatusInternalServerError, Err: err, Message: publicMsg}
}

// Sentinel errors
var (
	ErrUpload          = errors.New("couldn't create the upload.")
	ErrBadJSON         = errors.New("invalid request body.")
	ErrFileName        = errors.New("file name lenght must be >= 0.")
	ErrFileExtension   = errors.New("file extension not valid")
	ErrFileSize        = errors.New("invalid file size.")
	ErrFileChunkSize   = errors.New("invalid chunk size.")
	ErrFileChunkGTSize = errors.New("chunk size must not be grater than file size.")
	ErrInvalidUUID     = errors.New("invalid uuid.")
	ErrChunkIDURL      = errors.New("invalid chunk id url param.")
	ErrProcessChunk    = errors.New("couldn't write the chunk.")
	ErrCompleteUpload  = errors.New("couldn't complete the upload.")
	ErrStatusUpload    = errors.New("couldn't get info about the upload.")

	ErrUUID                = errors.New("couldn't generate uuid")
	ErrChunkID             = errors.New("chunk id not valid")
	ErrUploadNotCompleted  = errors.New("not all chunks have been uploaded yet")
	ErrUploadCompleted     = errors.New("upload already completed")
	ErrGetMetadata         = errors.New("failed to retrieve upload status")
	ErrRecoverChunks       = errors.New("failed to retrieve chunks")
	ErrUploadNotFound      = errors.New("upload not found")
	ErrChunkAlreadyWritten = errors.New("chunk already written")

	ErrMimeType = errors.New("file mime type do not correspond with the file extension provided")
)
