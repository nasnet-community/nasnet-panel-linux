//go:build race

package xray

func init() { raceDetector = true }
