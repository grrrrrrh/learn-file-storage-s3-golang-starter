package main

import (
	"bytes"
	"fmt"
	"os/exec"
)

func processVideoForFastStart(filePath string) (string, error) {
	outputPath := filePath + ".faststart.mp4"

	cmd := exec.Command(
		"ffmpeg",
		"-y", // overwrite if exists
		"-i", filePath,
		"-c", "copy",
		"-movflags", "+faststart",
		"-f", "mp4",
		outputPath,
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("ffmpeg failed: %w: %s", err, stderr.String())
	}

	return outputPath, nil
}
