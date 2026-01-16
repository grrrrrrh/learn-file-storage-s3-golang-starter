package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"

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
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)

	if _, err := io.Copy(tempFile, videoFile); err != nil {
		_ = tempFile.Close()
		respondWithError(w, http.StatusInternalServerError, "could not write temp file", err)
		return
	}

	// ✅ CLOSE so ffmpeg can read it properly
	if err := tempFile.Close(); err != nil {
		respondWithError(w, http.StatusInternalServerError, "could not close temp file", err)
		return
	}

	// ✅ Process video for fast start
	processedPath, err := processVideoForFastStart(tempPath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "could not process video for fast start", err)
		return
	}
	defer os.Remove(processedPath)

	processedFile, err := os.Open(processedPath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "could not open processed video", err)
		return
	}
	defer processedFile.Close()

	// ✅ Aspect ratio check
	aspect, err := getVideoAspectRatio(processedPath)
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

	hexKey, err := randomHex32()
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "could not generate key", err)
		return
	}

	key := fmt.Sprintf("%s/%s.mp4", prefix, hexKey)

	// ✅ Upload processed file to S3 (ONLY ONCE)
	_, err = cfg.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket:      aws.String(cfg.s3Bucket),
		Key:         aws.String(key),
		Body:        processedFile,
		ContentType: aws.String("video/mp4"),
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "could not upload to s3", err)
		return
	}

	// ✅ Store a REAL URL again (CloudFront URL)
	cfBase := strings.TrimRight(cfg.s3CfDistribution, "/")
	videoURL := fmt.Sprintf("%s/%s", cfBase, key)

	err = cfg.db.UpdateVideoURL(videoID, videoURL)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "could not update video_url", err)
		return
	}

	respondWithJSON(w, http.StatusOK, map[string]string{
		"video_url": videoURL,
	})
}

// randomHex32 returns 64 hex characters (32 random bytes).
func randomHex32() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
