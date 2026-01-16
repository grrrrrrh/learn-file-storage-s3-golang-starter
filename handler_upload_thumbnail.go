package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	const maxMemory = 10 << 20
	if err := r.ParseMultipartForm(maxMemory); err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse multipart form", err)
		return
	}

	file, header, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	defer file.Close()

	rawCT := header.Header.Get("Content-Type")
	mediaType, _, parseErr := mime.ParseMediaType(rawCT)
	if parseErr != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid Content-Type", parseErr)
		return
	}

	if mediaType != "image/jpeg" && mediaType != "image/png" {
		respondWithError(w, http.StatusBadRequest, "Thumbnail must be a PNG or JPEG", fmt.Errorf("got %s", mediaType))
		return
	}

	// Get video + authorize
	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "Video not found", err)
		return
	}
	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "Not authorized", nil)
		return
	}

	// Determine extension from media type (image/png -> png, image/jpeg -> jpeg)
	parts := strings.Split(mediaType, "/")
	if len(parts) != 2 || parts[1] == "" {
		respondWithError(w, http.StatusBadRequest, "Unsupported Content-Type", fmt.Errorf("content-type: %q", mediaType))
		return
	}
	ext := parts[1]

	// 32 random bytes -> URL-safe base64 string (no padding)
	randBytes := make([]byte, 32)
	if _, err := rand.Read(randBytes); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to generate filename", err)
		return
	}

	name := base64.RawURLEncoding.EncodeToString(randBytes)
	filename := fmt.Sprintf("%s.%s", name, ext)
	dstPath := filepath.Join(cfg.assetsRoot, filename)

	if err := os.MkdirAll(cfg.assetsRoot, 0755); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to create assets directory", err)
		return
	}

	dst, err := os.Create(dstPath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to create file", err)
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to save file", err)
		return
	}

	thumbURL := fmt.Sprintf("http://localhost:%s/assets/%s", cfg.port, filename)
	video.ThumbnailURL = &thumbURL

	if err := cfg.db.UpdateVideo(video); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to update video", err)
		return
	}

	respondWithJSON(w, http.StatusOK, video)
}
