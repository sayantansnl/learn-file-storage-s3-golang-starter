package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<30)
	videoID, err := uuid.Parse(r.PathValue("videoID"))
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "unable to parse UUID", err)
		return
	}

	bearerToken, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "unable to get bearer token", err)
		return
	}

	userID, err := auth.ValidateJWT(bearerToken, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "unable to validate jwt", err)
		return
	}

	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "unable to get video", err)
		return
	}

	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "action not allowed", nil)
		return
	}

	file, fileHeader, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "unable to form file", err)
		return
	}

	defer file.Close()

	mediaType, _, err := mime.ParseMediaType(fileHeader.Header.Get("Content-Type"))
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "uable to mime parse media type", err)
		return
	}

	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "media is not of video-type", nil)
		return
	}

	tempFile, err := os.CreateTemp("", "tubely-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "unable to create temp file", err)
		return
	}

	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	if _, err := io.Copy(tempFile, file); err != nil {
		respondWithError(w, http.StatusInternalServerError, "unable to copy", err)
		return
	}

	if _, err := tempFile.Seek(0, io.SeekStart); err != nil {
		respondWithError(w, http.StatusInternalServerError, "unable to restart reading from temp", err)
		return
	}

	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		respondWithError(w, http.StatusInternalServerError, "unable to read into bytes", err)
		return
	}
	encodedString := base64.RawURLEncoding.EncodeToString(bytes)
	fileType := strings.Split(mediaType, "/")[1]
	fileNameWithType := fmt.Sprintf("%s.%s", encodedString, fileType)

	if _, err := cfg.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &fileNameWithType,
		Body:        tempFile,
		ContentType: &mediaType,
	}); err != nil {
		respondWithError(w, http.StatusInternalServerError, "unable to put into bucket", err)
		return
	}

	videoURL := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", cfg.s3Bucket, cfg.s3Region, fileNameWithType)
	video.VideoURL = &videoURL

	if err := cfg.db.UpdateVideo(video); err != nil {
		respondWithError(w, http.StatusInternalServerError, "unable to update video", err)
		return
	}

	respondWithJSON(w, http.StatusOK, video)
}
