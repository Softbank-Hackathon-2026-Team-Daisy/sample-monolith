// Package platform names the environment the process is running in, for display.
//
// It reads markers that each runtime sets on its own, so deployments need no
// extra configuration: ECS sets AWS_EXECUTION_ENV, Cloud Run sets K_SERVICE and
// Docker creates /.dockerenv. DEPLOY_PLATFORM overrides the detection.
package platform

import (
	"os"
	"strings"
)

// Detect returns a short display name such as "AWS · ECS Fargate".
// getenv is typically os.Getenv; exists reports whether a file exists.
func Detect(getenv func(string) string, exists func(string) bool) string {
	if v := strings.TrimSpace(getenv("DEPLOY_PLATFORM")); v != "" {
		return v
	}
	switch v := getenv("AWS_EXECUTION_ENV"); {
	case v == "AWS_ECS_FARGATE":
		return "AWS · ECS Fargate"
	case strings.HasPrefix(v, "AWS_ECS"):
		return "AWS · ECS"
	}
	if getenv("K_SERVICE") != "" {
		return "GCP · Cloud Run"
	}
	if exists("/.dockerenv") {
		return "On-premises · Docker"
	}
	return "Local"
}

// FileExists reports whether path exists.
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
