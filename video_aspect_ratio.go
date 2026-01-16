package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
)

type ffprobeOutput struct {
	Streams []struct {
		CodecType string `json:"codec_type"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
	} `json:"streams"`
}

func getVideoAspectRatio(filePath string) (string, error) {
	cmd := exec.Command(
		"ffprobe",
		"-v", "error",
		"-print_format", "json",
		"-show_streams",
		filePath,
	)

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("ffprobe failed: %w", err)
	}

	var out ffprobeOutput
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		return "", fmt.Errorf("ffprobe json parse failed: %w", err)
	}

	// Find the first VIDEO stream (more reliable than Streams[0])
	for _, s := range out.Streams {
		if s.CodecType != "video" {
			continue
		}
		if s.Width == 0 || s.Height == 0 {
			return "other", nil
		}

		ratio := float64(s.Width) / float64(s.Height)

		// 16:9 ≈ 1.777..., 9:16 ≈ 0.5625
		if math.Abs(ratio-(16.0/9.0)) < 0.05 {
			return "16:9", nil
		}
		if math.Abs(ratio-(9.0/16.0)) < 0.05 {
			return "9:16", nil
		}
		return "other", nil
	}

	return "other", nil
}
