package upload

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/emanu3l3/continuum/utils"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type handler struct {
	srv                Service
	maxFileSize        int64
	maxChunkSize       int64
	acceptedExtensions map[string]struct{}
}

func (h *handler) validateFileFields(file *FileMetadata) error {
	if len(file.Name) <= 0 {
		return ErrFileName
	}

	if _, ok := h.acceptedExtensions[file.Extension]; !ok {
		return ErrFileExtension
	}

	if file.Size <= 0 || file.Size > h.maxFileSize {
		return ErrFileSize
	}

	if file.ChunkSize <= 0 || file.ChunkSize > h.maxChunkSize {
		return ErrFileChunkSize
	}

	if file.ChunkSize > file.Size {
		return ErrFileChunkGTSize
	}

	return nil
}

func getUUID(r *http.Request) uuid.UUID {
	upUUID := r.Context().Value(upUUIDKey).(uuid.UUID)
	return upUUID
}

func getUUIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		strUUID := chi.URLParam(r, "upUUID")

		if err := uuid.Validate(strUUID); err != nil {
			utils.WriteError(w, http.StatusBadRequest, err)
			return
		}

		upUUID, err := uuid.Parse(strUUID)
		if err != nil {
			utils.WriteError(w, http.StatusBadRequest, err)
			return
		}

		ctx := context.WithValue(r.Context(), upUUIDKey, upUUID)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func writeServiceError(w http.ResponseWriter, err error) {
	var svcErr *Error
	if errors.As(err, &svcErr) {
		if svcErr.Status >= http.StatusInternalServerError {
			log.Printf("[%s] internal error: %v", svcErr.Op, svcErr.Err)
		}
		utils.WriteError(w, svcErr.Status, errors.New(svcErr.Message))
		return
	}

	log.Printf("unexpected error: %v", err)
	utils.WriteError(w, http.StatusInternalServerError, errors.New("internal error"))
}

func NewHandler(srv Service, maxFileSize int64, maxChunkSize int64, acceptedExtensions map[string]struct{}) http.Handler {
	h := &handler{
		srv:                srv,
		maxFileSize:        maxFileSize,
		maxChunkSize:       maxChunkSize,
		acceptedExtensions: acceptedExtensions,
	}

	r := chi.NewRouter()
	r.Post("/files", h.HandleNewUpload)
	r.Group(func(r chi.Router) {
		r.Use(getUUIDMiddleware)
		r.Patch("/files/{upUUID}/chunk/{chunkID}", h.HandleProcessChunk)
		r.Get("/files/{upUUID}", h.HandleStatusUpload)
	})

	return r
}

func (h *handler) HandleNewUpload(w http.ResponseWriter, r *http.Request) {
	var fileMtd FileMetadata

	err := json.NewDecoder(r.Body).Decode(&fileMtd)
	if err != nil {
		utils.WriteError(w, http.StatusBadRequest, ErrBadJSON)
		return
	}
	fileMtd.CreatedAt = time.Now().UTC()
	fileMtd.TotalChunks = utils.CeilDiv(fileMtd.Size, fileMtd.ChunkSize)

	if err := h.validateFileFields(&fileMtd); err != nil {
		utils.WriteError(w, http.StatusBadRequest, err)
		return
	}

	upUUID, err := h.srv.NewUpload(r.Context(), &fileMtd)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	utils.WriteJson(w, http.StatusCreated, map[string]uuid.UUID{"uuid": upUUID})
}

func (h *handler) HandleProcessChunk(w http.ResponseWriter, r *http.Request) {
	upUUID := getUUID(r)

	chunkID, err := strconv.Atoi(chi.URLParam(r, "chunkID"))
	if err != nil {
		utils.WriteError(w, http.StatusBadRequest, ErrChunkIDURL)
		return
	}

	err = h.srv.ProcessChunk(r.Context(), r.Body, upUUID, int64(chunkID))
	if err != nil {
		writeServiceError(w, err)
		return
	}

	utils.WriteJson(w, http.StatusOK, map[string]string{"success": "chunk written"})
}

func (h *handler) HandleStatusUpload(w http.ResponseWriter, r *http.Request) {
	upUUID := getUUID(r)

	upStatus, err := h.srv.ProcessStatus(r.Context(), upUUID)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	utils.WriteJson(w, http.StatusOK, upStatus)
}
