package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"

	// 🔧 CHANGE these to your actual module paths
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	// 1) Limit upload size to 1GB
	r.Body = http.MaxBytesReader(w, r.Body, 1<<30)

	// 2) Extract videoID from URL params and parse as UUID
	videoIDStr := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDStr)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid videoID", err)
		return
	}

	// 3) Authenticate user to get userID
	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "missing/invalid auth token", err)
		return
	}
	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "invalid auth token", err)
		return
	}

	// 4) Get video metadata; ensure user owns the video
	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "video not found", err)
		return
	}
	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "not the video owner", nil)
		return
	}

	// 5) Parse uploaded file from form data: key "video"
	videoFile, videoHeader, err := r.FormFile("video")
	if err != nil {
		// If body is too large, FormFile/ParseMultipartForm often errors out here.
		respondWithError(w, http.StatusBadRequest, "could not read uploaded file", err)
		return
	}
	defer videoFile.Close()

	// 6) Validate it's an MP4 using mime.ParseMediaType and "video/mp4"
	ct := videoHeader.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(ct)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "invalid content-type", err)
		return
	}
	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "only video/mp4 is supported", nil)
		return
	}

	// 7) Save to a temporary file on disk
	tempFile, err := os.CreateTemp("", "tubely-upload-*.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "could not create temp file", err)
		return
	}
	defer tempFile.Close()
	defer os.Remove(tempFile.Name())

	if _, err := io.Copy(tempFile, videoFile); err != nil {
		respondWithError(w, http.StatusInternalServerError, "could not write temp file", err)
		return
	}
	// after io.Copy(...) succeeded

	// Detect aspect ratio using ffprobe on the temp file path
	aspect, err := getVideoAspectRatio(tempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "could not detect aspect ratio", err)
		return
	}

	prefix := "other"
	switch aspect {
	case "16:9":
		prefix = "landscape"
	case "9:16":
		prefix = "portrait"
	}

	// rewind again just to be safe (ffprobe doesn’t move pointer, but harmless)
	_, err = tempFile.Seek(0, io.SeekStart)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "could not rewind temp file", err)
		return
	}

	hexKey, err := randomHex32()
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "could not generate key", err)
		return
	}

	// ✅ key now includes the prefix "folder"

	// 9) Put object into S3
	key := fmt.Sprintf("%s/%s.mp4", prefix, hexKey) // hexKey is 64 hex chars (32 random bytes)
	_, err = cfg.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket:      aws.String(cfg.s3Bucket),
		Key:         aws.String(key),
		Body:        tempFile,
		ContentType: aws.String(mediaType),
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "could not upload to s3", err)
		return
	}

	// 10) Update video_url in DB
	videoURL := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", cfg.s3Bucket, cfg.s3Region, key)

	err = cfg.db.UpdateVideoURL(videoID, videoURL)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "could not update video_url", err)
		return
	}

	respondWithJSON(w, http.StatusOK, map[string]string{
		"video_url": videoURL,
	})
}

// randomHex32 returns 32 hex characters (16 random bytes).
func randomHex32() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
